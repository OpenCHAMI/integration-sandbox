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
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestUC8_VersionOutput(t *testing.T) {
	for _, test := range []struct {
		name, output string
		valid        bool
	}{
		{"release", "Version: v0.8.1\nTag: v0.8.1\nGo: go1.24.6\nCompiler: gc\n", true},
		{"development", "Version: unknown\nGo: go1.24.6\nCompiler: gc\n", true},
		{"empty", "", false},
		{"unstructured", "v0.8.1", false},
		{"missing_compiler", "Version: v0.8.1\nGo: go1.24.6\n", false},
		{"empty_version", "Version: \nGo: go1.24.6\nCompiler: gc\n", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := validateUC8Version(test.output)
			if test.valid {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}

func TestUC8_PCSReadiness(t *testing.T) {
	ready := `{"status":[{"xname":"x0c0s0b0","error":"","powerState":"on","managementState":"available"}]}`
	missing := `{"status":[{"xname":"x0c0s0b0","error":"Component not found in component map.","powerState":"","managementState":""}]}`
	for _, test := range []struct {
		name, body string
		code       int
		converges  bool
	}{
		{"pending_then_ready", missing, 200, true},
		{"component_error", missing, 200, false},
		{"empty_status", `{"status":[]}`, 200, false},
		{"wrong_component", `{"status":[{"xname":"x0c0s1b0","powerState":"on","managementState":"available"}]}`, 200, false},
		{"stale_state", `{"status":[{"xname":"x0c0s0b0","powerState":"on","managementState":"unavailable"}]}`, 200, false},
		{"malformed_json", "not JSON", 200, false},
		{"http_error", ready, 500, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/v1/power-status" || r.URL.Query().Get("xname") != "x0c0s0b0" {
					w.WriteHeader(http.StatusNotFound)
					return
				}
				body := test.body
				if calls.Add(1) > 1 && test.converges {
					body = ready
				}
				w.WriteHeader(test.code)
				if _, err := fmt.Fprint(w, body); err != nil {
					t.Errorf("write status: %v", err)
				}
			}))
			defer server.Close()
			oldPower := Endpoints["power"]
			Endpoints["power"] = server.URL
			t.Cleanup(func() { Endpoints["power"] = oldPower })
			timeout := 50 * time.Millisecond
			if test.converges {
				timeout = 5 * time.Second
			}
			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()
			err := waitUC8PCSReady(ctx, "x0c0s0b0")
			if test.converges {
				require.NoError(t, err)
				require.Equal(t, int32(2), calls.Load())
			} else {
				require.ErrorIs(t, err, context.DeadlineExceeded)
				require.ErrorContains(t, err, "last response:")
			}
		})
	}
}
