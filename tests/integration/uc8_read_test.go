//go:build integration

// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package integration

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

// Legacy cloud-init routes are absent from the pinned metadata service.
// These tests establish CLI request/output contracts, not live compatibility.
func TestUC8_LegacyCloudInitReads(t *testing.T) {
	const group = `{"name":"uc8-group","description":"uc8","meta-data":{"sentinel":"uc8"},"file":{"content":"#cloud-config\n","filename":"uc8.yaml","encoding":"plain"}}`
	for _, test := range []struct {
		name, path, response, output string
		args                         []string
		json                         bool
	}{
		{"defaults", "/admin/cluster-defaults", `{"sentinel":"uc8-default"}`, `{"sentinel":"uc8-default"}`, []string{"defaults", "get", "-F", "json"}, true},
		{"group_raw", "/admin/groups/uc8-group", group, "[" + group + "]", []string{"group", "get", "raw", "uc8-group", "-F", "json"}, true},
		{"group_metadata", "/admin/groups/uc8-group", group, `[{"name":"uc8-group","meta-data":{"sentinel":"uc8"}}]`, []string{"group", "get", "meta-data", "uc8-group", "-F", "json"}, true},
		{"node_metadata", "/admin/impersonation/x0c0s0b0n0/meta-data", `{"instance-id":"x0c0s0b0n0","sentinel":"uc8"}`, `[{"instance-id":"x0c0s0b0n0","sentinel":"uc8"}]`, []string{"node", "get", "meta-data", "x0c0s0b0n0", "-F", "json"}, true},
		{"node_userdata", "/admin/impersonation/x0c0s0b0n0/user-data", "#cloud-config\nuc8: true\n", "#cloud-config\nuc8: true\n", []string{"node", "get", "user-data", "x0c0s0b0n0", "--headers", "never"}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				if req.Method != http.MethodGet || req.URL.Path != test.path {
					t.Errorf("unexpected request: %s %s", req.Method, req.URL)
					http.Error(w, "unexpected request", http.StatusBadRequest)
					return
				}
				calls.Add(1)
				w.Header().Set("Content-Type", "application/json")
				if _, err := fmt.Fprint(w, test.response); err != nil {
					t.Errorf("write fixture: %v", err)
				}
			}))
			defer server.Close()
			r := uc8ContractRunner(t)
			args := append([]string{"cloud-init", "--uri", uc8FixtureURL(server.URL), "--no-token"}, test.args...)
			result := r.execute(CLIInput{IgnoreConfig: true}, args...)
			require.NoError(t, result.Err, result.Stderr)
			require.Greater(t, calls.Load(), int32(0))
			if test.json {
				require.JSONEq(t, test.output, result.Stdout)
			} else {
				require.Contains(t, result.Stdout, test.output)
			}
		})
	}
}
