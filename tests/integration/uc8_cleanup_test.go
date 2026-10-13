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
	"sync"
	"testing"

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
