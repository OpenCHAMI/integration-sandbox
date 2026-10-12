//go:build integration

// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
//
// SPDX-License-Identifier: MIT

package integration

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUC8_CleanupPreservesOtherResources(t *testing.T) {
	for _, failure := range []bool{false, true} {
		t.Run(fmt.Sprintf("delete_failure=%t", failure), func(t *testing.T) {
			var deleted []string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer test-token" {
					w.WriteHeader(http.StatusUnauthorized)
					return
				}
				if r.Method == http.MethodGet && r.URL.Path == "/bootconfigurations" {
					w.Header().Set("Content-Type", "application/json")
					_, err := fmt.Fprint(w, `[{"metadata":{"name":"uc8-owned","uid":"owned-1"}},{"metadata":{"name":"other","uid":"other-1"}},{"metadata":{"name":"uc8-owned","uid":"owned-2"}}]`)
					if err != nil {
						t.Errorf("write list: %v", err)
					}
					return
				}
				if r.Method != http.MethodDelete {
					w.WriteHeader(http.StatusMethodNotAllowed)
					return
				}
				deleted = append(deleted, r.URL.Path)
				if failure && r.URL.Path == "/bootconfigurations/owned-1" {
					w.WriteHeader(http.StatusInternalServerError)
				} else {
					// Missing records are harmless, including after partial creation.
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			defer server.Close()
			oldBoot, oldSMD := Endpoints["boot"], Endpoints["smd"]
			Endpoints["boot"], Endpoints["smd"] = server.URL, server.URL
			t.Cleanup(func() { Endpoints["boot"], Endpoints["smd"] = oldBoot, oldSMD })

			err := cleanupUC8Resources(context.Background(), "uc8-owned", "test-token")
			if failure {
				require.ErrorContains(t, err, "HTTP 500")
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, []string{"/bootconfigurations/owned-1", "/bootconfigurations/owned-2", "/hsm/v2/groups/uc8-owned"}, deleted)
		})
	}
}
