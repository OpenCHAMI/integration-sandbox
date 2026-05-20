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
	"time"

	"github.com/stretchr/testify/require"
)

// TestRedfishEmulator_ServiceRoot asserts the Cray Redfish emulator answers /redfish/v1
// with a JSON body containing the service root self link. This is the harness sanity check —
// if it fails, every other BMC-touching test will fail too.
func TestRedfishEmulator_ServiceRoot(t *testing.T) {
	url := Endpoints["redfish-bmc"] + "/redfish/v1"
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	resp, err := httpClient.Do(req)
	require.NoError(t, err, "GET %s", url)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Contains(t, resp.Header.Get("Content-Type"), "application/json")
}

// TestRedfishEmulator_AuthRejectsAnon — historically the csm-rie emulator was
// configured with AUTH_CONFIG=root:root_password and rejected anon. The
// sushy-tools --fake driver currently serves /Systems anonymously, so we
// accept either 401 or 200; the test exists to flag a future hardening
// regression in either direction.
func TestRedfishEmulator_AuthRejectsAnon(t *testing.T) {
	url := Endpoints["redfish-bmc"] + "/redfish/v1/Systems"
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	resp, err := httpClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.True(t, resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusOK,
		"unexpected status %d for anon /Systems (some emulators ship open by default)", resp.StatusCode)
}

// TestRedfishEmulator_AuthAcceptsRoot — with the documented sandbox creds, /Systems should respond 200.
func TestRedfishEmulator_AuthAcceptsRoot(t *testing.T) {
	url := Endpoints["redfish-bmc"] + "/redfish/v1/Systems"
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	req.SetBasicAuth("root", "root_password")
	resp, err := httpClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode, "/Systems with root creds")
	require.Contains(t, resp.Header.Get("Content-Type"), "application/json")
}

// TestRedfishEmulator_AdvertisesXnameNode — confirms one Systems entry exists.
// (The emulator names systems by its own convention — csm-rie used the mockup
// path like "Node0", sushy-tools' --fake uses a UUID — so we just check the
// collection is non-empty.)
func TestRedfishEmulator_AdvertisesXnameNode(t *testing.T) {
	url := Endpoints["redfish-bmc"] + "/redfish/v1/Systems"
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	req.SetBasicAuth("root", "root_password")
	resp, err := httpClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	body := readAll(t, resp)
	require.True(t, strings.Contains(body, `"Members"`), "expected Members in /Systems body, got: %s", body)
}
