//go:build integration
// +build integration

// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
//
// SPDX-License-Identifier: MIT

package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// TestUC5_Magellan_SMD covers the headline magellan → SMD flow:
//
//  1. Snapshot SMD's RedfishEndpoints baseline (whatever count is currently
//     present — the seed fixture writes /State/Components but not /Inventory/
//     RedfishEndpoints, so a fresh stack starts at 0).
//  2. Run the canonical magellan scan → collect → send pipeline against the
//     8 CSM-RIE BMC sims (`x0c0s{0..7}b0`, hostname-aliased on the docker
//     network per compose/bmc-sim.yaml). Use `docker compose run --rm
//     magellan-runner` so we exercise the same one-shot helper that the
//     sandbox documents (compose/core.yaml:139).
//  3. Verify SMD's /hsm/v2/Inventory/RedfishEndpoints now contains all 8
//     xnames with the User claim equal to "root" — this proves the data
//     produced by `magellan collect` (which queries each Redfish service
//     for live inventory) round-tripped through `magellan send` into a real
//     SMD that persisted it and can serve it back via independent GET.
//
// Stub-resistance: would fail against any wiremock or canned-response stub
// because the assertion compares specific xname IDs and User fields produced
// dynamically by the scan, not pre-canned JSON, and the SMD round-trip
// requires real postgres-backed persistence.
func TestUC5_Magellan_SMD(t *testing.T) {
	smdURL := Endpoints["smd"]

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	wantXnames := []string{
		"x0c0s0b0", "x0c0s1b0", "x0c0s2b0", "x0c0s3b0",
		"x0c0s4b0", "x0c0s5b0", "x0c0s6b0", "x0c0s7b0",
	}

	// Setup-time cleanup: wipe any prior RedfishEndpoints so UC5's
	// baseline is deterministic. We intentionally do NOT register
	// `cleanup` as a t.Cleanup hook — that used to be there for "test
	// hygiene" but in practice it destroyed state subsequent tests
	// rely on (UC6's power-control flow needs the populated
	// ComponentEndpoints magellan-send writes; power-control caches
	// HSMData and only refreshes by add — empty-RfFQDN entries get
	// stuck once written, so transient deletion of RedfishEndpoints
	// permanently breaks downstream power-status reads until
	// power-control restarts). Leaving UC5's post-magellan state in
	// place is the kinder default for the suite; UC5's own
	// idempotency on re-run is preserved by the setup-time wipe just
	// below.
	cleanup := func() {
		for _, xn := range wantXnames {
			req, _ := http.NewRequestWithContext(ctx, http.MethodDelete,
				smdURL+"/hsm/v2/Inventory/RedfishEndpoints/"+xn, nil)
			if r, err := httpClient.Do(req); err == nil {
				r.Body.Close()
			}
		}
	}
	cleanup()

	// Step 1: baseline. After cleanup we expect 0; if a future seed adds
	// pre-existing endpoints, this test still works as long as the count
	// after the magellan run is baseline + 8.
	baseline := redfishEndpointCount(ctx, t, smdURL)

	// Step 2: run the canonical magellan pipeline. The script bootstraps
	// the BMC ID map (xname→xname for our hostname-aliased BMCs), scans
	// the 8 hosts, collects inventory, and POSTs the result to SMD.
	runMagellanPipeline(ctx, t)

	// Step 3: SMD now reflects the discovered endpoints.
	endpoints := getRedfishEndpoints(ctx, t, smdURL)
	if got := len(endpoints); got != baseline+len(wantXnames) {
		t.Fatalf("expected %d RedfishEndpoints (baseline %d + %d magellan-discovered), got %d",
			baseline+len(wantXnames), baseline, len(wantXnames), got)
	}
	got := map[string]redfishEndpoint{}
	for _, e := range endpoints {
		got[e.ID] = e
	}
	for _, xn := range wantXnames {
		e, ok := got[xn]
		if !ok {
			t.Errorf("expected SMD to have RedfishEndpoint %q after magellan run, got IDs=%v", xn, sortedKeys(got))
			continue
		}
		if e.User != "root" {
			t.Errorf("RedfishEndpoint %q: expected User=root, got %q", xn, e.User)
		}
		if e.FQDN == "" {
			t.Errorf("RedfishEndpoint %q: expected non-empty FQDN", xn)
		}
	}
}

// redfishEndpoint mirrors the subset of fields SMD returns from
// /hsm/v2/Inventory/RedfishEndpoints we assert on. Other fields are ignored;
// SMD's full schema includes Type, MACAddr, Discoverable, etc.
type redfishEndpoint struct {
	ID   string `json:"ID"`
	FQDN string `json:"FQDN"`
	User string `json:"User"`
}

// redfishEndpointsResponse matches SMD's wrapper shape `{"RedfishEndpoints": [...]}`.
type redfishEndpointsResponse struct {
	RedfishEndpoints []redfishEndpoint `json:"RedfishEndpoints"`
}

func getRedfishEndpoints(ctx context.Context, t *testing.T, smdURL string) []redfishEndpoint {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		smdURL+"/hsm/v2/Inventory/RedfishEndpoints", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		t.Fatalf("GET RedfishEndpoints: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET RedfishEndpoints: HTTP %d", resp.StatusCode)
	}
	var out redfishEndpointsResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode RedfishEndpoints: %v", err)
	}
	return out.RedfishEndpoints
}

func redfishEndpointCount(ctx context.Context, t *testing.T, smdURL string) int {
	t.Helper()
	return len(getRedfishEndpoints(ctx, t, smdURL))
}

func sortedKeys(m map[string]redfishEndpoint) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	// Tests don't actually need stable order; this helper is for failure
	// diagnostics only, where readability beats determinism.
	return out
}

// runMagellanPipeline runs the documented one-shot magellan-runner
// invocation. The shell snippet is deliberately inlined rather than carried
// in a fixture file because:
//   - The id-map references the 8 fixture xnames specifically; if the fixture
//     count changes, this snippet must change too — co-locating keeps the
//     two in sync without a third source-of-truth file.
//   - The runner overrides the entrypoint to /magellan, so we have to
//     re-override to sh -c to chain three subcommands. Inlining makes that
//     explicit at the call site.
func runMagellanPipeline(ctx context.Context, t *testing.T) {
	t.Helper()

	// Heredoc wrapping for the id-map JSON. Embedded as a sh script so the
	// scan + collect + send chain runs in a single container with a shared
	// /tmp.
	const idMapJSON = `{"map_key":"bmc-ip-addr","id_map":{"x0c0s0b0":"x0c0s0b0","x0c0s1b0":"x0c0s1b0","x0c0s2b0":"x0c0s2b0","x0c0s3b0":"x0c0s3b0","x0c0s4b0":"x0c0s4b0","x0c0s5b0":"x0c0s5b0","x0c0s6b0":"x0c0s6b0","x0c0s7b0":"x0c0s7b0"}}`

	script := fmt.Sprintf(`set -e
printf '%%s\n' '%s' > /tmp/idmap.json
/magellan scan https://x0c0s0b0 https://x0c0s1b0 https://x0c0s2b0 https://x0c0s3b0 https://x0c0s4b0 https://x0c0s5b0 https://x0c0s6b0 https://x0c0s7b0 --cache /tmp/assets.db -i
/magellan collect --cache /tmp/assets.db -u root -p root_password -o /tmp/inventory.json --cacert '' --bmc-id-map @/tmp/idmap.json
/magellan send -d @/tmp/inventory.json http://smd:27779 --force-update
`, idMapJSON)

	cmd := exec.CommandContext(ctx,
		"docker", "compose",
		"-f", "../../compose/infra.yaml",
		"-f", "../../compose/bmc-sim.yaml",
		"-f", "../../compose/core.yaml",
		"run", "--rm",
		"--entrypoint", "sh",
		"magellan-runner",
		"-c", script,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("magellan pipeline failed: %v\noutput:\n%s", err, string(out))
	}
	// magellan emits a noisy "config file not found" warning that's
	// harmless. Surface its real signals only when the test fails (above).
	_ = strings.TrimSpace(string(out))
}
