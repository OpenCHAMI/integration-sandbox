// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type identityFake struct {
	tokens  []string
	issued  int
	project string
	failure bool
	args    []string
	raw     []byte
}

func (f *identityFake) run(_ context.Context, args ...string) ([]byte, error) {
	switch args[0] {
	case "compose":
		return []byte("ts-owned\n"), nil
	case "inspect":
		v := container{ID: "ts-owned", Config: containerConfig{Labels: map[string]string{"com.docker.compose.project": f.project, "com.docker.compose.service": "tokensmith-backend", "openchami.acceptance.run": f.project}}}
		v.State.Running = true
		return json.Marshal([]container{v})
	case "exec":
		f.args = append([]string(nil), args...)
		if f.raw != nil {
			f.issued++
			return f.raw, nil
		}
		if f.failure || f.issued >= len(f.tokens) {
			return nil, errors.New("secret-untrusted-error")
		}
		token := f.tokens[f.issued]
		f.issued++
		return json.Marshal(struct {
			Token string `json:"bootstrap_token"`
		}{token})
	default:
		return nil, errors.New("unexpected fake command")
	}
}

func identityFixture(t *testing.T) (controller, *identityFake, string) {
	t.Helper()
	run, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(run, 0700); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(run, "config")
	if err := os.Mkdir(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "boot.json"), []byte(`{"tokensmith_bootstrap_token":"initial-secret","port":27790}`), 0444); err != nil {
		t.Fatal(err)
	}
	ledger := filepath.Join(run, "credentials.redactions")
	t.Setenv("SBX_DECLARATIVE_BOOT_CONFIG_DIR", dir)
	t.Setenv("SBX_DECLARATIVE_HSM_BOOTSTRAP_REDACTIONS", ledger)
	t.Setenv("SBX_DECLARATIVE_TOKENSMITH_STORE", "/tokensmith/data/bootstrap-tokens")
	t.Setenv("SBX_DECLARATIVE_TOKENSMITH_CONTAINER", "")
	f := &identityFake{project: "sandbox", tokens: []string{"fresh-credential-one", "fresh-credential-two"}}
	return controller{project: "sandbox", runDir: run, compose: "compose.yaml", exec: f}, f, dir
}

func TestPrepareIdentityRotatesPrivateConfig(t *testing.T) {
	c, fake, dir := identityFixture(t)
	bound, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer bound.Close()
	if err := c.prepareBootIdentity(context.Background()); err != nil {
		t.Fatal(err)
	}
	first, err := os.ReadFile(filepath.Join(dir, "boot.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := c.prepareBootIdentity(context.Background()); err != nil {
		t.Fatal(err)
	}
	second, err := os.ReadFile(filepath.Join(dir, "boot.json"))
	if err != nil || string(first) == string(second) || fake.issued != 2 {
		t.Fatal("credentials did not rotate")
	}
	if !strings.Contains(string(second), `"port":27790`) {
		t.Fatal("unrelated config changed")
	}
	wantArgs := []string{"exec", "ts-owned", "tokensmith", "bootstrap-token", "create", "--bootstrap-store=/tokensmith/data/bootstrap-tokens", "--subject=acceptance-hsm", "--audience=hsm", "--scopes=read:smd", "--ttl=1h", "--output-format=json"}
	if strings.Join(fake.args, "\x00") != strings.Join(wantArgs, "\x00") {
		t.Fatal("incorrect issuance policy")
	}
	visible, err := bound.Open("boot.json")
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(visible)
	if err != nil || string(data) != string(second) {
		t.Fatal("directory-bound config did not observe rename")
	}
	visible.Close()
	ledger, err := os.ReadFile(filepath.Join(c.runDir, "credentials.redactions"))
	if err != nil || !strings.Contains(string(ledger), "fresh-credential-one\n") || !strings.Contains(string(ledger), "fresh-credential-two\n") {
		t.Fatal("missing private redaction entries")
	}
	info, err := os.Stat(filepath.Join(dir, "boot.json"))
	if err != nil || info.Mode().Perm() != 0444 {
		t.Fatal("invalid config mode")
	}
	info, err = os.Stat(filepath.Join(c.runDir, "credentials.redactions"))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("invalid ledger mode")
	}
	audit, err := os.ReadFile(filepath.Join(c.runDir, "credential-issuance.jsonl"))
	if err != nil || strings.Contains(string(audit), "fresh-credential") || len(strings.Split(strings.TrimSpace(string(audit)), "\n")) != 2 {
		t.Fatal("invalid nonsecret issuance audit")
	}
}

func TestPrepareIdentityRejectsReusedToken(t *testing.T) {
	c, fake, _ := identityFixture(t)
	fake.tokens = []string{"fresh-credential-one", "fresh-credential-one"}
	if err := c.prepareBootIdentity(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := c.prepareBootIdentity(context.Background()); err == nil {
		t.Fatal("reused credential accepted")
	}
}

func TestPrepareIdentityRejectsUnownedTokenSmith(t *testing.T) {
	c, fake, _ := identityFixture(t)
	fake.project = "unrelated"
	if err := c.prepareBootIdentity(context.Background()); err == nil || fake.issued != 0 {
		t.Fatal("unowned backend used")
	}
}

func TestPrepareIdentityRejectsUnsafeConfigMode(t *testing.T) {
	c, fake, dir := identityFixture(t)
	if err := os.Chmod(dir, 0777); err != nil {
		t.Fatal(err)
	}
	if err := c.prepareBootIdentity(context.Background()); err == nil || fake.issued != 0 {
		t.Fatal("unsafe config permitted issuance")
	}
}

func TestPrepareIdentityRedactsIssuerFailure(t *testing.T) {
	c, fake, _ := identityFixture(t)
	fake.failure = true
	err := c.prepareBootIdentity(context.Background())
	if err == nil || strings.Contains(err.Error(), "secret-untrusted-error") {
		t.Fatal("issuer error leaked")
	}
	audit, readErr := os.ReadFile(filepath.Join(c.runDir, "credential-issuance.jsonl"))
	if readErr != nil || strings.Contains(string(audit), "secret") {
		t.Fatal("issuer failure leaked to audit")
	}
}

func TestPrepareIdentityRejectsUnsafeRedactionLedger(t *testing.T) {
	c, fake, _ := identityFixture(t)
	path := filepath.Join(c.runDir, "credentials.redactions")
	if err := os.WriteFile(path, nil, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if err := c.prepareBootIdentity(context.Background()); err == nil || fake.issued != 0 {
		t.Fatal("unsafe ledger permitted issuance")
	}
}

func TestPrepareIdentityRejectsUnexpectedStore(t *testing.T) {
	c, fake, _ := identityFixture(t)
	t.Setenv("SBX_DECLARATIVE_TOKENSMITH_STORE", "/other")
	if err := c.prepareBootIdentity(context.Background()); err == nil || fake.issued != 0 {
		t.Fatal("unexpected store permitted issuance")
	}
}
