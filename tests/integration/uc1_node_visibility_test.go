//go:build integration
// +build integration

// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
//
// SPDX-License-Identifier: MIT

package integration

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Use case 1: register nodes in SMD; confirm they're visible via boot-service
// and metadata-service.
//
// Important property of the current OpenCHAMI surface (as of 2026-05-03):
// boot-service `/nodes` and metadata-service `/groups` are independent stores;
// they do NOT auto-sync from SMD. The use case therefore covers "register the
// same xname in all three services and confirm each one returns it via its
// own surface" — i.e. the propagation gap is treated as a known property and
// the test asserts manual fan-out works end-to-end.
//
// The cloud-init lookup path (metadata-service /meta-data resolves the caller
// IP via SMD EthernetInterfaces) IS validated end-to-end here, because that
// flow really does cross the SMD/metadata boundary.

const uc1ClusterTag = "uc1-sandbox"

type uc1Node struct {
	Xname string
	NID   int32
	Mac   string
	IP    string
}

var uc1Nodes = []uc1Node{
	{"x0c0s0b0n0", 1000, "02:00:00:00:00:00", "10.252.0.20"},
	{"x0c0s1b0n0", 1001, "02:00:00:00:00:01", "10.252.0.21"},
	{"x0c0s2b0n0", 1002, "02:00:00:00:00:02", "10.252.0.22"},
	{"x0c0s3b0n0", 1003, "02:00:00:00:00:03", "10.252.0.23"},
}

// TestUC1_PopulateAndVerify is a single t.Run-grouped test so we can see exactly
// which step of the flow broke without a stack of helper-only tests.
func TestUC1_PopulateAndVerify(t *testing.T) {
	smdURL := Endpoints["smd"]
	bootURL := Endpoints["boot"]
	metaURL := Endpoints["metadata"]

	// 1. SMD: register components, ethernet interfaces (so X-Forwarded-For lookup works).
	t.Run("smd_register_components", func(t *testing.T) {
		comps := map[string]any{
			"Components": func() []any {
				out := make([]any, 0, len(uc1Nodes))
				for _, n := range uc1Nodes {
					out = append(out, map[string]any{
						"ID": n.Xname, "Type": "Node", "State": "On",
						"Flag": "OK", "Enabled": true, "Role": "Compute", "NID": n.NID,
					})
				}
				return out
			}(),
		}
		code, body := httpJSON(t, http.MethodPost, smdURL+"/hsm/v2/State/Components", comps)
		require.Truef(t, code == 200 || code == 201 || code == 204,
			"register components: HTTP %d body=%s", code, pretty(body))
	})

	t.Run("smd_register_ethernet_interfaces", func(t *testing.T) {
		// Each interface registered separately because SMD's bulk endpoint
		// shape varies between minor versions. Per-IF POSTs are stable.
		for _, n := range uc1Nodes {
			ifce := map[string]any{
				"ID":          strings.ReplaceAll(n.Mac, ":", ""),
				"Description": "uc1 sandbox NIC for " + n.Xname,
				"MACAddress":  n.Mac,
				"IPAddresses": []map[string]any{{"IPAddress": n.IP, "Network": "HMN"}},
				"ComponentID": n.Xname,
				"Type":        "Node",
			}
			code, body := httpJSON(t, http.MethodPost,
				smdURL+"/hsm/v2/Inventory/EthernetInterfaces", ifce)
			require.Truef(t, code == 200 || code == 201 || code == 204 || code == 409,
				"%s ethernet POST: HTTP %d body=%s", n.Xname, code, pretty(body))
		}
	})

	t.Run("smd_lists_each_component", func(t *testing.T) {
		code, body := httpJSON(t, http.MethodGet,
			smdURL+"/hsm/v2/State/Components?type=Node", nil)
		require.Equal(t, http.StatusOK, code)
		var resp struct {
			Components []struct {
				ID string `json:"ID"`
			} `json:"Components"`
		}
		require.NoError(t, json.Unmarshal(body, &resp))
		got := map[string]bool{}
		for _, c := range resp.Components {
			got[c.ID] = true
		}
		for _, n := range uc1Nodes {
			require.Truef(t, got[n.Xname], "xname %s missing from SMD components", n.Xname)
		}
	})

	// 2. boot-service: register a Node entry per xname (hermetic reset first).
	t.Run("boot_service_register_nodes", func(t *testing.T) {
		for _, n := range uc1Nodes {
			bootResetByName(t, bootURL, n.Xname)
			bootCreateNode(t, bootURL, n.Xname, n.Mac, n.NID, []string{uc1ClusterTag})
		}
	})

	t.Run("boot_service_lists_each_node_by_xname", func(t *testing.T) {
		code, body := httpJSON(t, http.MethodGet, bootURL+"/nodes", nil)
		require.Equal(t, http.StatusOK, code)
		var nodes []struct {
			Spec struct {
				Xname string `json:"xname"`
			} `json:"spec"`
		}
		require.NoError(t, json.Unmarshal(body, &nodes))
		got := map[string]bool{}
		for _, n := range nodes {
			got[n.Spec.Xname] = true
		}
		for _, n := range uc1Nodes {
			require.Truef(t, got[n.Xname], "xname %s missing from boot-service /nodes", n.Xname)
		}
	})

	// 3. metadata-service: register a sandbox group whose template depends on `name`,
	//    then resolve a node by IP to confirm the SMD ↔ metadata cloud-init lookup works.
	t.Run("metadata_register_sandbox_group", func(t *testing.T) {
		group := map[string]any{
			"metadata": map[string]any{"name": uc1ClusterTag},
			"spec": map[string]any{
				"description": "uc1 sandbox cluster",
				"template":    "#cloud-config\nhostname: {{ name }}\n",
				"metaData":    map[string]string{"name": "default-host"},
			},
		}
		code, body := httpJSON(t, http.MethodPost, metaURL+"/groups", group)
		require.Truef(t, code == 201 || code == 200 || code == 409,
			"metadata register group: HTTP %d body=%s", code, pretty(body))
	})

	t.Run("metadata_lists_sandbox_group", func(t *testing.T) {
		code, body := httpJSON(t, http.MethodGet, metaURL+"/groups", nil)
		require.Equal(t, http.StatusOK, code)
		require.Contains(t, string(body), `"name":"`+uc1ClusterTag+`"`)
	})

	t.Run("metadata_resolves_node_via_x_forwarded_for", func(t *testing.T) {
		// metadata-service queries SMD by IP to find the xname behind the call.
		// Requires that step 1 (SMD ethernet interface registration) succeeded.
		// Allow a few seconds for SMD's reverse index to settle.
		var lastBody []byte
		var lastCode int
		deadline := time.Now().Add(15 * time.Second)
		for time.Now().Before(deadline) {
			lastCode, lastBody = httpJSONWithHeaders(t, http.MethodGet,
				metaURL+"/user-data",
				map[string]string{"X-Forwarded-For": uc1Nodes[0].IP},
				nil)
			if lastCode == http.StatusOK {
				break
			}
			time.Sleep(1 * time.Second)
		}
		require.Equalf(t, http.StatusOK, lastCode,
			"metadata /user-data via X-Forwarded-For=%s: body=%s",
			uc1Nodes[0].IP, pretty(lastBody))
		require.Contains(t, string(lastBody), "#cloud-config",
			"expected cloud-config payload, got: %s", pretty(lastBody))
	})
}

// uc1Cleanup removes the entries the suite created. Called by the
// teardown step in TestUC1_Cleanup so make ci stays idempotent across runs.
func TestUC1_Cleanup(t *testing.T) {
	if !cleanupEnabled() {
		t.Skip("set SBX_UC_CLEANUP=1 to enable; default keeps fixtures for debugging")
	}
	smdURL := Endpoints["smd"]
	bootURL := Endpoints["boot"]
	metaURL := Endpoints["metadata"]

	for _, n := range uc1Nodes {
		_, _ = httpJSON(t, http.MethodDelete,
			fmt.Sprintf("%s/hsm/v2/State/Components/%s", smdURL, n.Xname), nil)
		bootResetByName(t, bootURL, n.Xname)
	}
	_, _ = httpJSON(t, http.MethodDelete, metaURL+"/groups/"+uc1ClusterTag, nil)
}

func cleanupEnabled() bool {
	return strings.EqualFold(envOr("SBX_UC_CLEANUP", "0"), "1") ||
		strings.EqualFold(envOr("SBX_UC_CLEANUP", "0"), "true")
}
