//go:build integration

// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Docker Desktop's host gateway reaches in-process fixtures without requiring
// its optional host-networking feature. Linux runners use host networking.
func uc8FixtureURL(uri string) string {
	if runtime.GOOS == "darwin" {
		return strings.Replace(uri, "127.0.0.1", "host.docker.internal", 1)
	}
	return uri
}

// Contract cases execute the real CLI image but do not require live services.
func uc8ContractRunner(t *testing.T) *ochami_runner {
	t.Helper()
	env, err := LoadImages()
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	t.Cleanup(cancel)
	r := &ochami_runner{ctx: ctx, t: t, env: env, config_path: filepath.Join(t.TempDir(), "config.yaml")}
	require.NoError(t, r.sync_config())
	return r
}

func requireUC8CLIFailure(t *testing.T, r *ochami_runner, result CLIResult) {
	t.Helper()
	var exitErr *exec.ExitError
	require.ErrorAs(t, result.Err, &exitErr, result.Stderr)
	require.Greater(t, result.ExitCode, 0)
	require.NotEmpty(t, result.Stderr)
	require.NoError(t, r.ctx.Err(), "the CLI must fail before harness cancellation")
}

func TestUC8_FailureContracts(t *testing.T) {
	for _, test := range []struct {
		name, stdin string
		args        []string
	}{
		{"unknown_flag", "", []string{"version", "--uc8-unknown-flag"}},
		{"invalid_json", "{", []string{"boot", "--uri", "http://127.0.0.1:1", "config", "add", "-d", "@-", "-f", "json"}},
		{"invalid_yaml", "spec: [", []string{"boot", "--uri", "http://127.0.0.1:1", "config", "add", "-d", "@-", "-f", "yaml"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := uc8ContractRunner(t)
			result := r.execute(CLIInput{IgnoreConfig: true, Stdin: test.stdin}, test.args...)
			requireUC8CLIFailure(t, r, result)
		})
	}
	for _, test := range []struct {
		name string
		code int
		body string
	}{
		{"not_found", 404, `{"error":"uc8 resource not found"}`},
		{"server_failure", 500, `{"error":"uc8 server failure"}`},
		{"malformed_response", 200, "{"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				if req.Method == http.MethodGet && req.URL.Path == "/bootconfigurations/uc8-missing" {
					calls.Add(1)
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(test.code)
				if _, err := fmt.Fprint(w, test.body); err != nil {
					t.Errorf("write fixture: %v", err)
				}
			}))
			defer server.Close()
			r := uc8ContractRunner(t)
			result := r.execute(CLIInput{IgnoreConfig: true}, "boot", "--uri", uc8FixtureURL(server.URL), "--api-version", "v1", "--no-token", "config", "get", "uc8-missing")
			requireUC8CLIFailure(t, r, result)
			require.Greater(t, calls.Load(), int32(0), "failure must come from the fixture request, not Docker/network setup: %s", result.Stderr)
		})
	}
	t.Run("connection_failure", func(t *testing.T) {
		server := httptest.NewServer(http.NotFoundHandler())
		uri := uc8FixtureURL(server.URL)
		server.Close()
		r := uc8ContractRunner(t)
		result := r.execute(CLIInput{IgnoreConfig: true}, "boot", "--uri", uri, "--api-version", "v1", "--no-token", "config", "get", "uc8-missing")
		requireUC8CLIFailure(t, r, result)
		require.Regexp(t, `(?i)(connect|refused)`, result.Stderr)
	})
}

func TestUC8_RunnerResult(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		r := uc8ContractRunner(t)
		result := r.execute(CLIInput{IgnoreConfig: true}, "version")
		require.NoError(t, result.Err, result.Stderr)
		require.Zero(t, result.ExitCode)
		require.NoError(t, validateUC8Version(result.Stdout))
	})
	t.Run("cli_failure", func(t *testing.T) {
		r := uc8ContractRunner(t)
		result := r.execute(CLIInput{IgnoreConfig: true}, "uc8-unknown-command")
		var exitErr *exec.ExitError
		require.ErrorAs(t, result.Err, &exitErr)
		require.Greater(t, result.ExitCode, 0)
		require.Contains(t, result.Stderr, "unknown command")
	})
	t.Run("canceled_context", func(t *testing.T) {
		r := uc8ContractRunner(t)
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		r.ctx = ctx
		result := r.execute(CLIInput{IgnoreConfig: true}, "version")
		require.ErrorIs(t, result.Err, context.Canceled)
		require.Equal(t, -1, result.ExitCode)
		require.Error(t, exec.Command("docker", "inspect", result.ContainerName).Run(), "owned invocation must be absent")
	})
	t.Run("invalid_mount", func(t *testing.T) {
		r := uc8ContractRunner(t)
		result := r.execute(CLIInput{Files: map[string]string{r.config_path: "relative-path"}}, "version")
		require.ErrorContains(t, result.Err, "invalid fixture mount")
		require.Equal(t, -1, result.ExitCode)
	})
}

func TestUC8_InputFiles(t *testing.T) {
	for _, format := range []string{"json", "yaml"} {
		t.Run(format, func(t *testing.T) {
			var mu sync.Mutex
			var observed map[string]any
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				if req.Method != http.MethodPost || req.URL.Path != "/bootconfigurations" {
					w.WriteHeader(404)
					return
				}
				var data map[string]any
				if err := json.NewDecoder(req.Body).Decode(&data); err != nil {
					t.Errorf("decode request: %v", err)
					w.WriteHeader(400)
					return
				}
				mu.Lock()
				observed = data
				mu.Unlock()
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(201)
				if _, err := fmt.Fprint(w, `{"apiVersion":"v1","kind":"BootConfiguration","metadata":{"name":"uc8-input","uid":"uc8-id"},"spec":{"kernel":"http://fake-address/vmlinuz","params":"uc8-file"},"status":{}}`); err != nil {
					t.Errorf("write resource: %v", err)
				}
			}))
			defer server.Close()
			path := uc8PayloadFile(t, "input."+format, map[string]any{"metadata": map[string]string{"name": "uc8-input"}, "spec": map[string]string{"kernel": "http://fake-address/vmlinuz", "params": "uc8-file"}}, format)
			r := uc8ContractRunner(t)
			args, err := r.envelopeArgs([]string{"boot", "--uri", uc8FixtureURL(server.URL), "--api-version", "v1", "--no-token", "config", "add", "-d", "@/fixtures/input", "-f", format})
			require.NoError(t, err)
			result := r.execute(CLIInput{IgnoreConfig: true, Files: map[string]string{path: "/fixtures/input"}}, args...)
			require.NoError(t, result.Err, result.Stderr)
			require.Zero(t, result.ExitCode)
			mu.Lock()
			defer mu.Unlock()
			require.NotNil(t, observed, "server must receive the actual CLI request")
			require.Equal(t, "uc8-input", observed["metadata"].(map[string]any)["name"], "actual request: %#v", observed)
			require.Equal(t, "uc8-file", observed["spec"].(map[string]any)["params"])
		})
	}
}
