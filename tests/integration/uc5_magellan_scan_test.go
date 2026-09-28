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
	// the BMC ID map (BMC-sim container IP → xname), scans
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
// invocation: scan → collect (fed from the scan cache) → send. The shell
// snippet is deliberately inlined rather than carried in a fixture file
// because:
//   - The id-map references the 8 fixture xnames specifically; if the fixture
//     count changes, this snippet must change too — co-locating keeps the
//     two in sync without a third source-of-truth file.
//   - The runner overrides the entrypoint to /magellan, so we have to
//     re-override to sh -c to chain three subcommands. Inlining makes that
//     explicit at the call site.
//
// Two details here are load-bearing and easy to regress:
//
//   - The id-map is keyed by the BMC sims' *container IPs* (see
//     bmcIDMapJSON), not by xname: magellan ≥ v0.6.0 accepts exactly one
//     map_key, "bmc-ip-addr", and looks it up by the target's resolved
//     IPv4 — whereas v0.5.1 used the scan address (the hostname), which is
//     why xname keys worked there. The scan itself still targets the
//     x0c0sNb0 aliases, so FQDN/Hostname stay xname-shaped.
//   - collect runs with stdin left alone: magellan ≥ v0.6.1 honors --cache
//     regardless of stdin (OpenCHAMI/magellan#189), so the `< /dev/null`
//     redirect that pre-v0.6.1 builds needed — a non-TTY `docker compose
//     run` hands the container a fifo that IsStdinEmpty() misread as
//     piped data, making collect find nothing and exit 1 — is gone. The
//     image pin (images/default.env) must stay ≥ v0.6.1 for that to hold.
func runMagellanPipeline(ctx context.Context, t *testing.T) {
	t.Helper()

	idMapJSON := bmcIDMapJSON(t)

	script := fmt.Sprintf(`set -e
printf '%%s\n' '%s' > /tmp/idmap.json
/magellan scan https://x0c0s0b0 https://x0c0s1b0 https://x0c0s2b0 https://x0c0s3b0 https://x0c0s4b0 https://x0c0s5b0 https://x0c0s6b0 https://x0c0s7b0 --cache /tmp/assets.db -i
stat /tmp/assets.db
/magellan collect --cache /tmp/assets.db -u root -p root_password -o /tmp/inventory.json --bmc-id-map @/tmp/idmap.json -i
stat /tmp/inventory.json
/magellan send -d @/tmp/inventory.json http://smd:27779 --force-update
`, idMapJSON)

	cmd := exec.CommandContext(ctx,
		"docker", "compose",
		"-f", "../../compose/infra.yaml",
		"-f", "../../compose/bmc-sim.yaml",
		"-f", "../../compose/core.yaml",
		"run", "--rm", "--remove-orphans",
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

// bmcIDMapJSON returns magellan's `--bmc-id-map` document with the BMC sims'
// container IPs as keys and their xnames as values.
//
// magellan ≥ v0.6.0 validates that map_key is "bmc-ip-addr" and resolves the
// selector to the target's IPv4, so the map has to be keyed by address. The
// compose network hands those out dynamically (and the magellan image ships
// no resolver of its own — no getent/nslookup), so Docker is asked directly:
// its answer for the sandbox-redfish-* containers is exactly what the
// x0c0sNb0 aliases resolve to from inside the compose network, i.e. the same
// value magellan will look up.
//
// Each line of `docker inspect` output carries the container's hostname too,
// so a fixture/compose drift (container renamed, index shifted) fails loudly
// instead of silently producing an id-map that never matches.
func bmcIDMapJSON(t *testing.T) string {
	t.Helper()

	args := []string{"inspect", "-f",
		`{{.Config.Hostname}} {{(index .NetworkSettings.Networks "openchami-sandbox").IPAddress}}`}
	for i := range Xnames {
		args = append(args, fmt.Sprintf("sandbox-redfish-%d", i))
	}
	out, err := exec.Command("docker", args...).CombinedOutput()
	if err != nil {
		t.Fatalf("docker inspect (BMC sim IPs): %v\noutput:\n%s", err, string(out))
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) != len(Xnames) {
		t.Fatalf("expected %d BMC sims from docker inspect, got %d:\n%s",
			len(Xnames), len(lines), string(out))
	}

	var b strings.Builder
	b.WriteString(`{"map_key":"bmc-ip-addr","id_map":{`)
	for i, line := range lines {
		fields := strings.Fields(line)
		if len(fields) != 2 || fields[1] == "" {
			t.Fatalf("BMC sim %d: cannot read hostname/IP from docker inspect line %q (is the stack up?)",
				i, line)
		}
		hostname, ip := fields[0], fields[1]
		if hostname != Xnames[i] {
			t.Fatalf("container sandbox-redfish-%d reports hostname %q, want %q — fixture and compose have drifted",
				i, hostname, Xnames[i])
		}
		if i > 0 {
			b.WriteString(",")
		}
		fmt.Fprintf(&b, "%q:%q", ip, hostname)
	}
	b.WriteString("}}")
	return b.String()
}
