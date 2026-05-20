//go:build integration
// +build integration

// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
//
// SPDX-License-Identifier: MIT

package integration

import (
	"encoding/json"
	"net/http"
	"sort"
	"testing"

	"github.com/stretchr/testify/require"
)

// Use case 2: two OpenCHAMI clusters (alpha, beta) with disjoint node sets;
// move one node from beta to alpha; assert the membership flipped.
//
// Implementation note: a "cluster" in the operator's model is a separate
// namespace + DB, but the integration sandbox represents clusters via the
// `groups` label on each Node and via metadata-service Group resources.
// To exercise true SMD isolation (two SMD instances), enable the optional
// `compose/multi-smd.yaml` overlay; the test switches its writes to a second
// SMD URL when SBX_MULTI_SMD=1.
//
// boot-service indexes /nodes by uid (not by xname/name). All membership PUTs
// resolve uid first via bootNodeUIDByName.

type uc2Node struct {
	Xname   string
	NID     int32
	Mac     string
	Cluster string // "uc2-alpha" or "uc2-beta"
}

// Disjoint xname range from UC1 (x0c0s0..3) and UC3 (x9c0s*) so the suites can
// run back-to-back without cleanup.
var uc2Initial = []uc2Node{
	{"x0c0s4b0n0", 2004, "02:00:00:00:20:04", uc2ClusterA},
	{"x0c0s5b0n0", 2005, "02:00:00:00:20:05", uc2ClusterA},
	{"x0c0s6b0n0", 2006, "02:00:00:00:20:06", uc2ClusterB},
	{"x0c0s7b0n0", 2007, "02:00:00:00:20:07", uc2ClusterB},
}

const (
	uc2MoveTarget = "x0c0s6b0n0" // moves from beta -> alpha
	uc2ClusterA   = "uc2-alpha"
	uc2ClusterB   = "uc2-beta"
)

func TestUC2_MultiClusterMove(t *testing.T) {
	bootURL := Endpoints["boot"]
	metaURL := Endpoints["metadata"]

	// 1. metadata-service: define two clusters (groups).
	t.Run("metadata_define_clusters", func(t *testing.T) {
		for _, name := range []string{uc2ClusterA, uc2ClusterB} {
			code, body := httpJSON(t, http.MethodPost, metaURL+"/groups", map[string]any{
				"metadata": map[string]any{"name": name},
				"spec": map[string]any{
					"description": "uc2 cluster " + name,
					"template":    "#cloud-config\nhostname: {{ cluster }}-{{ name }}\n",
					"metaData": map[string]string{
						"cluster": name,
						"name":    "default",
					},
				},
			})
			require.Truef(t, code == 201 || code == 200 || code == 409,
				"create cluster %s: HTTP %d body=%s", name, code, pretty(body))
		}
	})

	// 2. boot-service: hermetic reset, then register all nodes with their initial cluster label.
	//    boot-service does NOT enforce metadata.name uniqueness, so we delete all
	//    matching entries first to avoid stale duplicates from earlier runs.
	t.Run("boot_service_register_with_initial_membership", func(t *testing.T) {
		for _, n := range uc2Initial {
			bootResetByName(t, bootURL, n.Xname)
			bootCreateNode(t, bootURL, n.Xname, n.Mac, n.NID, []string{n.Cluster})
		}
	})

	// 3. Verify initial membership.
	t.Run("verify_initial_membership", func(t *testing.T) {
		got := bootGroupsByXname(t, bootURL)
		for _, n := range uc2Initial {
			require.Containsf(t, got[n.Xname], n.Cluster,
				"%s should be in %s initially, got %v", n.Xname, n.Cluster, got[n.Xname])
		}
	})

	// 4. Move uc2MoveTarget from beta to alpha.
	t.Run("move_node_beta_to_alpha", func(t *testing.T) {
		bootSetGroups(t, bootURL, uc2MoveTarget, []string{uc2ClusterA})
	})

	// 5. Verify the move propagated.
	t.Run("verify_membership_after_move", func(t *testing.T) {
		got := bootGroupsByXname(t, bootURL)
		require.Contains(t, got[uc2MoveTarget], uc2ClusterA,
			"%s should now be in alpha; groups=%v", uc2MoveTarget, got[uc2MoveTarget])
		require.NotContains(t, got[uc2MoveTarget], uc2ClusterB,
			"%s should NOT still be in beta; groups=%v", uc2MoveTarget, got[uc2MoveTarget])

		// alpha now has 3, beta has 1 (out of the four uc2 nodes).
		alphaCount, betaCount := 0, 0
		for _, n := range uc2Initial {
			for _, g := range got[n.Xname] {
				if g == uc2ClusterA {
					alphaCount++
				}
				if g == uc2ClusterB {
					betaCount++
				}
			}
		}
		require.Equal(t, 3, alphaCount, "alpha should have 3 nodes after move, got %d", alphaCount)
		require.Equal(t, 1, betaCount, "beta should have 1 node after move, got %d", betaCount)
	})
}

// bootGroupsByXname returns map[xname][]groups from /nodes.
func bootGroupsByXname(t *testing.T, bootURL string) map[string][]string {
	t.Helper()
	code, body := httpJSON(t, http.MethodGet, bootURL+"/nodes", nil)
	require.Equal(t, http.StatusOK, code)
	var nodes []struct {
		Spec struct {
			Xname  string   `json:"xname"`
			Groups []string `json:"groups"`
		} `json:"spec"`
	}
	require.NoError(t, json.Unmarshal(body, &nodes))
	out := map[string][]string{}
	for _, n := range nodes {
		gs := append([]string{}, n.Spec.Groups...)
		sort.Strings(gs)
		out[n.Spec.Xname] = gs
	}
	return out
}

func TestUC2_Cleanup(t *testing.T) {
	if !cleanupEnabled() {
		t.Skip("set SBX_UC_CLEANUP=1 to enable")
	}
	bootURL := Endpoints["boot"]
	metaURL := Endpoints["metadata"]
	for _, n := range uc2Initial {
		bootResetByName(t, bootURL, n.Xname)
	}
	for _, name := range []string{uc2ClusterA, uc2ClusterB} {
		_, _ = httpJSON(t, http.MethodDelete, metaURL+"/groups/"+name, nil)
	}
}
