//go:build integration && declarative_boot

// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package declarativeboot

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestLifecycleHelpersReservedPoolBounds(t *testing.T) {
	for _, count := range []int{0, 9} {
		t.Run(fmt.Sprintf("reject-count-%d", count), func(t *testing.T) {
			if _, err := lifecycleReservedNodes(count, "lifecycle-123456789abcdef0"); err == nil {
				t.Fatalf("count %d accepted", count)
			}
		})
	}
	if _, err := lifecycleReservedNodes(1, "bad"); err == nil {
		t.Fatal("invalid seed accepted")
	}
	nodes, err := lifecycleReservedNodes(8, "lifecycle-123456789abcdef0")
	if err != nil {
		t.Fatal(err)
	}
	xnames, nids, macs := map[string]bool{}, map[int32]bool{}, map[string]bool{}
	xnamePattern := regexp.MustCompile(`^x([0-9]{1,4})c([0-7])s([0-9]+)b([0-9]+)n([0-9]+)$`)
	for count := 1; count <= 8; count++ {
		if values, err := lifecycleReservedNodes(count, "lifecycle-123456789abcdef0"); err != nil || len(values) != count {
			t.Fatalf("count %d failed", count)
		}
	}
	for i, node := range nodes {
		if node.xname != fmt.Sprintf("x1c0s%db0n0", i) || !xnamePattern.MatchString(node.xname) || node.nid != int32(10000+i) {
			t.Fatalf("invalid reserved node %d", i)
		}
		mac, err := net.ParseMAC(node.mac)
		if err != nil || len(mac) != 6 || mac[0]&3 != 2 || xnames[node.xname] || nids[node.nid] || macs[node.mac] {
			t.Fatalf("invalid or duplicate identity at slot %d", i)
		}
		xnames[node.xname], nids[node.nid], macs[node.mac] = true, true, true
	}
	other, err := lifecycleReservedNodes(1, "lifecycle-1234567801234567")
	if err != nil || other[0].mac == nodes[0].mac {
		t.Fatal("case entropy did not change MAC")
	}
}

func TestLifecycleHelpersReservedPoolWire(t *testing.T) {
	var checkedInterface atomic.Bool
	var deleted atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet:
			if strings.HasPrefix(r.URL.Path, "/hsm/v2/Inventory/EthernetInterfaces/") {
				checkedInterface.Store(true)
			}
			w.WriteHeader(http.StatusNotFound)
		case r.Method == http.MethodPost && r.URL.Path == "/hsm/v2/State/Components":
			var body Components
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body.Components) != 1 || body.Components[0].ID != "x1c0s0b0n0" || body.Components[0].NID != 10000 {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPost && r.URL.Path == "/hsm/v2/Inventory/EthernetInterfaces":
			if !checkedInterface.Load() {
				t.Error("interface was created without an absence check")
			}
			w.WriteHeader(http.StatusCreated)
		case r.Method == http.MethodDelete:
			deleted.Add(1)
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)
	t.Cleanup(func() {
		if deleted.Load() != 2 {
			t.Error("owned component and interface were not cleaned up")
		}
	})
	client, err := NewHTTPClient(server.URL, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	c := &lifecycleCase{t: t, ctx: context.Background(), smd: SMDClient{HTTP: client}, series: "lifecycle-123456789abcdef0"}
	nodes := c.nodes(1)
	if len(nodes) != 1 || nodes[0].xname != "x1c0s0b0n0" || nodes[0].nid != 10000 {
		t.Fatal("unexpected reserved fixture identity")
	}
}
