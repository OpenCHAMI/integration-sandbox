// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/openchami/integration-sandbox/tests/internal/quiescence"
)

func TestInitialIdentityActionUsesSharedPipeline(t *testing.T) {
	c, fake, dir := identityFixture(t)
	if err := os.Remove(filepath.Join(dir, "boot.json")); err != nil {
		t.Fatal(err)
	}
	lock := filepath.Join(c.runDir, "quiescence.lock")
	if err := os.WriteFile(lock, nil, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SBX_DECLARATIVE_QUIESCENCE_LOCK", lock)
	handler := actions["prepare-boot-identity"]
	if handler == nil {
		t.Fatal("initial identity action is not registered")
	}
	result, err := handler(context.Background(), c, json.RawMessage(`{}`))
	if err != nil || string(result) != `{}` {
		t.Fatal("initial issuance failed")
	}
	sum := sha256.Sum256([]byte(fake.tokens[0]))
	audit, err := os.ReadFile(filepath.Join(c.runDir, "credential-issuance.jsonl"))
	if err != nil || !strings.Contains(string(audit), "sha256:"+hex.EncodeToString(sum[:])) {
		t.Fatal("initial audit missing token digest")
	}
	if err := c.prepareBootIdentity(context.Background()); err != nil {
		t.Fatal("fresh rotation after initialization failed")
	}
}

func TestIssuedDuplicateIsRecordedForRedaction(t *testing.T) {
	c, fake, _ := identityFixture(t)
	fake.tokens = []string{"duplicate-token", "duplicate-token"}
	if err := c.prepareBootIdentity(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := c.prepareBootIdentity(context.Background()); err == nil {
		t.Fatal("duplicate accepted")
	}
	data, err := os.ReadFile(filepath.Join(c.runDir, "credentials.redactions"))
	if err != nil || strings.Count(string(data), "duplicate-token\n") != 2 {
		t.Fatal("issued duplicate not registered for redaction")
	}
}

func TestInitialIdentityRejectsAmbiguityBeforeIssuance(t *testing.T) {
	c, fake, _ := identityFixture(t)
	lock := filepath.Join(c.runDir, "quiescence.lock")
	if err := os.WriteFile(lock, nil, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SBX_DECLARATIVE_QUIESCENCE_LOCK", lock)
	writer, err := quiescence.BeginWrite(context.Background(), lock, "http")
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.Abandon(); err != nil {
		t.Fatal(err)
	}
	if _, err := actions["prepare-boot-identity"](context.Background(), c, json.RawMessage(`{}`)); err == nil || fake.issued != 0 {
		t.Fatal("ambiguous barrier permitted issuance")
	}
}

func TestInitialIdentityRejectsUnexpectedFields(t *testing.T) {
	c, fake, _ := identityFixture(t)
	if _, err := actions["prepare-boot-identity"](context.Background(), c, json.RawMessage(`{"extra":true}`)); err == nil || fake.issued != 0 {
		t.Fatal("invalid action input permitted issuance")
	}
}
