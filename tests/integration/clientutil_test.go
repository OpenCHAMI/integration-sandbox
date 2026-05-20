//go:build integration
// +build integration

// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
//
// SPDX-License-Identifier: MIT

package integration

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"testing"
	"time"
	_ "unsafe" // doc: kept so refactors stay binary-compatible
)

// httpJSON is a generic helper: marshal `body` to JSON, send method+url, return status + raw body.
func httpJSON(t *testing.T, method, url string, body any) (int, []byte) {
	t.Helper()
	var rdr io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		rdr = bytes.NewReader(buf)
	}
	req, err := http.NewRequest(method, url, rdr)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, out
}

// httpJSONWithHeaders adds extra headers (e.g. X-Forwarded-For).
func httpJSONWithHeaders(t *testing.T, method, url string, headers map[string]string, body any) (int, []byte) {
	t.Helper()
	var rdr io.Reader
	if body != nil {
		buf, _ := json.Marshal(body)
		rdr = bytes.NewReader(buf)
	}
	req, _ := http.NewRequest(method, url, rdr)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, out
}

// expectStatus is a small helper that fails loudly with the body when status doesn't match.
func expectStatus(t *testing.T, want int, got int, body []byte, ctx string) {
	t.Helper()
	if got != want {
		t.Fatalf("%s: got HTTP %d (want %d), body=%s", ctx, got, want, string(body))
	}
}

// composeRestart restarts a sandbox service via `docker compose -f ... restart <name>`.
// Tests use this to validate UC3 (restart resilience).
func composeRestart(t *testing.T, service string) {
	t.Helper()
	cmd := exec.Command(
		"docker", "compose",
		"-f", "../../compose/infra.yaml",
		"-f", "../../compose/bmc-sim.yaml",
		"-f", "../../compose/core.yaml",
		"restart", service,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("docker compose restart %s: %v\n%s", service, err, string(out))
	}
}

// waitFor polls fn until it returns nil or timeout. Useful after composeRestart.
func waitFor(t *testing.T, what string, timeout time.Duration, fn func() error) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		if err := fn(); err == nil {
			return
		} else {
			lastErr = err
		}
		time.Sleep(2 * time.Second)
	}
	t.Fatalf("waitFor %s timed out after %s: %v", what, timeout, lastErr)
}

// pretty renders a small JSON snippet for fail messages without dragging in extra deps.
func pretty(b []byte) string {
	s := string(b)
	if len(s) > 400 {
		s = s[:400] + "...(truncated)"
	}
	return strings.ReplaceAll(s, "\n", " ")
}

// errorf returns a formatted error usable inside waitFor's fn.
func errorf(format string, a ...any) error {
	return fmt.Errorf(format, a...)
}

// bootNodeUIDsByName returns every uid for entries whose metadata.name matches `name`.
// boot-service does NOT enforce name uniqueness, so a re-run without cleanup
// leaves duplicates; tests must reset by name, not just upsert.
func bootNodeUIDsByName(t *testing.T, bootURL, name string) []string {
	t.Helper()
	code, body := httpJSON(t, http.MethodGet, bootURL+"/nodes", nil)
	if code != http.StatusOK {
		t.Fatalf("list nodes: HTTP %d body=%s", code, pretty(body))
	}
	var arr []struct {
		Metadata struct {
			Name string `json:"name"`
			UID  string `json:"uid"`
		} `json:"metadata"`
	}
	if err := json.Unmarshal(body, &arr); err != nil {
		t.Fatalf("decode nodes list: %v body=%s", err, pretty(body))
	}
	out := []string{}
	for _, n := range arr {
		if n.Metadata.Name == name {
			out = append(out, n.Metadata.UID)
		}
	}
	return out
}

// bootResetByName deletes every node entry that shares `name`, leaving the
// boot-service state hermetic with respect to that name. Idempotent.
func bootResetByName(t *testing.T, bootURL, name string) {
	t.Helper()
	for _, uid := range bootNodeUIDsByName(t, bootURL, name) {
		code, body := httpJSON(t, http.MethodDelete, bootURL+"/nodes/"+uid, nil)
		if code != 200 && code != 204 && code != 404 {
			t.Fatalf("delete %s/%s: HTTP %d body=%s", name, uid, code, pretty(body))
		}
	}
}

// bootCreateNode posts a fresh node. Caller is responsible for ensuring no
// duplicate exists (use bootResetByName first).
func bootCreateNode(t *testing.T, bootURL, xname, mac string, nid int32, groups []string) {
	t.Helper()
	body := map[string]any{
		"metadata": map[string]any{"name": xname},
		"spec": map[string]any{
			"xname":    xname,
			"hostname": xname,
			"bootMac":  mac,
			"role":     "Compute",
			"nid":      nid,
			"groups":   groups,
		},
	}
	code, respBody := httpJSON(t, http.MethodPost, bootURL+"/nodes", body)
	if code != 201 && code != 200 {
		t.Fatalf("create %s: HTTP %d body=%s", xname, code, pretty(respBody))
	}
}

// bootSetGroups updates the groups for the node uniquely identified by `name`.
// Caller must guarantee a single entry exists for that name.
func bootSetGroups(t *testing.T, bootURL, name string, groups []string) {
	t.Helper()
	uids := bootNodeUIDsByName(t, bootURL, name)
	if len(uids) != 1 {
		t.Fatalf("expected exactly one node for %s, got %d", name, len(uids))
	}
	uid := uids[0]
	// fetch current spec to preserve other fields
	code, body := httpJSON(t, http.MethodGet, bootURL+"/nodes/"+uid, nil)
	if code != http.StatusOK {
		t.Fatalf("GET %s: HTTP %d body=%s", uid, code, pretty(body))
	}
	var current struct {
		Metadata map[string]any `json:"metadata"`
		Spec     map[string]any `json:"spec"`
	}
	if err := json.Unmarshal(body, &current); err != nil {
		t.Fatalf("decode current %s: %v", uid, err)
	}
	current.Spec["groups"] = groups
	put := map[string]any{"metadata": current.Metadata, "spec": current.Spec}
	code, body = httpJSON(t, http.MethodPut, bootURL+"/nodes/"+uid, put)
	if code != 200 && code != 204 {
		t.Fatalf("PUT %s: HTTP %d body=%s", uid, code, pretty(body))
	}
}
