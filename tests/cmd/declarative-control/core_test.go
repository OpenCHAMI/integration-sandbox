// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTokenFixtureUsesLoopbackFacade(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/__fixtures/token" {
			t.Error("wrong fixture request")
		}
		var body struct {
			Kind   string   `json:"kind"`
			Scopes []string `json:"scopes"`
		}
		if json.NewDecoder(r.Body).Decode(&body) != nil || body.Kind != "expired" || len(body.Scopes) != 1 || body.Scopes[0] != "read:boot" {
			t.Error("wrong token request")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"token":"test-secret"}`))
	}))
	defer server.Close()
	t.Setenv("SBX_DECLARATIVE_FIXTURE_URL", server.URL)
	result, err := tokenFixture(context.Background(), controller{}, json.RawMessage(`{"kind":"expired","scopes":["read:boot"]}`))
	if err != nil || string(result) != `{"token":"test-secret"}` {
		t.Fatal("fixture response not returned")
	}
}

func TestTokenFixtureRejectsRemoteOrigin(t *testing.T) {
	t.Setenv("SBX_DECLARATIVE_FIXTURE_URL", "http://example.com:80")
	if _, err := tokenFixture(context.Background(), controller{}, json.RawMessage(`{"kind":"expired","scopes":["read:boot"]}`)); err == nil {
		t.Fatal("remote origin accepted")
	}
}

func TestAssetRegistrationRejectsTraversal(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	c := controller{project: "sandbox", runDir: dir}
	entry := asset{Kind: "bind-directory", Name: "copy", Project: "sandbox", Service: "boot", Path: filepath.Dir(dir)}
	if err := c.registerAsset(entry); err == nil {
		t.Fatal("outside directory accepted")
	}
	if _, err := os.Stat(filepath.Join(dir, "assets.jsonl")); !os.IsNotExist(err) {
		t.Fatal("invalid asset was recorded")
	}
}

func TestInvalidCLILeavesStdoutEmpty(t *testing.T) {
	var out bytes.Buffer
	if err := execute(context.Background(), []string{"token-fixture", `{`}, &out); err == nil || out.Len() != 0 {
		t.Fatal("invalid JSON accepted or output written")
	}
}

func TestRequestRejectsUnknownFields(t *testing.T) {
	_, err := decodeRequest[struct {
		Service string `json:"service"`
	}](json.RawMessage(`{"service":"boot","extra":true}`))
	if err == nil {
		t.Fatal("unknown field accepted")
	}
}

func TestRequestRejectsDuplicateFields(t *testing.T) {
	_, err := decodeRequest[struct {
		Service string `json:"service"`
	}](json.RawMessage(`{"service":"smd","service":"boot"}`))
	if err == nil {
		t.Fatal("duplicate field accepted")
	}
}

func TestRequestRejectsWrongFieldCase(t *testing.T) {
	_, err := decodeRequest[struct {
		Service string `json:"service"`
	}](json.RawMessage(`{"Service":"boot"}`))
	if err == nil {
		t.Fatal("case-variant field accepted")
	}
}

func TestAssetRegistrationWritesJSONL(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	c := controller{project: "sandbox", runDir: dir}
	a := asset{Kind: "container", Name: "copy-one", Project: "sandbox", Service: "boot"}
	if err := c.registerAsset(a); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(dir, "assets.jsonl"))
	if err != nil || !strings.HasSuffix(string(body), "\n") {
		t.Fatal("asset JSONL missing")
	}
	var got asset
	if json.Unmarshal(body, &got) != nil || got != a {
		t.Fatal("asset changed")
	}
}
