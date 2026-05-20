//go:build integration
// +build integration

// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
//
// SPDX-License-Identifier: MIT

package integration

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestService_HealthEndpoints — every service must answer GET /health (or equivalent) with 2xx.
// If a service in this list lacks /health, that is a bug to file in the appropriate bugs.md.
func TestService_HealthEndpoints(t *testing.T) {
	cases := []struct {
		name   string
		url    string
		accept []int
	}{
		{"tokensmith-jwks", Endpoints["tokensmith"] + "/.well-known/jwks.json", []int{200}},
		{"boot-health", Endpoints["boot"] + "/health", []int{200, 204}},
		{"metadata-health", Endpoints["metadata"] + "/health", []int{200, 204}},
		{"fru-health", Endpoints["fru"] + "/health", []int{200, 204}},
		{"power-health", Endpoints["power"] + "/health", []int{200, 204}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, tc.url, nil)
			resp, err := httpClient.Do(req)
			require.NoError(t, err)
			defer resp.Body.Close()
			ok := false
			for _, c := range tc.accept {
				if resp.StatusCode == c {
					ok = true
					break
				}
			}
			require.Truef(t, ok, "%s -> HTTP %d (accepted %v)", tc.url, resp.StatusCode, tc.accept)
		})
	}
}
