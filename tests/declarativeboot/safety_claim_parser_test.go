//go:build integration && declarative_boot

// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package declarativeboot

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"math/big"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestSafetyClaimFixtureInspection(t *testing.T) {
	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	other, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	ec, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	encode := base64.RawURLEncoding.EncodeToString
	keys := safetyFixtureKeys{Keys: []safetyFixtureKey{
		{Type: "RSA", ID: "rsa", N: encode(rsaKey.N.Bytes()), E: encode(big.NewInt(int64(rsaKey.E)).Bytes()), Algorithm: "RS256"},
		{Type: "EC", ID: "ec", Curve: "P-256", X: encode(ec.X.FillBytes(make([]byte, 32))), Y: encode(ec.Y.FillBytes(make([]byte, 32))), Algorithm: "ES256"},
	}}
	now := time.Unix(1700000000, 0)
	policy := safetyClaimPolicy{Issuer: "issuer", Now: now, Scopes: []string{"read:boot", "propose:boot", "promote:boot"}}
	for _, tc := range []struct {
		kind  string
		valid bool
		lie   string
	}{
		{"wrong-issuer", true, ""}, {"wrong-audience", true, ""}, {"expired", true, ""},
		{"wrong-algorithm", true, ""}, {"bad-signature", true, ""},
		{"wrong-issuer", false, "wrong-audience"}, {"wrong-algorithm", false, "bad-signature"},
		{"bad-signature", false, "wrong-issuer"},
	} {
		t.Run(tc.kind+"-"+tc.lie, func(t *testing.T) {
			kind := tc.kind
			if tc.lie != "" {
				kind = tc.lie
			}
			claims := tokenClaims{Scope: scopes(policy.Scopes), RegisteredClaims: jwt.RegisteredClaims{Issuer: policy.Issuer, Audience: jwt.ClaimStrings{"boot-service"}, ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour))}}
			if kind == "wrong-issuer" {
				claims.Issuer = "other"
			}
			if kind == "wrong-audience" {
				claims.Audience = jwt.ClaimStrings{"other"}
			}
			if kind == "expired" {
				claims.ExpiresAt = jwt.NewNumericDate(now.Add(-time.Minute))
			}
			var method jwt.SigningMethod = jwt.SigningMethodRS256
			key, kid := any(rsaKey), "rsa"
			if kind == "wrong-algorithm" {
				method, key, kid = jwt.SigningMethodES256, ec, "ec"
			}
			if kind == "bad-signature" {
				key = other
			}
			token := jwt.NewWithClaims(method, claims)
			token.Header["kid"] = kid
			raw, err := token.SignedString(key)
			if err != nil {
				t.Fatal(err)
			}
			if got := safetyInspectFixture(raw, tc.kind, keys, policy) == nil; got != tc.valid {
				t.Fatalf("fixture inspection valid = %v; want %v", got, tc.valid)
			}
		})
	}
	untrustedEC, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	claims := tokenClaims{Scope: scopes(policy.Scopes), RegisteredClaims: jwt.RegisteredClaims{Issuer: policy.Issuer, Audience: jwt.ClaimStrings{"boot-service"}, ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour))}}
	wrongAlgorithm := jwt.NewWithClaims(jwt.SigningMethodES256, claims)
	wrongAlgorithm.Header["kid"] = "ec"
	raw, err := wrongAlgorithm.SignedString(untrustedEC)
	if err != nil {
		t.Fatal(err)
	}
	if safetyInspectFixture(raw, "wrong-algorithm", keys, policy) == nil {
		t.Fatal("unsupported algorithm fixture with invalid signature was accepted")
	}
}
