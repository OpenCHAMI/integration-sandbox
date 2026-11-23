//go:build integration && declarative_boot

// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package declarativeboot

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/openchami/integration-sandbox/tests/internal/quiescence"
)

func quiescenceTestLock(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "lock")
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestHTTPQuiescenceHoldsUntilBodyComplete(t *testing.T) {
	started := make(chan struct{})
	finish := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		close(started)
		<-finish
		_, _ = w.Write([]byte("done"))
	}))
	defer server.Close()
	path := quiescenceTestLock(t)
	t.Setenv("SBX_DECLARATIVE_BOOT_URL", server.URL)
	t.Setenv("SBX_DECLARATIVE_QUIESCENCE_LOCK", path)
	client, err := NewHTTPClient(server.URL, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() {
		_, err := client.Do(context.Background(), Request{Method: http.MethodGet, Path: "/"})
		result <- err
	}()
	<-started
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	if capture, err := quiescence.AcquireCapture(ctx, path); !errors.Is(err, context.DeadlineExceeded) {
		if capture != nil {
			capture.Close()
		}
		close(finish)
		t.Fatalf("capture did not wait: %v", err)
	}
	close(finish)
	if err := <-result; err != nil {
		t.Fatal(err)
	}
	capture, err := quiescence.AcquireCapture(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if err := capture.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestHTTPQuiescenceTruncatedBodyIsSticky(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "20")
		_, _ = w.Write([]byte("short"))
	}))
	defer server.Close()
	path := quiescenceTestLock(t)
	t.Setenv("SBX_DECLARATIVE_BOOT_URL", server.URL)
	t.Setenv("SBX_DECLARATIVE_QUIESCENCE_LOCK", path)
	client, err := NewHTTPClient(server.URL, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Do(context.Background(), Request{Method: http.MethodPost, Path: "/"}); err == nil {
		t.Fatal("truncated body accepted")
	}
	if capture, err := quiescence.AcquireCapture(context.Background(), path); !errors.Is(err, quiescence.ErrInFlight) {
		if capture != nil {
			capture.Close()
		}
		t.Fatalf("capture should reject marker: %v", err)
	}
}

func TestHTTPQuiescenceTargetRequiresLock(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("request dispatched without lock") }))
	defer server.Close()
	t.Setenv("SBX_DECLARATIVE_BOOT_URL", server.URL)
	t.Setenv("SBX_DECLARATIVE_QUIESCENCE_LOCK", "")
	client, err := NewHTTPClient(server.URL, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Do(context.Background(), Request{Method: http.MethodGet, Path: "/"}); err == nil {
		t.Fatal("missing lock accepted")
	}
}
