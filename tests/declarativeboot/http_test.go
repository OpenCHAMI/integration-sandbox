//go:build integration && declarative_boot

// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package declarativeboot

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHTTPHelpersConditionalAndDecode(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/bootassignments/by-name/group-default" || r.Header.Get("If-Match") != `"old"` {
			t.Errorf("unexpected request: %s %s precondition %q", r.Method, r.URL.Path, r.Header.Get("If-Match"))
		}
		w.Header().Set("ETag", `"new"`)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"metadata":{"name":"group-default","uid":"opaque-uid"},"spec":{"configRef":{"name":"acceptance-os"}}}`))
	}))
	defer server.Close()
	client, err := NewHTTPClient(server.URL, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.Do(context.Background(), Request{Method: http.MethodPut, Path: "/bootassignments/by-name/group-default", Headers: http.Header{"If-Match": []string{`"old"`}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := response.RequireStatus(http.StatusOK); err != nil {
		t.Fatal(err)
	}
	value, err := DecodeJSON[BootAssignment](response)
	if err != nil {
		t.Fatal(err)
	}
	if value.Metadata.UID != "opaque-uid" || value.Spec.ConfigRef.Name != "acceptance-os" || response.Header.Get("ETag") != `"new"` {
		t.Fatalf("decoded response incorrect: %+v", value)
	}
	t.Logf("decoded fixture response: uid=%s config=%s etag=%s", value.Metadata.UID, value.Spec.ConfigRef.Name, response.Header.Get("ETag"))
}

func TestHTTPHelpersMalformedAndUnexpectedStatus(t *testing.T) {
	response := Response{Status: 500, Body: []byte("secret-token{")}
	if err := response.RequireStatus(200); err == nil || strings.Contains(err.Error(), "secret-token") {
		t.Fatalf("unexpected or unsafe status error: %v", err)
	}
	if _, err := DecodeJSON[BootAssignment](response); err == nil || strings.Contains(err.Error(), "secret-token") {
		t.Fatalf("unexpected or unsafe decode error: %v", err)
	}
	if !json.Valid([]byte(`{"ok":true}`)) {
		t.Fatal("invalid test fixture")
	}
}

func TestHTTPHelpersCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer server.Close()
	client, err := NewHTTPClient(server.URL, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = client.Do(ctx, Request{Method: http.MethodGet, Path: "/?token=secret-token"})
	if err == nil || strings.Contains(err.Error(), "secret-token") {
		t.Fatalf("unexpected or unsafe error: %v", err)
	}
}

func TestHTTPHelpersTimeoutRedactsURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer server.Close()
	client, err := NewHTTPClient(server.URL, 30*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Do(context.Background(), Request{Method: http.MethodGet, Path: "/?token=private-value"})
	if err == nil || strings.Contains(err.Error(), "private-value") {
		t.Fatalf("expected redacted timeout: %v", err)
	}
}
