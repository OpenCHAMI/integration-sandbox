//go:build integration && declarative_boot

// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package declarativeboot

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/openchami/integration-sandbox/tests/internal/quiescence"
)

func TestLifecycleReadersStopDrainsRequestBeforeCapture(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		close(entered)
		select {
		case <-release:
			w.WriteHeader(http.StatusOK)
		case <-req.Context().Done():
		}
	}))
	t.Cleanup(server.Close)
	path := quiescenceTestLock(t)
	t.Setenv("SBX_DECLARATIVE_BOOT_URL", server.URL)
	t.Setenv("SBX_DECLARATIVE_QUIESCENCE_LOCK", path)
	client, err := NewHTTPClient(server.URL, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	r := &lifecycleReaders{ctx: ctx, cancel: cancel, errs: make(chan error, 1)}
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		_, err := client.Do(ctx, Request{Method: http.MethodGet, Path: "/boot"})
		if err != nil {
			r.errs <- err
		}
	}()
	<-entered
	stopped := make(chan struct{})
	go func() { r.stop(t); close(stopped) }()
	for !r.stopping.Load() && ctx.Err() == nil {
		time.Sleep(time.Millisecond)
	}
	if ctx.Err() != nil {
		t.Fatal("drain canceled request")
	}
	select {
	case <-stopped:
		t.Fatal("stop returned with request in flight")
	default:
	}
	close(release)
	<-stopped
	capture, err := quiescence.AcquireCapture(context.Background(), path)
	if err != nil {
		t.Fatalf("completed read left an ambiguous request: %v", err)
	}
	if err := capture.Close(); err != nil {
		t.Fatal(err)
	}
}
