//go:build integration && declarative_boot

// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package declarativeboot

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/openchami/integration-sandbox/tests/internal/quiescence"
)

func TestHTTPQuiescenceTimeoutLeavesMarker(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer server.Close()
	path := quiescenceTestLock(t)
	t.Setenv("SBX_DECLARATIVE_BOOT_URL", server.URL)
	t.Setenv("SBX_DECLARATIVE_QUIESCENCE_LOCK", path)
	client, err := NewHTTPClient(server.URL, 30*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Do(context.Background(), Request{Method: http.MethodPost, Path: "/"}); err == nil {
		t.Fatal("timed out request succeeded")
	}
	if capture, err := quiescence.AcquireCapture(context.Background(), path); !errors.Is(err, quiescence.ErrInFlight) {
		if capture != nil {
			capture.Close()
		}
		t.Fatalf("missing timeout marker: %v", err)
	}
}

func TestHTTPQuiescenceBodyLimitLeavesMarker(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(make([]byte, (4<<20)+1)) }))
	defer server.Close()
	path := quiescenceTestLock(t)
	t.Setenv("SBX_DECLARATIVE_BOOT_URL", server.URL)
	t.Setenv("SBX_DECLARATIVE_QUIESCENCE_LOCK", path)
	client, err := NewHTTPClient(server.URL, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Do(context.Background(), Request{Method: http.MethodPost, Path: "/"}); err == nil {
		t.Fatal("oversized response accepted")
	}
	if capture, err := quiescence.AcquireCapture(context.Background(), path); !errors.Is(err, quiescence.ErrInFlight) {
		if capture != nil {
			capture.Close()
		}
		t.Fatalf("missing body limit marker: %v", err)
	}
}
