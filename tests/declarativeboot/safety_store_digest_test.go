//go:build integration && declarative_boot

// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package declarativeboot

import (
	"context"
	"encoding/hex"
	"strings"
	"testing"
)

func safetyStoreDigest(t *testing.T, ctx context.Context, instance string) (string, safetyControlReceipt) {
	t.Helper()
	var result struct {
		ConfigurationDigest string `json:"configurationDigest"`
	}
	receipt := safetyControlAction(t, ctx, "snapshot-store", struct {
		Instance string `json:"instance"`
	}{Instance: instance}, &result)
	if receipt.Instance != instance {
		t.Fatal("store receipt identifies the wrong instance")
	}
	if instance == "auth-off" && receipt.Service != safetyEnv(t, "SBX_DECLARATIVE_AUTH_OFF_SERVICE") {
		t.Fatal("auth-off receipt identifies the wrong compose service")
	}
	digest := strings.TrimPrefix(result.ConfigurationDigest, "sha256:")
	decoded, err := hex.DecodeString(digest)
	if err != nil || len(decoded) != 32 {
		t.Fatal("invalid configuration digest")
	}
	return digest, receipt
}
