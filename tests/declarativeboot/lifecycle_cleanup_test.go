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

func TestLifecycleHelpersAssignmentCleanup(t *testing.T) {
	deleted := false
	t.Cleanup(func() {
		if !deleted {
			t.Error("assignment was not cleaned up")
		}
	})
	resource := BootAssignment{Metadata: Metadata{Name: "fixture-assignment", UID: "opaque-uid"}, Spec: AssignmentSpec{ConfigRef: ConfigReference{Name: "fixture-series"}}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/bootassignments/by-name/fixture-assignment" {
			t.Error("unexpected assignment path")
			w.WriteHeader(http.StatusNotFound)
			return
		}
		switch r.Method {
		case http.MethodPut:
			if r.Header.Get("If-None-Match") != "*" {
				t.Error("create lacked conditional header")
			}
			w.Header().Set("ETag", `"initial"`)
			w.WriteHeader(http.StatusCreated)
			if err := json.NewEncoder(w).Encode(resource); err != nil {
				t.Error(err)
			}
		case http.MethodGet:
			w.Header().Set("ETag", `"current"`)
			if err := json.NewEncoder(w).Encode(resource); err != nil {
				t.Error(err)
			}
		case http.MethodDelete:
			if r.Header.Get("If-Match") != `"current"` {
				t.Error("cleanup did not use current ETag")
			}
			deleted = true
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Error("unexpected method")
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	t.Cleanup(server.Close)
	client, err := NewHTTPClient(server.URL, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	c := &lifecycleCase{t: t, ctx: context.Background(), boot: BootClient{HTTP: client}, series: "fixture"}
	c.createAssignment("assignment", resource.Spec)
}
