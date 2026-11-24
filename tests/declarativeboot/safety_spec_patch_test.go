//go:build integration && declarative_boot

// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package declarativeboot

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestSafetySpecMergePatch(t *testing.T) {
	var priority atomic.Int64
	priority.Store(100)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch || r.URL.Path != "/bootassignments/opaque" || r.Header.Get("Content-Type") != "application/merge-patch+json" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if r.Header.Get("If-Match") != `"current"` {
			w.WriteHeader(http.StatusPreconditionFailed)
			return
		}
		var specPatch struct {
			Priority *int `json:"priority"`
		}
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		if decoder.Decode(&specPatch) != nil || specPatch.Priority == nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		priority.Store(int64(*specPatch.Priority))
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	client, err := NewHTTPClient(server.URL, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	boot := BootClient{HTTP: client}
	request := Request{Method: http.MethodPatch, Path: "/bootassignments/opaque", Headers: http.Header{"Content-Type": {"application/merge-patch+json"}, "If-Match": {`"stale"`}}, Body: []byte(`{"priority":98}`)}
	response, err := boot.Raw(context.Background(), request)
	if err != nil || response.Status != http.StatusPreconditionFailed || priority.Load() != 100 {
		t.Fatal("stale spec patch did not preserve priority")
	}
	request.Headers.Set("If-Match", `"current"`)
	response, err = boot.Raw(context.Background(), request)
	if err != nil || response.Status != http.StatusOK || priority.Load() != 98 {
		t.Fatal("spec-level merge patch did not update priority")
	}
}
