//go:build integration && declarative_boot

// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package declarativeboot

import (
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"slices"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func safetyIssuer() string {
	if issuer := os.Getenv("SBX_DECLARATIVE_ISSUER"); issuer != "" {
		return issuer
	}
	return "http://tokensmith:27780"
}

func (f *safetyFixture) keys() (safetyFixtureKeys, *HTTPClient) {
	f.t.Helper()
	client, err := NewHTTPClient(safetyEnv(f.t, "SBX_DECLARATIVE_TOKENSMITH_URL"), 10*time.Second)
	if err != nil {
		f.t.Fatal(err)
	}
	response, err := client.Do(f.ctx, Request{Method: http.MethodGet, Path: "/.well-known/jwks.json"})
	safetyStatus(f.t, response, err, http.StatusOK)
	keys, err := DecodeJSON[safetyFixtureKeys](response)
	if err != nil || len(keys.Keys) == 0 {
		f.t.Fatal("invalid TokenSmith JWKS")
	}
	return keys, client
}

func (f *safetyFixture) role(scopes []string) BootClient {
	f.t.Helper()
	keys, client := f.keys()
	bootstrap, err := CreateBootstrapToken(f.ctx, DockerExec, BootstrapOptions{Container: safetyEnv(f.t, "SBX_DECLARATIVE_TOKENSMITH_CONTAINER"), Store: safetyEnv(f.t, "SBX_DECLARATIVE_TOKENSMITH_STORE"), Subject: safetyName(f.t), Audience: "boot-service", Scopes: scopes, TTL: "5m"})
	if err != nil {
		f.t.Fatal("role bootstrap failed")
	}
	raw, err := ExchangeToken(f.ctx, client, bootstrap)
	if err != nil {
		f.t.Fatal("role token exchange failed")
	}
	claims := &tokenClaims{}
	parsed, err := jwt.ParseWithClaims(raw, claims, func(token *jwt.Token) (any, error) { return safetyFixtureKeyFor(keys, token) }, jwt.WithValidMethods([]string{"RS256", "RS384", "RS512", "PS256", "PS384", "PS512"}), jwt.WithIssuer(safetyIssuer()), jwt.WithAudience("boot-service"), jwt.WithExpirationRequired())
	if err != nil || parsed == nil || !parsed.Valid || len(claims.Scope) != len(scopes) {
		f.t.Fatal("role JWT verification or scope set failed")
	}
	for _, scope := range scopes {
		if !slices.Contains(claims.Scope, scope) {
			f.t.Fatal("role JWT scope set failed")
		}
	}
	return BootClient{HTTP: f.client.HTTP, Bearer: raw}
}

func (f *safetyFixture) claimFixture(kind string, keys safetyFixtureKeys) string {
	f.t.Helper()
	scopes := []string{"read:boot", "propose:boot", "promote:boot"}
	request, err := json.Marshal(tokenFixtureRequest{Kind: kind, Scopes: scopes})
	if err != nil {
		f.t.Fatal("encode token fixture request")
	}
	bin := safetyEnv(f.t, "SBX_DECLARATIVE_CONTROL_BIN")
	output, err := exec.CommandContext(f.ctx, bin, "token-fixture", string(request)).Output()
	if err != nil {
		f.t.Fatalf("token fixture adapter failed for %s", kind)
	}
	var result tokenFixtureResponse
	if json.Unmarshal(output, &result) != nil || result.Token == "" {
		f.t.Fatalf("invalid token fixture output for %s", kind)
	}
	if err := safetyInspectFixture(result.Token, kind, keys, safetyClaimPolicy{Issuer: safetyIssuer(), Now: time.Now(), Scopes: scopes}); err != nil {
		f.t.Fatalf("token fixture inspection failed for %s", kind)
	}
	return result.Token
}
