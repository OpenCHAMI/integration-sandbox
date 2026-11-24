// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUncertainIssuerOutputLeavesRedactionFence(t *testing.T) {
	for _, tc := range []struct {
		name    string
		output  []byte
		failure bool
	}{
		{"malformed", []byte(`{"bootstrap_token":`), false},
		{"duplicate", []byte(`{"bootstrap_token":"one","bootstrap_token":"two"}`), false},
		{"oversized", []byte(strings.Repeat("x", 16385)), false},
		{"issuer failure", nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, fake, _ := identityFixture(t)
			fake.raw, fake.failure = tc.output, tc.failure
			if err := c.prepareBootIdentity(context.Background()); err == nil {
				t.Fatal("uncertain issuer response accepted")
			}
			info, err := os.Lstat(filepath.Join(c.runDir, identityIncomplete))
			if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 {
				t.Fatal("private incomplete fence missing")
			}
		})
	}
}

func TestExistingRedactionFencePreventsIssuance(t *testing.T) {
	c, fake, _ := identityFixture(t)
	if err := os.WriteFile(filepath.Join(c.runDir, identityIncomplete), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := c.prepareBootIdentity(context.Background()); err == nil || fake.issued != 0 {
		t.Fatal("issuance bypassed existing fence")
	}
}

func TestAuditFailureAfterMintStillProtectsToken(t *testing.T) {
	c, fake, _ := identityFixture(t)
	if err := os.WriteFile(filepath.Join(c.runDir, "credential-issuance.jsonl"), []byte("{}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := c.prepareBootIdentity(context.Background()); err == nil || fake.issued != 1 {
		t.Fatal("invalid audit accepted or mint not reached")
	}
	ledger, err := os.ReadFile(filepath.Join(c.runDir, "credentials.redactions"))
	if err != nil || !strings.Contains(string(ledger), fake.tokens[0]+"\n") {
		t.Fatal("minted token was not protected")
	}
	if _, err := os.Lstat(filepath.Join(c.runDir, identityIncomplete)); !os.IsNotExist(err) {
		t.Fatal("protected issuance left incomplete fence")
	}
}
