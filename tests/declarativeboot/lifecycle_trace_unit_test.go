//go:build integration && declarative_boot

// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package declarativeboot

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestLifecycleHelpersWriterTrace(t *testing.T) {
	writers := []struct {
		name string
		run  func(context.Context, BootClient) (Response, error)
	}{
		{"pointer", func(ctx context.Context, boot BootClient) (Response, error) {
			return boot.PromoteDefault(ctx, Promotion{Series: "fixture", RevisionUID: "revision", IfMatch: `"etag"`})
		}},
		{"assignment", func(ctx context.Context, boot BootClient) (Response, error) {
			return boot.ApplyAssignment(ctx, ConditionalAssignment{Resource: BootAssignment{Metadata: Metadata{Name: "fixture"}, Spec: AssignmentSpec{ConfigRef: ConfigReference{Name: "series"}}}, IfMatch: `"etag"`})
		}},
	}
	for _, writer := range writers {
		for _, timing := range []string{"prewrite-only", "delayed-prewrite", "in-flight", "after-first-byte"} {
			name := writer.name + "/" + timing
			t.Run(name, func(t *testing.T) {
				received, release, finishBody := make(chan struct{}, 1), make(chan struct{}, 1), make(chan struct{}, 1)
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
					if req.Method == http.MethodGet {
						w.WriteHeader(http.StatusOK)
						return
					}
					received <- struct{}{}
					select {
					case <-release:
						w.WriteHeader(http.StatusOK)
						if timing == "after-first-byte" {
							w.(http.Flusher).Flush()
							select {
							case <-finishBody:
							case <-req.Context().Done():
							}
						}
					case <-req.Context().Done():
					}
				}))
				defer server.Close()
				client, err := NewHTTPClient(server.URL, 3*time.Second)
				if err != nil {
					t.Fatal(err)
				}
				boot := BootClient{HTTP: client}
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				defer cancel()
				readers := &lifecycleReaders{ctx: ctx, errs: make(chan error, 1)}
				readers.active.Store(2)
				readers.completed.Store(7)
				readers.beforeWrite(t)
				response, err := boot.Raw(ctx, Request{Method: http.MethodGet, Path: "/reader"})
				if err != nil || response.Status != http.StatusOK {
					t.Fatal("prewrite read failed")
				}
				prewriteAt := time.Now()
				if timing != "delayed-prewrite" {
					readers.recordCompletion(prewriteAt)
				}
				writerCtx, window := readers.writerContext(ctx)
				done := make(chan error, 1)
				var wg sync.WaitGroup
				wg.Add(1)
				go func() { defer wg.Done(); _, err := writer.run(writerCtx, boot); done <- err }()
				defer func() { cancel(); wg.Wait() }()
				select {
				case <-received:
				case <-ctx.Done():
					t.Fatal("writer did not reach server")
				}
				select {
				case <-window.started:
				case <-ctx.Done():
					t.Fatal("writer request was not sent")
				}
				if timing == "delayed-prewrite" {
					readers.recordCompletion(prewriteAt)
				}
				if timing == "in-flight" {
					response, err = boot.Raw(ctx, Request{Method: http.MethodGet, Path: "/reader"})
					if err != nil || response.Status != http.StatusOK {
						t.Fatal("in-flight read failed")
					}
					readers.recordCompletion(time.Now())
				}
				release <- struct{}{}
				if timing == "after-first-byte" {
					select {
					case <-window.firstByte:
					case <-ctx.Done():
						t.Fatal("writer did not receive first response byte")
					}
					response, err = boot.Raw(ctx, Request{Method: http.MethodGet, Path: "/reader"})
					if err != nil || response.Status != http.StatusOK {
						t.Fatal("post-header read failed")
					}
					readers.recordCompletion(time.Now())
					finishBody <- struct{}{}
				}
				select {
				case err = <-done:
				case <-ctx.Done():
					t.Fatal("writer did not complete")
				}
				if err != nil {
					t.Fatal(err)
				}
				got := window.finish(err)
				want := timing == "in-flight"
				if got != want {
					t.Fatalf("overlap = %v, want %v", got, want)
				}
				response, err = boot.Raw(ctx, Request{Method: http.MethodGet, Path: "/reader"})
				if err != nil || response.Status != http.StatusOK {
					t.Fatal("post-response read failed")
				}
				readers.recordCompletion(time.Now())
				if window.finish(nil) != want {
					t.Fatal("post-response read changed overlap")
				}
			})
		}
	}
}
