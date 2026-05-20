//go:build integration
// +build integration

// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
//
// SPDX-License-Identifier: MIT

package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

// TestUC6_PowerControl_Redfish proves the power-control → Redfish round-trip:
//
//  1. Confirm the target node (x0c0s0b0n0) is known to power-control via
//     /v1/power-status — this requires that SMD discovery has populated
//     ComponentEndpoints from a prior magellan run (UC5) AND that
//     power-control's periodic refresh has pulled the component map.
//  2. Force the BMC to a known initial state (PowerState=On) by issuing a
//     direct Redfish reset to the csm-rie sim. This makes the test
//     deterministic regardless of what the previous test left behind.
//  3. POST a force-off transition to power-control.
//  4. Poll /v1/transitions/<id> until transitionStatus=completed.
//  5. Verify the transition's task succeeded, and verify the side effect:
//     the csm-rie sim's PowerState read directly via Redfish must now be
//     "Off". This is the cross-service half — power-control made power-off
//     happen at the BMC, and we observe it through an independent channel.
//  6. Reverse with an on transition, verify PowerState=On the same way.
//
// Stub-resistance: cannot pass against a wiremock or canned-response stub
// because step 5/6 read the Redfish sim directly (not via power-control), so
// the BMC sim's in-memory PowerState must actually mutate. Verified manually:
// csm-rie's EX235a mockup does flip PowerState on ComputerSystem.Reset.
func TestUC6_PowerControl_Redfish(t *testing.T) {
	powerURL := Endpoints["power"]
	bmcURL := Endpoints["redfish-bmc"]
	smdURL := Endpoints["smd"]

	const (
		nodeXname = "x0c0s0b0n0"
		bmcUser   = "root"
		bmcPass   = "root_password"
	)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Discover the System path from /redfish/v1/Systems rather than
	// hard-coding `/Systems/Node0`. Different BMC sims pick different
	// System.Id values — CSM-RIE used "Node0", sushy-tools' --fake
	// driver uses a UUID. Discovery makes this test BMC-implementation-
	// agnostic, which is what we want for a sandbox that may swap
	// emulators over time.
	systemPath, err := discoverFirstSystemPath(ctx, bmcURL, bmcUser, bmcPass)
	if err != nil {
		t.Fatalf("discover System path: %v", err)
	}

	// Cleanup: leave the sim powered On regardless of how the test exits, so
	// follow-on UC tests (and re-runs of this one) start from a known state.
	t.Cleanup(func() {
		_ = redfishReset(context.Background(), bmcURL+systemPath, bmcUser, bmcPass, "On")
	})

	// Ensure SMD has a fully-discovered ComponentEndpoint for the node.
	// UC5 deletes/re-adds RedfishEndpoints with empty passwords (magellan
	// doesn't propagate creds), so ComponentEndpoints get cascade-deleted
	// and never auto-rediscovered. Without this, doTransition's FillHSMData
	// returns empty AllowableActions and the task fails with "Power action
	// not supported". This step is idempotent: if discovery already ran,
	// the PATCH and discover are no-ops in effect.
	ensureBMCDiscovered(ctx, t, smdURL, "x0c0s0b0")
	waitForComponentEndpoint(ctx, t, smdURL, nodeXname, 30*time.Second)

	// Clear any stale HSM reservation on the target xname. Without this, a
	// prior power-control transition (this run or a previous one) may still
	// hold a reservation that hasn't expired (1-minute TTL with library-level
	// auto-renewal), causing the next FlexAquire to return "Reserved" failure
	// and the transition to fail with "Unable to reserve component". The
	// disable→repair pair forces SMD to drop the reservation and re-allow
	// new ones. Cheap and idempotent.
	clearReservation(ctx, t, smdURL, nodeXname)

	// Step 1: power-control must know about the node. Without SMD discovery
	// having populated ComponentEndpoints, this returns "Component not found
	// in component map" and the rest of the test is meaningless.
	if err := waitForPCSComponent(ctx, powerURL, nodeXname); err != nil {
		t.Fatalf("power-control did not pick up %s: %v", nodeXname, err)
	}

	// Step 2: reset to known-On. The sim's PowerState passes through
	// transient "PoweringOn"/"PoweringOff" before settling, so wait.
	if err := redfishReset(ctx, bmcURL+systemPath, bmcUser, bmcPass, "On"); err != nil {
		t.Fatalf("priming BMC to On: %v", err)
	}
	if got := waitRedfishPowerState(ctx, t, bmcURL+systemPath, bmcUser, bmcPass, "On"); got != "On" {
		t.Fatalf("priming: expected PowerState=On after settle, got %q", got)
	}

	// Step 2.5: wait for power-control to converge on the primed state.
	// PCS polls BMCs on a ~30s cycle; if we POST a transition before its
	// cache reflects the just-primed value it will short-circuit against
	// the stale view (e.g. "already Off") and return `completed` without
	// ever touching the BMC. Sushy's --fake driver delays state changes
	// 1-11s on its own side, so PCS may need up to ~60s to settle.
	if got, err := waitForPCSPowerState(ctx, powerURL, nodeXname, "On", 60*time.Second); err != nil {
		t.Fatalf("waiting for PCS view to catch up to primed On: %v (last=%q)", err, got)
	}

	// Step 3+4: force-off via power-control, wait for completion.
	offID := postTransition(ctx, t, powerURL, "force-off", nodeXname)
	offResult := waitTransition(ctx, t, powerURL, offID)
	if offResult.transitionStatus != "completed" {
		t.Fatalf("force-off transition: status=%q tasks=%+v", offResult.transitionStatus, offResult.tasks)
	}
	if len(offResult.tasks) != 1 || offResult.tasks[0].TaskStatus != "succeeded" {
		t.Fatalf("force-off: expected 1 succeeded task, got %+v", offResult.tasks)
	}

	// Step 5: Redfish observes the side effect.
	if got := waitRedfishPowerState(ctx, t, bmcURL+systemPath, bmcUser, bmcPass, "Off"); got != "Off" {
		t.Fatalf("after force-off: expected Redfish PowerState=Off, got %q", got)
	}

	// Step 5.5: same gate as Step 2.5, in reverse. PCS must observe the
	// node is now Off before we ask it to turn the node back On —
	// otherwise PCS short-circuits ("already On" against the stale
	// PowerState from before the force-off) and never sends the Reset.
	if got, err := waitForPCSPowerState(ctx, powerURL, nodeXname, "Off", 60*time.Second); err != nil {
		t.Fatalf("waiting for PCS view to catch up to Off: %v (last=%q)", err, got)
	}

	// Step 6: turn it back on via power-control, observe via Redfish.
	onID := postTransition(ctx, t, powerURL, "on", nodeXname)
	onResult := waitTransition(ctx, t, powerURL, onID)
	if onResult.transitionStatus != "completed" {
		t.Fatalf("on transition: status=%q tasks=%+v", onResult.transitionStatus, onResult.tasks)
	}
	if len(onResult.tasks) != 1 || onResult.tasks[0].TaskStatus != "succeeded" {
		t.Fatalf("on: expected 1 succeeded task, got %+v", onResult.tasks)
	}
	if got := waitRedfishPowerState(ctx, t, bmcURL+systemPath, bmcUser, bmcPass, "On"); got != "On" {
		t.Fatalf("after on: expected Redfish PowerState=On, got %q", got)
	}
}

// ensureBMCDiscovered makes sure SMD has fresh ComponentEndpoint data
// for nodeXname's BMC, restoring it after UC5 (which deletes its
// fixture endpoints on cleanup as part of its idempotency contract).
//
// Strategy: re-seed the RedfishEndpoint, then re-run the canonical
// magellan pipeline. magellan walks each BMC and POSTs the resulting
// inventory back to SMD; SMD's V2 parser materialises Components +
// ComponentEndpoints from the systems/managers in the request body.
//
// We deliberately do NOT call SMD's /Inventory/Discover endpoint —
// that path is the legacy CSM-era SMD-side discovery walker
// (gated off in compose/core.yaml::smd args). The OpenCHAMI flow is
// magellan-as-source-of-truth; calling /Inventory/Discover would
// silently 200 without populating anything.
func ensureBMCDiscovered(ctx context.Context, t *testing.T, smdURL, bmcXname string) {
	t.Helper()

	// First, make sure the seed RedfishEndpoint is in place. UC5's
	// cleanup may have deleted it; magellan's send will resurrect it,
	// but starting from a known seed shape keeps the magellan POST a
	// clean PUT-update rather than a fresh create with potentially
	// drifted defaults. POST is idempotent (409 = already exists).
	postBody, _ := json.Marshal(map[string]any{
		"ID":                 bmcXname,
		"FQDN":               bmcXname,
		"User":               "root",
		"Password":           "root_password",
		"RediscoverOnUpdate": true,
	})
	cReq, _ := http.NewRequestWithContext(ctx, http.MethodPost,
		smdURL+"/hsm/v2/Inventory/RedfishEndpoints", bytes.NewReader(postBody))
	cReq.Header.Set("Content-Type", "application/json")
	if r, err := httpClient.Do(cReq); err == nil {
		r.Body.Close()
	}

	patch, _ := json.Marshal(map[string]string{"Password": "root_password"})
	preq, _ := http.NewRequestWithContext(ctx, http.MethodPatch,
		smdURL+"/hsm/v2/Inventory/RedfishEndpoints/"+bmcXname, bytes.NewReader(patch))
	preq.Header.Set("Content-Type", "application/json")
	if r, err := httpClient.Do(preq); err == nil {
		r.Body.Close()
	} else {
		t.Fatalf("patch RedfishEndpoint password: %v", err)
	}

	// Re-run magellan to populate ComponentEndpoints. Lives in
	// uc5_magellan_scan_test.go in the same package; takes ~5s.
	runMagellanPipeline(ctx, t)
}

// waitForComponentEndpoint polls SMD until the ComponentEndpoint for nodeXname
// exists AND has a non-empty AllowableValues array on its Reset action. SMD's
// discovery is async, so the Discover POST returns immediately while the
// agent is still talking to the BMC.
func waitForComponentEndpoint(ctx context.Context, t *testing.T, smdURL, nodeXname string, timeout time.Duration) {
	t.Helper()
	url := smdURL + "/hsm/v2/Inventory/ComponentEndpoints/" + nodeXname
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		resp, err := httpClient.Do(req)
		if err != nil {
			time.Sleep(time.Second)
			continue
		}
		if resp.StatusCode == http.StatusOK {
			var ce struct {
				RedfishSystemInfo struct {
					Actions struct {
						Reset struct {
							AllowableValues []string `json:"ResetType@Redfish.AllowableValues"`
						} `json:"#ComputerSystem.Reset"`
					} `json:"Actions"`
				} `json:"RedfishSystemInfo"`
			}
			_ = json.NewDecoder(resp.Body).Decode(&ce)
			resp.Body.Close()
			if len(ce.RedfishSystemInfo.Actions.Reset.AllowableValues) > 0 {
				return
			}
		} else {
			resp.Body.Close()
		}
		time.Sleep(time.Second)
	}
	t.Fatalf("ComponentEndpoint %s did not gain AllowableValues within %v", nodeXname, timeout)
}

// clearReservation drops any stale HSM reservation/lock on xname via SMD's
// admin endpoints. /locks/disable forcibly releases the reservation (and
// flips ReservationDisabled=true); /locks/repair flips it back to false so
// power-control can reserve again on its next attempt.
func clearReservation(ctx context.Context, t *testing.T, smdURL, xname string) {
	t.Helper()
	body, _ := json.Marshal(map[string][]string{"ComponentIDs": {xname}})
	for _, path := range []string{"/hsm/v2/locks/disable", "/hsm/v2/locks/repair"} {
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost,
			smdURL+path, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := httpClient.Do(req)
		if err != nil {
			t.Fatalf("clearReservation %s: %v", path, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("clearReservation %s: HTTP %d", path, resp.StatusCode)
		}
	}
}

// waitRedfishPowerState polls the sim until PowerState matches `want` (csm-rie
// briefly reports PoweringOn/PoweringOff during transitions). Returns the
// last observed value if the deadline elapses without matching.
func waitRedfishPowerState(ctx context.Context, t *testing.T, systemURL, user, pass, want string) string {
	t.Helper()
	// sushy-tools' --fake driver intentionally delays state changes by a
	// randomised 1-11s ("hardware actions are not immediate"). Power-control
	// can report its transition "completed" as soon as the upstream Reset
	// POST succeeded, before the BMC's actual flip lands — so this poll must
	// outlast sushy's worst case + power-control's own status latency.
	deadline := time.Now().Add(45 * time.Second)
	var last string
	for time.Now().Before(deadline) {
		last = redfishPowerState(ctx, t, systemURL, user, pass)
		if last == want {
			return last
		}
		time.Sleep(500 * time.Millisecond)
	}
	return last
}

// pcsTransition is the request body for POST /v1/transitions.
type pcsTransition struct {
	Operation           string         `json:"operation"`
	TaskDeadlineMinutes int            `json:"taskDeadlineMinutes"`
	Location            []pcsXnameOnly `json:"location"`
}
type pcsXnameOnly struct {
	Xname string `json:"xname"`
}

type pcsTransitionResp struct {
	TransitionID     string         `json:"transitionID"`
	Operation        string         `json:"operation"`
	TransitionStatus string         `json:"transitionStatus"`
	transitionStatus string         // mirror, populated by waitTransition for predicate use
	tasks            []pcsTask      // populated only via waitTransition
	Tasks            []pcsTask      `json:"tasks"`
	TaskCounts       map[string]int `json:"taskCounts"`
}
type pcsTask struct {
	Xname                 string `json:"xname"`
	TaskStatus            string `json:"taskStatus"`
	TaskStatusDescription string `json:"taskStatusDescription"`
	Error                 string `json:"error"`
}

func postTransition(ctx context.Context, t *testing.T, powerURL, op, xname string) string {
	t.Helper()
	body, _ := json.Marshal(pcsTransition{
		Operation:           op,
		TaskDeadlineMinutes: 1,
		Location:            []pcsXnameOnly{{Xname: xname}},
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		powerURL+"/v1/transitions", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("build transition request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := httpClient.Do(req)
	if err != nil {
		t.Fatalf("POST transition: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST transition: HTTP %d", resp.StatusCode)
	}
	var tr pcsTransitionResp
	if err := json.NewDecoder(resp.Body).Decode(&tr); err != nil {
		t.Fatalf("decode transition: %v", err)
	}
	if tr.TransitionID == "" {
		t.Fatalf("transition response missing transitionID: %+v", tr)
	}
	return tr.TransitionID
}

func waitTransition(ctx context.Context, t *testing.T, powerURL, id string) pcsTransitionResp {
	t.Helper()
	url := powerURL + "/v1/transitions/" + id
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			t.Fatalf("waitTransition canceled: %v", ctx.Err())
		default:
		}
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		resp, err := httpClient.Do(req)
		if err != nil {
			time.Sleep(time.Second)
			continue
		}
		var tr pcsTransitionResp
		dec := json.NewDecoder(resp.Body)
		_ = dec.Decode(&tr)
		resp.Body.Close()
		switch tr.TransitionStatus {
		case "completed", "abort signaled":
			tr.transitionStatus = tr.TransitionStatus
			tr.tasks = tr.Tasks
			return tr
		}
		time.Sleep(2 * time.Second)
	}
	t.Fatalf("transition %s did not complete within 90s", id)
	return pcsTransitionResp{}
}

// waitForPCSComponent polls power-status until power-control reports a real
// powerState for xname AND its supportedPowerTransitions include the actions
// this test will issue. Without this, a transition POSTed during a stale-
// cache window fails with "Power action not supported for ..." even though
// SMD has the right data.
//
// power-control's periodic refresh runs every ~30s, and after a magellan
// re-discovery (UC5 deletes/re-adds RedfishEndpoints) there can be a window
// where supportedPowerTransitions is empty until the next refresh.
func waitForPCSComponent(ctx context.Context, powerURL, xname string) error {
	url := powerURL + "/v1/power-status?xname=" + xname
	deadline := time.Now().Add(120 * time.Second)
	var lastErr string
	wantOps := map[string]bool{"On": false, "Force-Off": false}
	for time.Now().Before(deadline) {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		resp, err := httpClient.Do(req)
		if err != nil {
			lastErr = err.Error()
			time.Sleep(2 * time.Second)
			continue
		}
		var body struct {
			Status []struct {
				Xname                     string   `json:"xname"`
				PowerState                string   `json:"powerState"`
				Error                     string   `json:"error"`
				SupportedPowerTransitions []string `json:"supportedPowerTransitions"`
			} `json:"status"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&body)
		resp.Body.Close()
		if len(body.Status) == 1 {
			s := body.Status[0]
			if s.Error == "" && s.PowerState != "" {
				ok := true
				for op := range wantOps {
					found := false
					for _, sup := range s.SupportedPowerTransitions {
						if sup == op {
							found = true
							break
						}
					}
					if !found {
						ok = false
						lastErr = fmt.Sprintf("supportedPowerTransitions=%v missing %q", s.SupportedPowerTransitions, op)
						break
					}
				}
				if ok {
					return nil
				}
			} else if s.Error != "" {
				lastErr = s.Error
			}
		}
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf("power-control did not become ready: %s", lastErr)
}

// waitForPCSPowerState polls power-control's /v1/power-status for
// xname until its cached PowerState matches `want`. Power-control's
// sample loop runs every ~30s — issuing a transition before PCS has
// observed the current BMC state lets PCS no-op against its stale
// view (e.g. "already Off") and report `completed` without ever
// contacting the BMC, leaving the actual hardware state untouched.
// This gate is the protocol-level equivalent of "wait for the
// controller to converge on observed reality" before kicking the
// next transition.
//
// Comparison is case-insensitive: Redfish uses TitleCase ("On"/"Off")
// but PCS normalises to lowercase in its /v1/power-status response,
// so callers can pass either form.
//
// Returns the last-seen PowerState (for diagnostics on timeout) and
// nil on success; non-nil error means we hit the deadline.
func waitForPCSPowerState(ctx context.Context, powerURL, xname, want string, timeout time.Duration) (string, error) {
	url := powerURL + "/v1/power-status?xname=" + xname
	deadline := time.Now().Add(timeout)
	wantLower := strings.ToLower(want)
	var last string
	for time.Now().Before(deadline) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return last, err
		}
		resp, err := httpClient.Do(req)
		if err != nil {
			time.Sleep(2 * time.Second)
			continue
		}
		var body struct {
			Status []struct {
				PowerState string `json:"powerState"`
			} `json:"status"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&body)
		resp.Body.Close()
		if len(body.Status) == 1 {
			last = body.Status[0].PowerState
			if strings.EqualFold(last, wantLower) {
				return last, nil
			}
		}
		time.Sleep(2 * time.Second)
	}
	return last, fmt.Errorf("power-control PowerState did not reach %q within %v (last=%q)",
		want, timeout, last)
}

// discoverFirstSystemPath GETs /redfish/v1/Systems and returns the
// path of the first Member (e.g. "/redfish/v1/Systems/<id>"). Used
// because different BMC emulators name their primary System
// differently — hard-coding the path couples the test to the
// emulator's mockup convention.
func discoverFirstSystemPath(ctx context.Context, bmcURL, user, pass string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		bmcURL+"/redfish/v1/Systems", nil)
	if err != nil {
		return "", err
	}
	req.SetBasicAuth(user, pass)
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GET /Systems: HTTP %d", resp.StatusCode)
	}
	var body struct {
		Members []struct {
			OdataID string `json:"@odata.id"`
		} `json:"Members"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", fmt.Errorf("decode /Systems: %w", err)
	}
	if len(body.Members) == 0 {
		return "", fmt.Errorf("empty /Systems collection on %s", bmcURL)
	}
	return body.Members[0].OdataID, nil
}

// redfishReset POSTs a ComputerSystem.Reset action to the BMC sim with the
// given ResetType. Used to put the sim into a known state before a test step
// AND to verify (in cleanup) that we hand off cleanly.
func redfishReset(ctx context.Context, systemURL, user, pass, resetType string) error {
	body, _ := json.Marshal(map[string]string{"ResetType": resetType})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		systemURL+"/Actions/ComputerSystem.Reset", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth(user, pass)
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("redfish reset %s: HTTP %d", resetType, resp.StatusCode)
	}
	return nil
}

// redfishPowerState reads PowerState directly from the BMC sim. This is the
// independent observation channel — it does NOT go through power-control or
// SMD, so it confirms the side effect actually landed at the BMC.
func redfishPowerState(ctx context.Context, t *testing.T, systemURL, user, pass string) string {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, systemURL, nil)
	if err != nil {
		t.Fatalf("build redfish GET: %v", err)
	}
	req.SetBasicAuth(user, pass)
	resp, err := httpClient.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", systemURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s: HTTP %d", systemURL, resp.StatusCode)
	}
	var sys struct {
		PowerState string `json:"PowerState"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&sys); err != nil {
		t.Fatalf("decode PowerState: %v", err)
	}
	return strings.TrimSpace(sys.PowerState)
}
