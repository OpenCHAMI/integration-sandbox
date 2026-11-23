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

func TestHTTPQuiescenceCancellationLeavesMarker(t *testing.T) {
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(started); <-r.Context().Done() }))
	defer server.Close()
	path := quiescenceTestLock(t)
	t.Setenv("SBX_DECLARATIVE_BOOT_URL", server.URL)
	t.Setenv("SBX_DECLARATIVE_QUIESCENCE_LOCK", path)
	client, err := NewHTTPClient(server.URL, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { _, err := client.Do(ctx, Request{Method: http.MethodPost, Path: "/"}); result <- err }()
	<-started
	cancel()
	if err := <-result; err == nil {
		t.Fatal("canceled request succeeded")
	}
	if capture, err := quiescence.AcquireCapture(context.Background(), path); !errors.Is(err, quiescence.ErrInFlight) {
		if capture != nil {
			capture.Close()
		}
		t.Fatalf("expected sticky marker: %v", err)
	}
}

func TestHTTPQuiescenceExclusiveBlocksDispatch(t *testing.T) {
	dispatched := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { dispatched <- struct{}{} }))
	defer server.Close()
	path := quiescenceTestLock(t)
	t.Setenv("SBX_DECLARATIVE_BOOT_URL", server.URL)
	t.Setenv("SBX_DECLARATIVE_QUIESCENCE_LOCK", path)
	capture, err := quiescence.AcquireCapture(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer capture.Close()
	client, err := NewHTTPClient(server.URL, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	_, err = client.Do(ctx, Request{Method: http.MethodGet, Path: "/"})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("writer should wait: %v", err)
	}
	select {
	case <-dispatched:
		t.Fatal("request dispatched under capture")
	default:
	}
}

func TestHTTPQuiescenceEarlyAckRequiresSourceAudit(t *testing.T) {
	allowMutation := make(chan struct{})
	mutationDone := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		go func() { <-allowMutation; close(mutationDone) }()
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	path := quiescenceTestLock(t)
	t.Setenv("SBX_DECLARATIVE_BOOT_URL", server.URL)
	t.Setenv("SBX_DECLARATIVE_QUIESCENCE_LOCK", path)
	client, err := NewHTTPClient(server.URL, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Do(context.Background(), Request{Method: http.MethodPost, Path: "/"}); err != nil {
		t.Fatal(err)
	}
	capture, err := quiescence.AcquireCapture(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-mutationDone:
		t.Fatal("mutation unexpectedly completed")
	default:
	}
	close(allowMutation)
	<-mutationDone
	if err := capture.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestHTTPQuiescenceInvalidHeaderDoesNotLeaveMarker(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("invalid request dispatched") }))
	defer server.Close()
	path := quiescenceTestLock(t)
	t.Setenv("SBX_DECLARATIVE_BOOT_URL", server.URL)
	t.Setenv("SBX_DECLARATIVE_QUIESCENCE_LOCK", path)
	client, err := NewHTTPClient(server.URL, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Do(context.Background(), Request{Method: http.MethodGet, Path: "/", Headers: http.Header{"Bad Header": {"x"}}}); err == nil {
		t.Fatal("invalid header accepted")
	}
	capture, err := quiescence.AcquireCapture(context.Background(), path)
	if err != nil {
		t.Fatalf("local validation left marker: %v", err)
	}
	if err := capture.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestHTTPQuiescenceNonTargetNeedsNoLock(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }))
	defer server.Close()
	t.Setenv("SBX_DECLARATIVE_BOOT_URL", "http://127.0.0.1:1")
	t.Setenv("SBX_DECLARATIVE_QUIESCENCE_LOCK", "")
	client, err := NewHTTPClient(server.URL, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Do(context.Background(), Request{Method: http.MethodGet, Path: "/"}); err != nil {
		t.Fatalf("non-target blocked: %v", err)
	}
}

func TestHTTPQuiescenceAuthOffCompleteErrorResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("unavailable"))
	}))
	defer server.Close()
	path := quiescenceTestLock(t)
	t.Setenv("SBX_DECLARATIVE_BOOT_URL", "")
	t.Setenv("SBX_DECLARATIVE_AUTH_OFF_URL", server.URL)
	t.Setenv("SBX_DECLARATIVE_QUIESCENCE_LOCK", path)
	client, err := NewHTTPClient(server.URL, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.Do(context.Background(), Request{Method: http.MethodGet, Path: "/"})
	if err != nil || response.Status != http.StatusServiceUnavailable {
		t.Fatalf("response: %d %v", response.Status, err)
	}
	capture, err := quiescence.AcquireCapture(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if err := capture.Close(); err != nil {
		t.Fatal(err)
	}
}
