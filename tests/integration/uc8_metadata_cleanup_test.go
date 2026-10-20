//go:build integration

// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package integration

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestUC8_MetadataCleanupOwnership(t *testing.T) {
	for _, test := range []struct {
		name, body            string
		status                int
		wantDelete, wantError bool
	}{
		{"owned_only", `[{"metadata":{"name":"owned","uid":"owned-uid"}},{"metadata":{"name":"other","uid":"foreign-uid"}}]`, 200, true, false},
		{"list_failure", `[]`, 500, false, true},
		{"malformed", `{`, 200, false, true},
		{"missing_uid", `[{"metadata":{"name":"owned"}}]`, 200, false, true},
		{"absent", `[{"metadata":{"name":"other","uid":"foreign-uid"}}]`, 200, false, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			deleted := make(chan string, 4)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodDelete {
					deleted <- r.URL.Path
					w.WriteHeader(204)
					return
				}
				w.WriteHeader(test.status)
				_, _ = fmt.Fprint(w, test.body)
			}))
			defer server.Close()
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			err := uc8DeleteOwnedModern(ctx, server.URL+"/groups", "owned", "")
			if test.wantError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			if test.wantDelete {
				select {
				case path := <-deleted:
					require.Equal(t, "/groups/owned-uid", path)
				case <-ctx.Done():
					t.Fatal("owned deletion was not observed")
				}
			}
			require.Empty(t, deleted, "cleanup must preserve unrelated resources")
		})
	}
}

func TestUC8_DiagnosticRedaction(t *testing.T) {
	raw := uc8UnsignedToken(`{"sub":"private-test-subject","exp":9999999999}`)
	require.NotContains(t, uc8Sanitize("request failed: "+raw), raw)
	require.Equal(t, "HTTP 401", uc8Sanitize("HTTP 401"))
}
