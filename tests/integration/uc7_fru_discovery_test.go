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

// TestUC7_FruTracker_Reconciler proves the fru-tracker reconciler ingests a
// DiscoverySnapshot and produces a Device tree with parent/child relationships
// resolved from serial-number references. Specifically:
//
//  1. POST /discoverysnapshots with a synthetic payload describing 8 nodes,
//     each with one CPU and two DIMMs, where each child references its
//     parent by serialNumber (no UIDs known to the client). 32 devices total
//     (8 parents + 24 children).
//  2. Capture the snapshot UID returned from the POST.
//  3. Poll /discoverysnapshots/<uid> until status.phase=Completed and
//     status.ready=true. The reconciler runs asynchronously in a worker pool
//     so the POST returns before reconciliation finishes.
//  4. GET /devices and find every device written by THIS snapshot (matched
//     by a per-run serial-number prefix to keep the test idempotent against
//     prior runs and other tests).
//  5. Verify every child has a non-empty spec.parentID, and that parentID
//     equals the UID of the device whose serialNumber matched the child's
//     spec.parentSerialNumber. This is the cross-service half: the
//     reconciler converted serial-number references into UID linkages.
//
// Stub-resistance: cannot pass against a wiremock or canned-response stub
// because step 5 reads back persistent state that the reconciler must have
// computed (UID generation + parent resolution) and stored. Step 4's count
// also requires the SQLite-backed Ent storage to actually persist 32 records
// from the POST.
//
// Note on scope: fru-tracker is a one-way sink in the current sandbox — it
// writes to its own SQLite store and does NOT propagate inventory back to
// SMD's /hsm/v2/Inventory/Hardware. The cross-service half stops at the
// reconciler. If/when fru-tracker grows an SMD writer, extend this test to
// verify the same tree appears in SMD.
func TestUC7_FruTracker_Reconciler(t *testing.T) {
	fruURL := Endpoints["fru"]

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Per-run prefix keeps this test idempotent against repeats and against
	// the other UCs that may have written devices of their own.
	runTag := fmt.Sprintf("uc7-%d", time.Now().UnixNano())

	// fru-tracker's reconciler keys devices by both serialNumber AND a
	// `redfish_uri` property (it skips any device without one), so every
	// synthetic spec must carry both. The URI doesn't have to point at a
	// real Redfish endpoint — only its uniqueness matters here.
	type rawSpec struct {
		DeviceType         string            `json:"deviceType"`
		SerialNumber       string            `json:"serialNumber"`
		PartNumber         string            `json:"partNumber,omitempty"`
		Manufacturer       string            `json:"manufacturer,omitempty"`
		ParentSerialNumber string            `json:"parentSerialNumber,omitempty"`
		Properties         map[string]string `json:"properties,omitempty"`
	}
	uri := func(parts ...string) map[string]string {
		return map[string]string{"redfish_uri": "/" + strings.Join(parts, "/")}
	}

	var raw []rawSpec
	parentSerials := make([]string, 0, 8)
	for n := 0; n < 8; n++ {
		nodeSerial := fmt.Sprintf("%s-node-%d", runTag, n)
		parentSerials = append(parentSerials, nodeSerial)
		raw = append(raw, rawSpec{
			DeviceType:   "Node",
			SerialNumber: nodeSerial,
			Manufacturer: "OpenCHAMI",
			Properties:   uri(runTag, "Systems", nodeSerial),
		})
		raw = append(raw, rawSpec{
			DeviceType:         "CPU",
			SerialNumber:       fmt.Sprintf("%s-cpu-%d", runTag, n),
			PartNumber:         "Xeon-Sandbox",
			ParentSerialNumber: nodeSerial,
			Properties:         uri(runTag, "Systems", nodeSerial, "Processors", fmt.Sprintf("CPU%d", n)),
		})
		for d := 0; d < 2; d++ {
			raw = append(raw, rawSpec{
				DeviceType:         "DIMM",
				SerialNumber:       fmt.Sprintf("%s-dimm-%d-%d", runTag, n, d),
				PartNumber:         "32GB-DDR5",
				ParentSerialNumber: nodeSerial,
				Properties:         uri(runTag, "Systems", nodeSerial, "Memory", fmt.Sprintf("DIMM%d", d)),
			})
		}
	}
	const expectedDevices = 8 + 8 + 16 // nodes + cpus + dimms
	if got := len(raw); got != expectedDevices {
		t.Fatalf("test bug: built %d device specs, expected %d", got, expectedDevices)
	}

	rawJSON, err := json.Marshal(raw)
	if err != nil {
		t.Fatalf("marshal raw: %v", err)
	}

	type meta struct {
		Name string `json:"name"`
	}
	type snapshotSpec struct {
		RawData json.RawMessage `json:"rawData"`
	}
	type snapshot struct {
		APIVersion string       `json:"apiVersion"`
		Kind       string       `json:"kind"`
		Metadata   meta         `json:"metadata"`
		Spec       snapshotSpec `json:"spec"`
	}
	body, _ := json.Marshal(snapshot{
		APIVersion: "example.fabrica.dev/v1",
		Kind:       "DiscoverySnapshot",
		Metadata:   meta{Name: runTag},
		Spec:       snapshotSpec{RawData: rawJSON},
	})

	// Step 1+2: POST and capture UID.
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		fruURL+"/discoverysnapshots", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("build POST: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := httpClient.Do(req)
	if err != nil {
		t.Fatalf("POST snapshot: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		t.Fatalf("POST snapshot: HTTP %d", resp.StatusCode)
	}
	var created struct {
		Metadata struct {
			UID string `json:"uid"`
		} `json:"metadata"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatalf("decode created snapshot: %v", err)
	}
	uid := strings.TrimSpace(created.Metadata.UID)
	if uid == "" {
		t.Fatalf("POST response missing metadata.uid")
	}

	// Cleanup: delete the snapshot we created. fru-tracker doesn't cascade-
	// delete devices, so leftovers are unavoidable today, but at least a
	// re-run won't re-create snapshots with our tag.
	t.Cleanup(func() {
		dctx, dcancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer dcancel()
		dreq, _ := http.NewRequestWithContext(dctx, http.MethodDelete,
			fruURL+"/discoverysnapshots/"+uid, nil)
		if r, err := httpClient.Do(dreq); err == nil {
			r.Body.Close()
		}
	})

	// Step 3: poll until reconciler reports completion.
	if err := waitSnapshotCompleted(ctx, fruURL, uid, 30*time.Second); err != nil {
		t.Fatalf("snapshot %s: %v", uid, err)
	}

	// Step 4+5: read /devices, filter to this run's tag, validate the tree.
	// Build the set of serials we POSTed so we can tell exactly which device
	// is missing if the count is off — fru-tracker has been observed to
	// intermittently drop one device under concurrent reconciliation, so
	// pinpointing the loser is more useful than a bare count.
	expectedSerials := map[string]string{} // serial -> deviceType
	for _, s := range raw {
		expectedSerials[s.SerialNumber] = s.DeviceType
	}
	devs := getDevices(ctx, t, fruURL)
	mine := map[string]device{} // uid -> device
	bySerial := map[string]device{}
	for _, d := range devs {
		if !strings.HasPrefix(d.Spec.SerialNumber, runTag+"-") {
			continue
		}
		mine[d.Metadata.UID] = d
		bySerial[d.Spec.SerialNumber] = d
	}
	if got := len(mine); got != expectedDevices {
		var missing []string
		for s, dt := range expectedSerials {
			if _, ok := bySerial[s]; !ok {
				missing = append(missing, fmt.Sprintf("%s (%s)", s, dt))
			}
		}
		t.Fatalf("expected %d devices for run %q, got %d; missing serials: %v",
			expectedDevices, runTag, got, missing)
	}

	parentUIDByName := map[string]string{}
	for _, p := range parentSerials {
		dev, ok := bySerial[p]
		if !ok {
			t.Errorf("parent device with serial %q missing", p)
			continue
		}
		if dev.Spec.DeviceType != "Node" {
			t.Errorf("parent %s: expected deviceType=Node, got %q", p, dev.Spec.DeviceType)
		}
		if dev.Spec.ParentID != "" {
			t.Errorf("parent %s: should have no ParentID, got %q", p, dev.Spec.ParentID)
		}
		parentUIDByName[p] = dev.Metadata.UID
	}

	childCount := 0
	for _, d := range mine {
		if d.Spec.ParentSerialNumber == "" {
			continue
		}
		childCount++
		wantParentUID, ok := parentUIDByName[d.Spec.ParentSerialNumber]
		if !ok {
			t.Errorf("child %s references unknown parent serial %q",
				d.Spec.SerialNumber, d.Spec.ParentSerialNumber)
			continue
		}
		if d.Spec.ParentID == "" {
			t.Errorf("child %s: ParentID is empty after reconcile", d.Spec.SerialNumber)
			continue
		}
		if d.Spec.ParentID != wantParentUID {
			t.Errorf("child %s: ParentID=%q, want %q (parent serial %q)",
				d.Spec.SerialNumber, d.Spec.ParentID, wantParentUID, d.Spec.ParentSerialNumber)
		}
	}
	if got := childCount; got != 24 {
		t.Errorf("expected 24 child devices linked to parents, got %d", got)
	}
}

type device struct {
	Metadata struct {
		Name string `json:"name"`
		UID  string `json:"uid"`
	} `json:"metadata"`
	Spec struct {
		DeviceType         string `json:"deviceType"`
		SerialNumber       string `json:"serialNumber"`
		ParentSerialNumber string `json:"parentSerialNumber,omitempty"`
		ParentID           string `json:"parentID,omitempty"`
	} `json:"spec"`
}

func getDevices(ctx context.Context, t *testing.T, fruURL string) []device {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fruURL+"/devices", nil)
	if err != nil {
		t.Fatalf("build GET /devices: %v", err)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		t.Fatalf("GET /devices: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /devices: HTTP %d", resp.StatusCode)
	}
	var devs []device
	if err := json.NewDecoder(resp.Body).Decode(&devs); err != nil {
		t.Fatalf("decode /devices: %v", err)
	}
	return devs
}

func waitSnapshotCompleted(ctx context.Context, fruURL, uid string, timeout time.Duration) error {
	url := fruURL + "/discoverysnapshots/" + uid
	deadline := time.Now().Add(timeout)
	var lastPhase string
	for time.Now().Before(deadline) {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		resp, err := httpClient.Do(req)
		if err != nil {
			time.Sleep(500 * time.Millisecond)
			continue
		}
		var snap struct {
			Status struct {
				Phase string `json:"phase"`
				Ready bool   `json:"ready"`
			} `json:"status"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&snap)
		resp.Body.Close()
		lastPhase = snap.Status.Phase
		if snap.Status.Phase == "Completed" && snap.Status.Ready {
			return nil
		}
		if snap.Status.Phase == "Error" {
			return fmt.Errorf("reconciler reported phase=Error")
		}
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("snapshot did not reach status.ready=true within %v (last phase=%q)", timeout, lastPhase)
}
