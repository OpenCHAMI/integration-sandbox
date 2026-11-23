//go:build integration && declarative_boot

// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package declarativeboot

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestSMDHelpersComponentAndInterface(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/hsm/v2/State/Components":
			var body Components
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body.Components) != 1 || body.Components[0].ID != "x0c0s0b0n0" || body.Components[0].NID != 1000 {
				t.Errorf("bad component request: %+v %v", body, err)
			}
		case "/hsm/v2/Inventory/EthernetInterfaces":
			var body EthernetInterface
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.ComponentID != "x0c0s0b0n0" || body.MACAddress != "02:00:00:00:00:00" {
				t.Errorf("bad interface request: %+v %v", body, err)
			}
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(404)
			return
		}
		w.WriteHeader(201)
	}))
	defer server.Close()
	client, err := NewHTTPClient(server.URL, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	smd := SMDClient{HTTP: client}
	if err := smd.PutComponents(context.Background(), Components{Components: []Component{{ID: "x0c0s0b0n0", Type: "Node", NID: 1000}}}); err != nil {
		t.Fatal(err)
	}
	if err := smd.CreateInterface(context.Background(), EthernetInterface{ID: "020000000000", ComponentID: "x0c0s0b0n0", MACAddress: "02:00:00:00:00:00"}); err != nil {
		t.Fatal(err)
	}
}

func TestSMDHelpersConflictIsFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(409) }))
	defer server.Close()
	client, err := NewHTTPClient(server.URL, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	err = (SMDClient{HTTP: client}).CreateInterface(context.Background(), EthernetInterface{ID: "020000000000"})
	if err == nil {
		t.Fatal("conflict silently accepted without confirming existing interface")
	}
}
