//go:build integration && declarative_boot

// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package declarativeboot

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestTokenHelpersExchangeAndVerify(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	claims := jwt.MapClaims{"iss": "issuer", "aud": []string{"boot-service"}, "exp": time.Now().Add(time.Minute).Unix(), "scope": []string{"read:boot", "propose:boot"}}
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = "test-key"
	signed, err := token.SignedString(key)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth/token":
			if err := r.ParseForm(); err != nil {
				t.Error(err)
				return
			}
			if r.Form.Get("subject_token") != "one-shot" || r.Form.Get("subject_token_type") != "urn:openchami:params:oauth:token-type:bootstrap-token" || r.Form.Get("grant_type") != "urn:ietf:params:oauth:grant-type:token-exchange" {
				t.Error("incorrect exchange form")
			}
			if err := json.NewEncoder(w).Encode(struct {
				AccessToken string `json:"access_token"`
				TokenType   string `json:"token_type"`
			}{signed, "Bearer"}); err != nil {
				t.Error(err)
			}
		case "/.well-known/jwks.json":
			fmt.Fprintf(w, `{"keys":[{"kty":"RSA","kid":"test-key","n":%q,"e":%q}]}`, base64.RawURLEncoding.EncodeToString(key.N.Bytes()), base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes()))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client, err := NewHTTPClient(server.URL, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	got, err := ExchangeToken(context.Background(), client, "one-shot")
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyToken(context.Background(), client, got, TokenPolicy{Issuer: "issuer", Audience: "boot-service", Scopes: []string{"read:boot"}}); err != nil {
		t.Fatal(err)
	}
	badKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	bad, err := token.SignedString(badKey)
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyToken(context.Background(), client, bad, TokenPolicy{Issuer: "issuer", Audience: "boot-service"}); err == nil || strings.Contains(err.Error(), bad) {
		t.Fatalf("invalid signature accepted or leaked: %v", err)
	}
	for _, policy := range []TokenPolicy{
		{Issuer: "wrong", Audience: "boot-service"},
		{Issuer: "issuer", Audience: "wrong"},
		{Issuer: "issuer", Audience: "boot-service", Scopes: []string{"promote:boot"}},
	} {
		if err := VerifyToken(context.Background(), client, signed, policy); err == nil || strings.Contains(err.Error(), signed) {
			t.Fatalf("invalid policy accepted or token leaked: %v", err)
		}
	}
}

func TestTokenHelpersBootstrapCommandFailureRedacted(t *testing.T) {
	runner := func(ctx context.Context, args ...string) ([]byte, error) {
		return []byte("secret"), fmt.Errorf("secret")
	}
	_, err := CreateBootstrapToken(context.Background(), runner, BootstrapOptions{Container: "sandbox-tokensmith", Store: "/tokensmith/data/bootstrap-tokens", Subject: "subject", Audience: "boot-service", Scopes: []string{"read:boot"}, TTL: "5m"})
	if err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatalf("expected redacted failure: %v", err)
	}
}

func TestTokenHelpersBootstrapCommand(t *testing.T) {
	runner := func(ctx context.Context, args ...string) ([]byte, error) {
		if len(args) != 11 || args[0] != "exec" || args[1] != "sandbox-tokensmith" || args[2] != "tokensmith" || args[3] != "bootstrap-token" || args[4] != "create" || args[5] != "--bootstrap-store=/tokensmith/data/bootstrap-tokens" || args[8] != "--scopes=read:boot,propose:boot" {
			t.Errorf("unexpected bootstrap command: %v", args)
		}
		return []byte(`{"bootstrap_token":"opaque"}`), nil
	}
	got, err := CreateBootstrapToken(context.Background(), runner, BootstrapOptions{Container: "sandbox-tokensmith", Store: "/tokensmith/data/bootstrap-tokens", Subject: "subject", Audience: "boot-service", Scopes: []string{"read:boot", "propose:boot"}, TTL: "5m"})
	if err != nil || got != "opaque" {
		t.Fatalf("bootstrap failed: %v", err)
	}
}
