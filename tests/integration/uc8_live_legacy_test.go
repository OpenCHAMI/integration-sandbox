//go:build integration

// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func uc8LegacyHTTP(ctx context.Context, method, uri, token string) (int, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, uri, nil)
	if err != nil {
		return 0, nil, err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return resp.StatusCode, data, err
}

func TestUC8_LiveLegacyAPIs(t *testing.T) {
	if os.Getenv("SBX_CLI_APIS_ENABLED") != "1" {
		t.Skip("real BSS/cloud-init require dedicated cli-apis profile")
	}
	t.Run("bss_parameters", func(t *testing.T) {
		r := uc8ContractRunner(t)
		ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
		defer cancel()
		r.ctx = ctx
		require.NoError(t, r.gen_access_token())
		const xname = "x0c0s7b0n0"
		uri := envOr("SBX_BSS_URL", "http://127.0.0.1:27778/boot/v1") + "/bootparameters?name=" + xname
		code, body, err := uc8LegacyHTTP(ctx, http.MethodGet, uri, r.access_token)
		require.NoError(t, err)
		// An empty selector result or 404 is safe; any existing record is a collision.
		require.True(t, code == 404 || (code == 200 && strings.TrimSpace(string(body)) == "[]"), "BSS fixture collision or unreadable selector (HTTP %d)", code)
		owned := false
		t.Cleanup(func() {
			if !owned {
				return
			}
			cleanupCtx, stop := context.WithTimeout(context.Background(), 10*time.Second)
			defer stop()
			code, _, err := uc8LegacyHTTP(cleanupCtx, http.MethodDelete, uri, r.access_token)
			require.NoError(t, err)
			require.True(t, code/100 == 2 || code == 404, "owned BSS cleanup HTTP %d", code)
		})
		command := func(args ...string) string {
			return uc8Command(t, r, "", append([]string{"bss", "boot", "params"}, args...)...)
		}
		marker := fmt.Sprintf("uc8=%d", time.Now().UnixNano())
		owned = true // absence checked; cleanup also covers interrupted CLI responses
		command("add", "--xname", xname, "--kernel", "http://fixture/kernel", "--initrd", "http://fixture/initrd", "--params", marker)
		for _, mutation := range []string{"update", "set"} {
			args := []string{mutation, "--xname", xname, "--params", marker + mutation}
			if mutation == "set" {
				args = append(args, "--kernel", "http://fixture/kernel", "--initrd", "http://fixture/initrd")
			}
			command(args...)
			require.Contains(t, command("get", "--xname", xname), marker+mutation)
			code, body, err = uc8LegacyHTTP(ctx, http.MethodGet, uri, r.access_token)
			require.NoError(t, err)
			require.Equal(t, 200, code)
			require.Contains(t, string(body), marker+mutation, "independent BSS read")
		}
		command("delete", "--xname", xname, "--no-confirm")
		code, body, err = uc8LegacyHTTP(ctx, http.MethodGet, uri, r.access_token)
		require.NoError(t, err)
		require.True(t, code == 404 || (code == 200 && strings.TrimSpace(string(body)) == "[]"))
		owned = false
	})
	t.Run("cloud_init_group", func(t *testing.T) {
		r := uc8ContractRunner(t)
		ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
		defer cancel()
		r.ctx = ctx
		require.NoError(t, r.gen_access_token())
		name := fmt.Sprintf("uc8-legacy-%d", time.Now().UnixNano())
		uri := envOr("SBX_CLOUD_INIT_URL", "http://127.0.0.1:27777") + "/admin/groups/" + name
		code, _, err := uc8LegacyHTTP(ctx, http.MethodGet, uri, r.access_token)
		require.NoError(t, err)
		require.Equal(t, 404, code, "refuse existing group or failed readiness")
		t.Cleanup(func() {
			cleanupCtx, stop := context.WithTimeout(context.Background(), 10*time.Second)
			defer stop()
			code, _, err := uc8LegacyHTTP(cleanupCtx, http.MethodDelete, uri, r.access_token)
			require.NoError(t, err)
			require.True(t, code/100 == 2 || code == 404, "legacy group cleanup HTTP %d", code)
		})
		command := func(args ...string) string {
			return uc8Command(t, r, "", append([]string{"cloud-init", "group"}, args...)...)
		}
		for _, mutation := range []string{"add", "set"} {
			payload := []any{map[string]any{"name": name, "description": mutation, "meta-data": map[string]string{"marker": mutation}, "file": map[string]string{"content": "#cloud-config\nuc8_marker: " + mutation + "\n", "encoding": "plain", "filename": "uc8.yaml"}}}
			data, err := json.Marshal(payload)
			require.NoError(t, err)
			command(mutation, "-d", string(data))
			require.Contains(t, command("get", "raw", name, "-F", "json"), name)
			require.Contains(t, command("get", "meta-data", name, "-F", "json"), mutation)
			code, body, err := uc8LegacyHTTP(ctx, http.MethodGet, uri, r.access_token)
			require.NoError(t, err)
			require.Equal(t, 200, code)
			require.Contains(t, string(body), mutation, "independent legacy API read")
		}
		require.Contains(t, command("render", name, "x0c0s0b0n0"), "uc8_marker: set")
		command("delete", name, "--no-confirm")
		code, _, err = uc8LegacyHTTP(ctx, http.MethodGet, uri, r.access_token)
		require.NoError(t, err)
		require.Equal(t, 404, code)
		for _, kind := range []string{"meta-data", "user-data", "vendor-data"} {
			args := []string{"cloud-init", "node", "get", kind, "x0c0s0b0n0"}
			if kind == "meta-data" {
				args = append(args, "-F", "json")
			} else {
				args = append(args, "--headers", "never")
			}
			out := uc8Command(t, r, "", args...)
			require.NotEmpty(t, out)
		}
	})
}
