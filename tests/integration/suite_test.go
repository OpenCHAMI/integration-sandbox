//go:build integration
// +build integration

// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
//
// SPDX-License-Identifier: MIT

package integration

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

// Endpoints is the canonical map of services-under-test, keyed by short name.
// Override any of these via env vars: SBX_<KEY>_URL.
var Endpoints = map[string]string{
	"vault":       envOr("SBX_VAULT_URL", "http://127.0.0.1:8200"),
	"localstack":  envOr("SBX_LOCALSTACK_URL", "http://127.0.0.1:4566"),
	"smd":         envOr("SBX_SMD_URL", "http://127.0.0.1:27779"),
	"tokensmith":  envOr("SBX_TOKENSMITH_URL", "http://127.0.0.1:27780"),
	"boot":        envOr("SBX_BOOT_URL", "http://127.0.0.1:27791"),
	"metadata":    envOr("SBX_METADATA_URL", "http://127.0.0.1:27792"),
	"fru":         envOr("SBX_FRU_URL", "http://127.0.0.1:27793"),
	"power":       envOr("SBX_POWER_URL", "http://127.0.0.1:28007"),
	"redfish-bmc": envOr("SBX_REDFISH_URL", "https://127.0.0.1:5000"),
}

// Xnames is the canonical fake-fleet list. Eight nodes, all sims.
var Xnames = []string{
	"x0c0s0b0", "x0c0s1b0", "x0c0s2b0", "x0c0s3b0",
	"x0c0s4b0", "x0c0s5b0", "x0c0s6b0", "x0c0s7b0",
}

func envOr(k, d string) string {
	if v := strings.TrimSpace(os.Getenv(k)); v != "" {
		return v
	}
	return d
}

// httpClient is shared across the suite for connection reuse.
// InsecureSkipVerify is required for the BMC emulator's self-signed cert
// (sushy-tools' --fake driver mints an ephemeral cert at container start).
var httpClient = &http.Client{
	Timeout: 10 * time.Second,
	Transport: &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	},
}

// TestMain runs once before any test in the package; ensures every endpoint is reachable.
func TestMain(m *testing.M) {
	// quick liveness sweep — keep diagnostic so a CI failure here is informative.
	timeout := 30 * time.Second
	if v := os.Getenv("SBX_WAIT_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			timeout = d
		}
	}
	deadline := time.Now().Add(timeout)
	for name, url := range Endpoints {
		ok := false
		for time.Now().Before(deadline) {
			req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
			resp, err := httpClient.Do(req)
			if err == nil && (resp.StatusCode < 500) {
				resp.Body.Close()
				ok = true
				break
			}
			if resp != nil {
				resp.Body.Close()
			}
			time.Sleep(2 * time.Second)
		}
		if !ok {
			fmt.Fprintf(os.Stderr, "suite setup: endpoint %s (%s) not reachable; tests may fail\n", name, url)
		}
	}
	os.Exit(m.Run())
}
