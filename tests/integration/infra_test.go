//go:build integration
// +build integration

// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
//
// SPDX-License-Identifier: MIT

package integration

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestVault_DevReady — the Vault dev container should report sealed=false, initialized=true.
func TestVault_DevReady(t *testing.T) {
	url := Endpoints["vault"] + "/v1/sys/health"
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	resp, err := httpClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	body := readAll(t, resp)
	require.Equal(t, http.StatusOK, resp.StatusCode, "vault sys/health: %s", body)
	require.Contains(t, body, `"sealed":false`)
	require.Contains(t, body, `"initialized":true`)
}

// TestLocalstack_S3Healthy — localstack /_localstack/health reports s3 as available.
func TestLocalstack_S3Healthy(t *testing.T) {
	url := Endpoints["localstack"] + "/_localstack/health"
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	resp, err := httpClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	body := readAll(t, resp)
	require.Equal(t, http.StatusOK, resp.StatusCode, body)
	require.True(t,
		strings.Contains(body, `"s3":"available"`) ||
			strings.Contains(body, `"s3": "available"`) ||
			strings.Contains(body, `"s3":"running"`) ||
			strings.Contains(body, `"s3": "running"`),
		"expected s3 service available/running, got: %s", body)
}
