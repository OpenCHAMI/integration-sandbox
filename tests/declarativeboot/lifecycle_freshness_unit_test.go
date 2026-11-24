//go:build integration && declarative_boot

// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package declarativeboot

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"
)

func TestLifecycleHelpersActualCacheHit(t *testing.T) {
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/bootscript":
			requests.Add(1)
			if _, err := w.Write([]byte("#!ipxe\nkernel /a release=A\ninitrd /a.img\nboot\n")); err != nil {
				t.Error(err)
			}
		case "/metrics":
			hits := requests.Load() - 1
			if hits < 0 {
				hits = 0
			}
			if _, err := fmt.Fprintf(w, "boot_service_declarative_cache_hits_total %d\n", hits); err != nil {
				t.Error(err)
			}
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	client, err := NewHTTPClient(server.URL, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	c := &lifecycleCase{t: t, ctx: context.Background(), boot: BootClient{HTTP: client}, fixtures: Fixtures{Expected: ExpectedFixtures{Artifacts: map[string]Artifact{"A": {Kernel: "/a", Initrd: "/a.img", Params: []string{"release=A"}}, "B": {Kernel: "/b", Initrd: "/b.img", Params: []string{"release=B"}}}}}}
	c.warm(url.Values{"host": {"fixture"}}, "A")
	if requests.Load() != 2 {
		t.Fatal("warm did not perform two boot requests")
	}
}

func TestLifecycleHelpersSMDNIDUpdate(t *testing.T) {
	var nid atomic.Int32
	nid.Store(10)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "PATCH /hsm/v2/State/Components/x1c0s0b0n0/NID":
			var body struct {
				NID int32 `json:"NID"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			nid.Store(body.NID)
			w.WriteHeader(http.StatusNoContent)
		case "GET /hsm/v2/State/Components/x1c0s0b0n0":
			if err := json.NewEncoder(w).Encode(Component{ID: "x1c0s0b0n0", NID: nid.Load()}); err != nil {
				t.Error(err)
			}
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	client, err := NewHTTPClient(server.URL, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	c := &lifecycleCase{t: t, ctx: context.Background(), smd: SMDClient{HTTP: client}}
	changedAt := c.setNID(lifecycleNode{xname: "x1c0s0b0n0", nid: 10}, 20)
	if nid.Load() != 20 || changedAt.IsZero() {
		t.Fatal("NID mutation was not observed")
	}
}

func TestLifecycleHelpersSMDInterfaceMove(t *testing.T) {
	var component atomic.Value
	component.Store("x1c0s0b0n0")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/hsm/v2/Inventory/EthernetInterfaces/020000000001" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		switch r.Method {
		case http.MethodPatch:
			var body struct {
				ComponentID string `json:"ComponentID"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			component.Store(body.ComponentID)
		case http.MethodGet:
			if err := json.NewEncoder(w).Encode(EthernetInterface{ID: "020000000001", ComponentID: component.Load().(string)}); err != nil {
				t.Error(err)
			}
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	defer server.Close()
	client, err := NewHTTPClient(server.URL, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	c := &lifecycleCase{t: t, ctx: context.Background(), smd: SMDClient{HTTP: client}}
	changed := c.moveInterface(lifecycleNode{xname: "x1c0s0b0n0", mac: "02:00:00:00:00:01"}, "x1c0s1b0n0")
	if component.Load().(string) != "x1c0s1b0n0" || changed.IsZero() {
		t.Fatal("SMD identity move did not round-trip")
	}
}
