//go:build integration

// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package integration

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Deliberately no usable signature: local CLI parsing is not authentication.
func uc8UnsignedToken(payload string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"PS256","kid":"fixture"}`)) + "." + base64.RawURLEncoding.EncodeToString([]byte(payload)) + ".c2lnbmF0dXJl"
}

func TestUC8_TokenLocalValidation(t *testing.T) {
	// Both manifest CLI pins use jwx v3. The pinned migration harness
	// explicitly selects v4 rather than inferring it from an unset variable.
	major := envOr("SBX_CLI_JWX_MAJOR", "3")
	require.Contains(t, []string{"3", "4"}, major, "unsupported CLI jwx major")
	now := time.Now().Unix()
	for _, test := range []struct {
		name, payload, diagnostic string
		rejected                  bool
	}{
		{"expired", fmt.Sprintf(`{"exp":%d}`, now-600), "token is expired", true},
		{"future_nbf", fmt.Sprintf(`{"nbf":%d,"exp":%d}`, now+600, now+3600), "token is not yet valid", true},
		{"future_iat", fmt.Sprintf(`{"iat":%d,"exp":%d}`, now+600, now+3600), "failed to parse token:", true},
		{"duplicate_exp", fmt.Sprintf(`{"exp":%d,"exp":%d}`, now+3600, now+7200), "failed to parse token:", true},
		{"duplicate_custom", `{"sub":"one","sub":"two"}`, "failed to parse token:", true},
		{"optional_times_absent", `{"sub":"uc8"}`, "", false},
		{"invalid_signature", fmt.Sprintf(`{"sub":"uc8","exp":%d}`, now+3600), "", false},
		{"expiry_warning", fmt.Sprintf(`{"exp":%d}`, now+600), "until token expires", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			var calls atomic.Int32
			var forwarded atomic.Bool
			raw := uc8UnsignedToken(test.payload)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				forwarded.Store(r.Header.Get("Authorization") == "Bearer "+raw)
				w.Header().Set("Content-Type", "application/json")
				_, _ = fmt.Fprint(w, `[]`)
			}))
			defer server.Close()
			r := uc8ContractRunner(t)
			args, err := r.envelopeArgs([]string{"boot", "--uri", uc8FixtureURL(server.URL), "--api-version", "v1", "config", "list", "-F", "json"})
			require.NoError(t, err)
			// HandleToken validates only when the selected cluster enables auth.
			config := uc8PayloadFile(t, "token-validation.yaml", map[string]any{
				"default-cluster": "test", "timeout": "10s",
				"clusters": []any{map[string]any{"name": "test", "cluster": map[string]any{
					"enable-auth": true, "boot-service": map[string]any{"uri": uc8FixtureURL(server.URL), "api-version": "v1"},
				}}},
			}, "yaml")
			result := r.execute(CLIInput{ConfigPath: config, GlobalArgs: []string{"--token", raw}}, args...)
			// Duplicate-claim rejection is an intentional v4 behavior change.
			rejected := test.rejected && (major != "3" || !strings.HasPrefix(test.name, "duplicate_"))
			if rejected {
				requireUC8CLIFailure(t, r, result)
				require.Equal(t, 4, result.ExitCode)
				require.Contains(t, result.Stderr, test.diagnostic)
				require.Zero(t, calls.Load(), "CLI must reject token before HTTP")
			} else {
				require.NoError(t, result.Err, result.Stderr)
				require.Greater(t, calls.Load(), int32(0))
				require.True(t, forwarded.Load(), "original bearer must be forwarded unchanged")
				if test.diagnostic != "" && !strings.HasPrefix(test.name, "duplicate_") {
					require.Contains(t, result.Stderr, test.diagnostic)
				}
			}
		})
	}
}
