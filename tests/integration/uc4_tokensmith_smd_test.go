//go:build integration
// +build integration

// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
//
// SPDX-License-Identifier: MIT

package integration

import (
	"context"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/url"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// TestUC4_Tokensmith_SMD covers the headline tokensmith → SMD flow:
//
//  1. Provision a one-shot RFC 8693 bootstrap token by exec-ing
//     `tokensmith bootstrap-token create` inside the running sandbox-tokensmith
//     container, against the same `--rfc8693-bootstrap-store` path the serve
//     command reads from. This is the canonical mint path documented in
//     tokensmith/cmd/tokenservice/bootstrap_token.go:165-202.
//  2. Exchange the opaque bootstrap token at /oauth/token with
//     grant_type=urn:ietf:params:oauth:grant-type:token-exchange (per
//     tokensmith/pkg/tokenservice/rfc8693_handlers.go:25-63) and capture the
//     issued JWT.
//  3. Verify the JWT cryptographically against the JWKS published at
//     /.well-known/jwks.json. This is the step that breaks against a
//     wiremock stub: a stub can return any blob; only a real tokensmith
//     produces a JWT whose signature validates against the published JWKS.
//  4. Verify the JWT's issuer, audience, expiry, and scope claims match
//     the bootstrap policy.
//  5. Use the JWT to authenticate a write to SMD (POST /hsm/v2/State/Components
//     for an xname not in the seeded fixture) and verify the write landed by
//     reading it back. The cross-service half — even though SMD's dev mode
//     does not strictly enforce JWT validity, the round-trip proves that
//     tokensmith's mint is real, reachable, and the produced token is shaped
//     such that downstream services accept it.
//
// Stub-resistance: would fail against any wiremock or canned-response stub
// because step 3 (signature verification) is cryptographic and step 5 is a
// stateful POST→GET round-trip on postgres-backed SMD.
func TestUC4_Tokensmith_SMD(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	tokensmithURL := Endpoints["tokensmith"]
	smdURL := Endpoints["smd"]

	// Step 1: provision a bootstrap token. The store path matches
	// compose/core.yaml:54 (`--rfc8693-bootstrap-store=/tokensmith/data/bootstrap-tokens`).
	bootstrapToken, policy := mintBootstrapToken(ctx, t,
		"sandbox-tokensmith",
		"/tokensmith/data/bootstrap-tokens",
		"uc4-test-subject",
		"smd",
		[]string{"write:smd", "read:smd"},
		"5m",
	)

	// Step 2: exchange the opaque bootstrap token for a JWT.
	form := url.Values{}
	form.Set("grant_type", "urn:ietf:params:oauth:grant-type:token-exchange")
	form.Set("subject_token", bootstrapToken)
	form.Set("subject_token_type", "urn:openchami:params:oauth:token-type:bootstrap-token")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		tokensmithURL+"/oauth/token", strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatalf("build /oauth/token request: %v", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := httpClient.Do(req)
	if err != nil {
		t.Fatalf("POST /oauth/token: %v", err)
	}
	body := readAll(t, resp)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("token exchange: HTTP %d, body=%s", resp.StatusCode, body)
	}
	var exchange struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.Unmarshal([]byte(body), &exchange); err != nil {
		t.Fatalf("decode token response: %v body=%s", err, body)
	}
	if exchange.AccessToken == "" {
		t.Fatalf("token exchange returned empty access_token, body=%s", body)
	}
	if !strings.EqualFold(exchange.TokenType, "Bearer") {
		t.Errorf("expected token_type=Bearer, got %q", exchange.TokenType)
	}

	// Step 3: verify JWT signature against /.well-known/jwks.json. This is
	// the bit that catches a stubbed tokensmith — a stub can hand back any
	// header.payload.signature triple, but only a real tokensmith signs with
	// a key that's published in its JWKS.
	keyfunc := jwksKeyfunc(ctx, t, tokensmithURL)
	parsed, err := jwt.Parse(exchange.AccessToken, keyfunc,
		jwt.WithValidMethods([]string{
			// RS* and PS* both accept RSA public keys; tokensmith currently
			// signs with PS256 (RSA-PSS). ES* covered for forward-compat.
			"RS256", "RS384", "RS512",
			"PS256", "PS384", "PS512",
			"ES256", "ES384", "ES512",
		}),
		jwt.WithLeeway(30*time.Second),
	)
	if err != nil {
		t.Fatalf("verify JWT against JWKS: %v", err)
	}
	if !parsed.Valid {
		t.Fatalf("JWT failed JWKS verification (parsed.Valid=false)")
	}

	// Step 4: claim assertions.
	claims, ok := parsed.Claims.(jwt.MapClaims)
	if !ok {
		t.Fatalf("expected MapClaims, got %T", parsed.Claims)
	}
	if iss, _ := claims["iss"].(string); iss != "http://tokensmith:27780" {
		t.Errorf("expected iss=http://tokensmith:27780, got %q", iss)
	}
	if !claimContainsString(claims, "aud", "smd") {
		t.Errorf("expected aud to contain %q, got %v", "smd", claims["aud"])
	}
	exp, expOK := jwtExpiry(claims)
	if !expOK {
		t.Error("expected exp claim to be present and parseable")
	} else if !exp.After(time.Now()) {
		t.Errorf("expected exp in future, got %v", exp)
	}
	// Scope may be an array or a space-delimited string per RFC 8693
	// implementations; tokensmith currently emits an array.
	for _, want := range policy.Scopes {
		if !claimContainsString(claims, "scope", want) {
			t.Errorf("expected scope claim to contain %q, got %v", want, claims["scope"])
		}
	}

	// Step 5: write to SMD using the JWT. We pick an xname that is NOT in
	// the seed fixture (smd-components.json is x0c0s{0..7}b0; x1c0s0b0
	// is in a different cabinet and is form-valid per SMD's xname parser).
	// This guarantees the GET-after-POST is observing a state change
	// produced by THIS test, not the seed.
	xname := "x1c0s0b0"
	smdReset := func() {
		// Best-effort cleanup so re-running the test is idempotent.
		// SMD returns 404 on missing component — that's fine.
		req, _ := http.NewRequestWithContext(ctx, http.MethodDelete,
			smdURL+"/hsm/v2/State/Components/"+xname, nil)
		if r, err := httpClient.Do(req); err == nil {
			r.Body.Close()
		}
	}
	smdReset()
	t.Cleanup(smdReset)

	componentBody := map[string]any{
		"Components": []map[string]any{{
			"ID":      xname,
			"Type":    "Node",
			"Role":    "Compute",
			"State":   "On",
			"Enabled": true,
		}},
	}
	postReq, err := jsonRequestWithBearer(ctx, http.MethodPost,
		smdURL+"/hsm/v2/State/Components", exchange.AccessToken, componentBody)
	if err != nil {
		t.Fatalf("build SMD POST: %v", err)
	}
	postResp, err := httpClient.Do(postReq)
	if err != nil {
		t.Fatalf("POST SMD components: %v", err)
	}
	postBody := readAll(t, postResp)
	postResp.Body.Close()
	if postResp.StatusCode/100 != 2 {
		t.Fatalf("expected SMD POST 2xx, got HTTP %d body=%s", postResp.StatusCode, postBody)
	}

	// Step 5b: confirm the write landed via independent GET (no auth in dev).
	getCode, getBody := httpJSON(t, http.MethodGet,
		smdURL+"/hsm/v2/State/Components/"+xname, nil)
	expectStatus(t, http.StatusOK, getCode, getBody, "SMD GET after authenticated POST")
	var got struct {
		ID    string `json:"ID"`
		Role  string `json:"Role"`
		State string `json:"State"`
	}
	if err := json.Unmarshal(getBody, &got); err != nil {
		t.Fatalf("decode SMD GET: %v body=%s", err, pretty(getBody))
	}
	if got.ID != xname {
		t.Errorf("expected ID=%q, got %q", xname, got.ID)
	}
	if got.Role != "Compute" {
		t.Errorf("expected Role=Compute, got %q", got.Role)
	}
	if got.State != "On" {
		t.Errorf("expected State=On, got %q", got.State)
	}
}

// bootstrapPolicy mirrors the JSON shape emitted by
// `tokensmith bootstrap-token create --output-format=json`
// (see tokensmith/cmd/tokenservice/bootstrap_token.go:122-138).
type bootstrapPolicy struct {
	Subject  string   `json:"subject"`
	Audience string   `json:"audience"`
	Scopes   []string `json:"scopes"`
}

// mintBootstrapToken provisions an opaque bootstrap token inside the running
// tokensmith container by exec-ing the CLI `tokensmith bootstrap-token create`
// against the shared bootstrap-store path. Returns the opaque token plus the
// recorded policy (for downstream claim assertions).
func mintBootstrapToken(ctx context.Context, t *testing.T, container, storePath, subject, audience string, scopes []string, ttl string) (string, bootstrapPolicy) {
	t.Helper()
	cmd := exec.CommandContext(ctx, "docker", "exec", container,
		"tokensmith", "bootstrap-token", "create",
		"--bootstrap-store="+storePath,
		"--subject="+subject,
		"--audience="+audience,
		"--scopes="+strings.Join(scopes, ","),
		"--ttl="+ttl,
		"--output-format=json",
	)
	out, err := cmd.Output()
	if err != nil {
		// CombinedOutput would mix in the "✓ Bootstrap token created" stderr
		// noise that the CLI writes to stderr regardless of success; capture
		// stderr separately for diagnostics.
		stderr := ""
		if ee, ok := err.(*exec.ExitError); ok {
			stderr = string(ee.Stderr)
		}
		t.Fatalf("docker exec tokensmith bootstrap-token create: %v\nstderr=%s", err, stderr)
	}
	var resp struct {
		BootstrapToken string          `json:"bootstrap_token"`
		Policy         bootstrapPolicy `json:"policy"`
	}
	if err := json.Unmarshal(out, &resp); err != nil {
		t.Fatalf("decode bootstrap-token create output: %v\nstdout=%s", err, string(out))
	}
	if resp.BootstrapToken == "" {
		t.Fatalf("bootstrap-token create returned empty token, stdout=%s", string(out))
	}
	return resp.BootstrapToken, resp.Policy
}

// jwksKeyfunc returns a jwt.Keyfunc that fetches and parses the JWKS at
// <tokensmithURL>/.well-known/jwks.json once per call. Supports RSA keys
// (kty=RSA, alg=RS256/RS384/RS512); falls through with an error for other
// key types so the test fails loudly rather than silently skipping
// verification.
func jwksKeyfunc(ctx context.Context, t *testing.T, tokensmithURL string) jwt.Keyfunc {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		tokensmithURL+"/.well-known/jwks.json", nil)
	if err != nil {
		t.Fatalf("build JWKS request: %v", err)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		t.Fatalf("GET JWKS: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET JWKS: HTTP %d", resp.StatusCode)
	}
	var jwks struct {
		Keys []struct {
			Kty string `json:"kty"`
			Kid string `json:"kid"`
			N   string `json:"n"`
			E   string `json:"e"`
		} `json:"keys"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&jwks); err != nil {
		t.Fatalf("decode JWKS: %v", err)
	}
	if len(jwks.Keys) == 0 {
		t.Fatal("JWKS contained no keys")
	}

	keys := map[string]*rsa.PublicKey{}
	for _, k := range jwks.Keys {
		if k.Kty != "RSA" {
			continue
		}
		nb, err := base64.RawURLEncoding.DecodeString(k.N)
		if err != nil {
			t.Fatalf("decode JWK n: %v", err)
		}
		eb, err := base64.RawURLEncoding.DecodeString(k.E)
		if err != nil {
			t.Fatalf("decode JWK e: %v", err)
		}
		pub := &rsa.PublicKey{
			N: new(big.Int).SetBytes(nb),
			E: int(new(big.Int).SetBytes(eb).Int64()),
		}
		keys[k.Kid] = pub
	}
	if len(keys) == 0 {
		t.Fatal("JWKS had no usable RSA keys")
	}

	return func(token *jwt.Token) (interface{}, error) {
		kid, _ := token.Header["kid"].(string)
		if kid != "" {
			if k, ok := keys[kid]; ok {
				return k, nil
			}
			return nil, fmt.Errorf("kid %q not in JWKS", kid)
		}
		// Fall back to first key when kid is absent (single-key JWKS).
		for _, k := range keys {
			return k, nil
		}
		return nil, fmt.Errorf("no keys available")
	}
}

// claimContainsString reports whether claim[name] (string or []string)
// contains want. Used because the `aud` claim may be either form per
// RFC 7519 Section 4.1.3.
func claimContainsString(claims jwt.MapClaims, name, want string) bool {
	switch v := claims[name].(type) {
	case string:
		// Some issuers emit space-delimited audiences in a single string.
		for _, p := range strings.Fields(v) {
			if p == want {
				return true
			}
		}
		return v == want
	case []any:
		for _, e := range v {
			if s, ok := e.(string); ok && s == want {
				return true
			}
		}
	}
	return false
}

// jwtExpiry extracts the exp claim as a time.Time, regardless of whether it
// came back as float64 (the JSON default for numeric claims) or json.Number.
func jwtExpiry(claims jwt.MapClaims) (time.Time, bool) {
	switch v := claims["exp"].(type) {
	case float64:
		return time.Unix(int64(v), 0), true
	case int64:
		return time.Unix(v, 0), true
	case json.Number:
		if n, err := v.Int64(); err == nil {
			return time.Unix(n, 0), true
		}
	}
	return time.Time{}, false
}

// jsonRequestWithBearer is a small helper that builds a JSON-bodied request
// with an Authorization: Bearer header. Distinct from httpJSON in
// clientutil_test.go because that helper does not expose request headers.
func jsonRequestWithBearer(ctx context.Context, method, urlStr, token string, body any) (*http.Request, error) {
	buf, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, method, urlStr, strings.NewReader(string(buf)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	return req, nil
}
