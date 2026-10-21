//go:build integration

// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
//
// SPDX-License-Identifier: MIT

package integration

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestUC8_CleanupPreservesOtherResources(t *testing.T) {
	list := `[{"metadata":{"name":"uc8-owned","uid":"owned-1"}},{"metadata":{"name":"other","uid":"other-1"}},{"metadata":{"name":"uc8-owned","uid":"owned-2"}}]`
	allDeletes := []string{"/bootconfigurations/owned-1", "/bootconfigurations/owned-2", "/hsm/v2/groups/uc8-owned"}
	for _, test := range []struct {
		name         string
		listStatus   int
		listBody     string
		deleteStatus int
		failFirst    bool
		wantError    string
		wantDeleted  []string
	}{
		{"deleted", 200, list, 200, false, "", allDeletes},
		{"no_content", 200, list, 204, false, "", allDeletes},
		{"already_absent", 200, list, 404, false, "", allDeletes},
		{"delete_failure", 200, list, 200, true, "HTTP 500", allDeletes},
		{"list_failure", 500, "", 200, false, "list boot configurations: HTTP 500", allDeletes[2:]},
		{"malformed_list", 200, "invalid JSON", 200, false, "decode boot configurations", allDeletes[2:]},
		{"missing_uid", 200, `[{"metadata":{"name":"uc8-owned"}},{"metadata":{"name":"other","uid":"other-1"}},{"metadata":{"name":"uc8-owned","uid":"owned-2"}}]`, 200, false, "has no UID", allDeletes[1:]},
	} {
		t.Run(test.name, func(t *testing.T) {
			var mu sync.Mutex
			var deleted []string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer test-token" {
					w.WriteHeader(http.StatusUnauthorized)
					return
				}
				if r.Method == http.MethodGet && r.URL.Path == "/bootconfigurations" {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(test.listStatus)
					if _, err := fmt.Fprint(w, test.listBody); err != nil {
						t.Errorf("write list: %v", err)
					}
					return
				}
				if r.Method != http.MethodDelete {
					w.WriteHeader(http.StatusMethodNotAllowed)
					return
				}
				mu.Lock()
				deleted = append(deleted, r.URL.Path)
				mu.Unlock()
				if test.failFirst && r.URL.Path == "/bootconfigurations/owned-1" {
					w.WriteHeader(http.StatusInternalServerError)
				} else {
					w.WriteHeader(test.deleteStatus)
				}
			}))
			defer server.Close()
			oldBoot, oldSMD := Endpoints["boot"], Endpoints["smd"]
			Endpoints["boot"], Endpoints["smd"] = server.URL, server.URL
			t.Cleanup(func() { Endpoints["boot"], Endpoints["smd"] = oldBoot, oldSMD })
			err := cleanupUC8Resources(context.Background(), "uc8-owned", "test-token")
			if test.wantError != "" {
				require.ErrorContains(t, err, test.wantError)
			} else {
				require.NoError(t, err)
			}
			mu.Lock()
			defer mu.Unlock()
			require.Equal(t, test.wantDeleted, deleted)
		})
	}
}

type uc8RoundTripFunc func(*http.Request) (*http.Response, error)

func (f uc8RoundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestUC8_CleanupTransport(t *testing.T) {
	for _, kind := range []string{"connection_failure", "interrupted_list", "request_deadline", "canceled_parent"} {
		t.Run(kind, func(t *testing.T) {
			var groupDeletes atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodDelete && r.URL.Path == "/hsm/v2/groups/uc8-owned" {
					groupDeletes.Add(1)
					w.WriteHeader(204)
					return
				}
				if kind == "interrupted_list" {
					conn, _, err := w.(http.Hijacker).Hijack()
					if err != nil {
						t.Errorf("hijack: %v", err)
						return
					}
					if _, err := io.WriteString(conn, "HTTP/1.1 200 OK\r\nContent-Length: 100\r\n\r\n["); err != nil {
						t.Errorf("write interrupted reply: %v", err)
					}
					if err := conn.Close(); err != nil {
						t.Errorf("close interrupted reply: %v", err)
					}
					return
				}
				if _, err := fmt.Fprint(w, "[]"); err != nil {
					t.Errorf("write list: %v", err)
				}
			}))
			defer server.Close()
			oldBoot, oldSMD := Endpoints["boot"], Endpoints["smd"]
			oldClient := httpClient
			Endpoints["boot"], Endpoints["smd"] = server.URL, server.URL
			t.Cleanup(func() { Endpoints["boot"], Endpoints["smd"] = oldBoot, oldSMD; httpClient = oldClient })
			if kind == "connection_failure" {
				closed := httptest.NewServer(http.NotFoundHandler())
				Endpoints["boot"] = closed.URL
				closed.Close()
			}
			if kind == "request_deadline" {
				copyClient := *httpClient
				transport := copyClient.Transport
				copyClient.Transport = uc8RoundTripFunc(func(r *http.Request) (*http.Response, error) {
					if r.Method == http.MethodGet {
						return nil, context.DeadlineExceeded
					}
					return transport.RoundTrip(r)
				})
				httpClient = &copyClient
			}
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			if kind == "canceled_parent" {
				cancel()
			}
			err := cleanupUC8Resources(ctx, "uc8-owned", "test-token")
			require.Error(t, err)
			if kind == "canceled_parent" {
				require.ErrorIs(t, err, context.Canceled)
				require.Zero(t, groupDeletes.Load())
			} else {
				require.Equal(t, int32(1), groupDeletes.Load())
			}
			if kind == "request_deadline" {
				require.True(t, errors.Is(err, context.DeadlineExceeded))
				require.NoError(t, ctx.Err(), "an operation failure must not consume the overall cleanup budget")
			}
		})
	}
}

func TestUC8_CleanupOwnershipScope(t *testing.T) {
	var mu sync.Mutex
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		paths = append(paths, r.Method+" "+r.URL.Path)
		mu.Unlock()
		if r.Method == http.MethodGet && r.URL.Path == "/nodes" {
			if _, err := fmt.Fprint(w, `[{"metadata":{"name":"uc8-owned","uid":"owned-node"}},{"metadata":{"name":"fixture-node","uid":"other-node"}}]`); err != nil {
				t.Errorf("write nodes: %v", err)
			}
			return
		}
		w.WriteHeader(204)
	}))
	defer server.Close()
	oldBoot := Endpoints["boot"]
	Endpoints["boot"] = server.URL
	t.Cleanup(func() { Endpoints["boot"] = oldBoot })
	require.NoError(t, cleanupUC8Owned(context.Background(), "uc8-owned", "test-token", uc8CleanupScope{}))
	require.NoError(t, cleanupUC8Owned(context.Background(), "uc8-owned", "test-token", uc8CleanupScope{Nodes: true}))
	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, []string{"GET /nodes", "DELETE /nodes/owned-node"}, paths)
}
