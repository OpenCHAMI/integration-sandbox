//go:build integration
// +build integration

// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
//
// SPDX-License-Identifier: MIT

package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestSMD_ReadyEndpoint — confirms SMD is alive.
func TestSMD_ReadyEndpoint(t *testing.T) {
	url := Endpoints["smd"] + "/hsm/v2/service/ready"
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	resp, err := httpClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
}

// TestSMD_HasSeededComponents — after `make seed`, all 8 sandbox xnames must be present.
func TestSMD_HasSeededComponents(t *testing.T) {
	url := Endpoints["smd"] + "/hsm/v2/State/Components?type=Node"
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	resp, err := httpClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var payload struct {
		Components []struct {
			ID string `json:"ID"`
		} `json:"Components"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&payload))

	got := make(map[string]bool)
	for _, c := range payload.Components {
		got[c.ID] = true
	}

	for _, x := range Xnames {
		require.Truef(t, got[x+"n0"], "expected node %sn0 in SMD components, got=%v", x, got)
	}
}

// TestSMD_HasSeededRedfishEndpoints — after `make seed`, all 8 BMCs must be registered.
func TestSMD_HasSeededRedfishEndpoints(t *testing.T) {
	url := Endpoints["smd"] + "/hsm/v2/Inventory/RedfishEndpoints"
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	resp, err := httpClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var payload struct {
		RedfishEndpoints []struct {
			ID string `json:"ID"`
		} `json:"RedfishEndpoints"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&payload))

	got := make(map[string]bool)
	for _, e := range payload.RedfishEndpoints {
		got[e.ID] = true
	}
	for _, x := range Xnames {
		require.Truef(t, got[x], "expected RedfishEndpoint %s in SMD inventory, got=%v", x, got)
	}
}
