// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/openchami/integration-sandbox/tests/internal/quiescence"
)

func TestCaptureWaitsForFullWriterResponseBody(t *testing.T) {
	c, path, _ := barrierFixture(t)
	writer, err := quiescence.BeginWrite(context.Background(), path, "http")
	if err != nil {
		t.Fatal(err)
	}
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("part"))
		w.(http.Flusher).Flush()
		<-release
		_, _ = w.Write([]byte("rest"))
	}))
	defer server.Close()
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })
	response, err := server.Client().Get(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	finished := make(chan error, 1)
	go func() {
		_, err := io.Copy(io.Discard, response.Body)
		if closeErr := response.Body.Close(); err == nil {
			err = closeErr
		}
		if err == nil {
			err = writer.Complete()
		} else {
			writer.Abandon()
		}
		finished <- err
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	captured := make(chan error, 1)
	go func() { captured <- c.withCapture(ctx, func() error { return nil }) }()
	select {
	case <-captured:
		releaseOnce.Do(func() { close(release) })
		t.Fatal("capture completed before response body")
	case <-time.After(100 * time.Millisecond):
	}
	releaseOnce.Do(func() { close(release) })
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-captured:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("capture did not resume")
	}
}
