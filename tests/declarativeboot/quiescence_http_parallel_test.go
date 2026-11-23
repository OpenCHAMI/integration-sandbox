//go:build integration && declarative_boot

// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package declarativeboot

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/openchami/integration-sandbox/tests/internal/quiescence"
)

func TestHTTPQuiescenceParallelBodies(t *testing.T) {
	started := make(chan struct{}, 2)
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.(http.Flusher).Flush()
		started <- struct{}{}
		<-release
		_, _ = w.Write([]byte("done"))
	}))
	defer server.Close()
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })
	path := quiescenceTestLock(t)
	t.Setenv("SBX_DECLARATIVE_BOOT_URL", server.URL)
	t.Setenv("SBX_DECLARATIVE_QUIESCENCE_LOCK", path)
	client, err := NewHTTPClient(server.URL, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() { _, err := client.Do(ctx, Request{Method: http.MethodPost, Path: "/"}); results <- err }()
	}
	for i := 0; i < 2; i++ {
		select {
		case <-started:
		case <-ctx.Done():
			t.Fatal("shared writers did not both dispatch")
		}
	}
	blocked, stop := context.WithTimeout(ctx, 40*time.Millisecond)
	defer stop()
	if capture, err := quiescence.AcquireCapture(blocked, path); !errors.Is(err, context.DeadlineExceeded) {
		if capture != nil {
			capture.Close()
		}
		t.Fatalf("capture did not wait for bodies: %v", err)
	}
	releaseOnce.Do(func() { close(release) })
	for i := 0; i < 2; i++ {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	capture, err := quiescence.AcquireCapture(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if err := capture.Close(); err != nil {
		t.Fatal(err)
	}
}
