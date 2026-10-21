//go:build integration

// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package integration

import (
	"context"
	"crypto/rand"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"
)

// These short-lived tokens are syntactically valid for CLI parsing and are
// accepted only by our HTTP fixtures, not by an enforcing service.
func uc8FixtureToken(t *testing.T, subject string) string {
	t.Helper()
	key := make([]byte, 32)
	_, err := rand.Read(key)
	require.NoError(t, err)
	value, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"sub": subject, "iss": "uc8-contract", "exp": time.Now().Add(time.Hour).Unix()}).SignedString(key)
	require.NoError(t, err)
	return value
}

func TestUC8_ConfigurationContracts(t *testing.T) {
	var mu sync.Mutex
	var headers []string
	fixture := func(name string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			mu.Lock()
			headers = append(headers, req.Header.Get("Authorization"))
			mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			if _, err := fmt.Fprintf(w, `{"apiVersion":"v1","kind":"BootConfiguration","metadata":{"uid":"uc8-read","name":"uc8-read"},"spec":{"kernel":"http://fake-address/vmlinuz","params":%q},"status":{}}`, name); err != nil {
				t.Errorf("write fixture: %v", err)
			}
		}))
	}
	first, second := fixture("first"), fixture("second")
	defer first.Close()
	defer second.Close()
	cluster := func(name, uri string) map[string]any {
		return map[string]any{"name": name, "cluster": map[string]any{"uri": uc8FixtureURL(uri), "boot-service": map[string]any{"uri": "/", "api-version": "v1"}}}
	}
	config := uc8PayloadFile(t, "clusters.yaml", map[string]any{"default-cluster": "test", "timeout": "30s", "log": map[string]string{"format": "rfc3339", "level": "info"}, "clusters": []any{cluster("test", first.URL), cluster("other", second.URL)}}, "yaml")
	firstToken, secondToken, explicitToken := uc8FixtureToken(t, "first"), uc8FixtureToken(t, "second"), uc8FixtureToken(t, "explicit")
	for _, test := range []struct {
		name, source, token string
		globals, flags      []string
		ignore              bool
	}{
		{"default_cluster", "first", firstToken, nil, nil, false},
		{"selected_cluster", "second", secondToken, []string{"--cluster", "other"}, nil, false},
		{"cluster_uri", "second", firstToken, []string{"--cluster-uri", uc8FixtureURL(second.URL)}, nil, false},
		{"service_uri", "second", firstToken, nil, []string{"--uri", uc8FixtureURL(second.URL)}, false},
		{"explicit_token", "first", explicitToken, []string{"--token", explicitToken}, nil, false},
		{"no_token", "first", "", []string{"--no-token"}, nil, false},
		{"ignore_config", "second", "", []string{"--no-token"}, []string{"--uri", uc8FixtureURL(second.URL), "--api-version", "v1"}, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := uc8ContractRunner(t)
			args := append([]string{"boot"}, test.flags...)
			args = append(args, "config", "get", "uc8-read", "-F", "json")
			args, err := r.envelopeArgs(args)
			require.NoError(t, err)
			mu.Lock()
			start := len(headers)
			mu.Unlock()
			result := r.execute(CLIInput{ConfigPath: config, IgnoreConfig: test.ignore, GlobalArgs: test.globals, Env: map[string]string{"TEST_ACCESS_TOKEN": firstToken, "OTHER_ACCESS_TOKEN": secondToken}}, args...)
			require.NoError(t, result.Err, result.Stderr)
			resource := uc8Resource(t, result.Stdout, "json")
			require.Equal(t, test.source, resource["spec"].(map[string]any)["params"])
			mu.Lock()
			defer mu.Unlock()
			require.Greater(t, len(headers), start)
			want := ""
			if test.token != "" {
				want = "Bearer " + test.token
			}
			for _, actual := range headers[start:] {
				require.Equal(t, want, actual)
			}
		})
	}
	for _, globals := range [][]string{{"--cluster", "test", "--cluster-uri", uc8FixtureURL(second.URL)}, {"--token", "test-token", "--no-token"}} {
		r := uc8ContractRunner(t)
		mu.Lock()
		before := len(headers)
		mu.Unlock()
		result := r.execute(CLIInput{ConfigPath: config, GlobalArgs: globals}, "boot", "config", "get", "uc8-read")
		requireUC8CLIFailure(t, r, result)
		mu.Lock()
		require.Equal(t, before, len(headers))
		mu.Unlock()
	}
}

func TestUC8_CLITimeout(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) { calls.Add(1); <-req.Context().Done() }))
	defer server.Close()
	r := uc8ContractRunner(t)
	result := r.execute(CLIInput{IgnoreConfig: true}, "boot", "--uri", uc8FixtureURL(server.URL), "--api-version", "v1", "--no-token", "--timeout", "200ms", "config", "get", "uc8-read")
	requireUC8CLIFailure(t, r, result)
	require.Regexp(t, `(?i)(timeout|deadline)`, result.Stderr)
	require.Greater(t, calls.Load(), int32(0))
}

func TestUC8_InFlightCancellation(t *testing.T) {
	entered := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) { entered <- struct{}{}; <-req.Context().Done() }))
	defer server.Close()
	r := uc8ContractRunner(t)
	ctx, cancel := context.WithCancel(r.ctx)
	defer cancel()
	r.ctx = ctx
	finished := make(chan CLIResult, 1)
	go func() {
		finished <- r.execute(CLIInput{IgnoreConfig: true}, "boot", "--uri", uc8FixtureURL(server.URL), "--api-version", "v1", "--no-token", "config", "get", "uc8-read")
	}()
	select {
	case <-entered:
		cancel()
	case <-time.After(10 * time.Second):
		cancel()
		t.Fatal("CLI never reached delayed request fixture")
	}
	select {
	case result := <-finished:
		require.ErrorIs(t, result.Err, context.Canceled)
		require.Equal(t, -1, result.ExitCode)
		require.Error(t, exec.Command("docker", "inspect", result.ContainerName).Run(), "canceled owned container must be removed")
	case <-time.After(35 * time.Second):
		t.Fatal("cancellation did not finish within cleanup budget")
	}
}
