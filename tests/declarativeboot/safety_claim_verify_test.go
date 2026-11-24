//go:build integration && declarative_boot

// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package declarativeboot

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rsa"
	"encoding/base64"
	"errors"
	"math/big"
	"slices"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type safetyFixtureKey struct {
	Type      string `json:"kty"`
	ID        string `json:"kid"`
	Algorithm string `json:"alg"`
	Use       string `json:"use"`
	N         string `json:"n"`
	E         string `json:"e"`
	Curve     string `json:"crv"`
	X         string `json:"x"`
	Y         string `json:"y"`
}
type safetyFixtureKeys struct {
	Keys []safetyFixtureKey `json:"keys"`
}
type safetyClaimPolicy struct {
	Issuer string
	Now    time.Time
	Scopes []string
}

func safetyFixtureKeyFor(set safetyFixtureKeys, token *jwt.Token) (any, error) {
	kid, ok := token.Header["kid"].(string)
	if !ok || kid == "" {
		return nil, ErrToken
	}
	var matched *safetyFixtureKey
	for i := range set.Keys {
		key := &set.Keys[i]
		if key.ID != kid {
			continue
		}
		if matched != nil {
			return nil, ErrToken
		}
		matched = key
	}
	if matched == nil || (matched.Use != "" && matched.Use != "sig") || (matched.Algorithm != "" && matched.Algorithm != token.Method.Alg()) {
		return nil, ErrToken
	}
	decode := base64.RawURLEncoding.DecodeString
	switch matched.Type {
	case "RSA":
		n, nErr := decode(matched.N)
		e, eErr := decode(matched.E)
		exponent := new(big.Int).SetBytes(e)
		if nErr != nil || eErr != nil || len(n) < 256 || !exponent.IsInt64() || exponent.Int64() < 3 || exponent.Int64() > 2147483647 || exponent.Int64()%2 == 0 {
			return nil, ErrToken
		}
		return &rsa.PublicKey{N: new(big.Int).SetBytes(n), E: int(exponent.Int64())}, nil
	case "EC":
		x, xErr := decode(matched.X)
		y, yErr := decode(matched.Y)
		if matched.Curve != "P-256" || xErr != nil || yErr != nil || len(x) != 32 || len(y) != 32 {
			return nil, ErrToken
		}
		pub := &ecdsa.PublicKey{Curve: elliptic.P256(), X: new(big.Int).SetBytes(x), Y: new(big.Int).SetBytes(y)}
		if !pub.Curve.IsOnCurve(pub.X, pub.Y) {
			return nil, ErrToken
		}
		return pub, nil
	default:
		return nil, ErrToken
	}
}

func safetyInspectFixture(raw, kind string, set safetyFixtureKeys, policy safetyClaimPolicy) error {
	claims := &tokenClaims{}
	parsed, err := jwt.ParseWithClaims(raw, claims, func(token *jwt.Token) (any, error) {
		return safetyFixtureKeyFor(set, token)
	}, jwt.WithValidMethods([]string{"RS256", "RS384", "RS512", "PS256", "PS384", "PS512", "ES256"}), jwt.WithoutClaimsValidation())
	if parsed == nil {
		return ErrToken
	}
	if kind == "bad-signature" {
		if !errors.Is(err, jwt.ErrTokenSignatureInvalid) {
			return ErrToken
		}
	} else if err != nil || !parsed.Valid {
		return ErrToken
	}
	allowed := slices.Contains([]string{"RS256", "RS384", "RS512", "PS256", "PS384", "PS512"}, parsed.Method.Alg())
	if (kind == "wrong-algorithm") == allowed {
		return ErrToken
	}
	if claims.Issuer == "" || (claims.Issuer != policy.Issuer) != (kind == "wrong-issuer") {
		return ErrToken
	}
	if len(claims.Audience) == 0 || (slices.Contains(claims.Audience, "boot-service")) == (kind == "wrong-audience") {
		return ErrToken
	}
	if claims.ExpiresAt == nil || (!claims.ExpiresAt.Time.After(policy.Now)) != (kind == "expired") {
		return ErrToken
	}
	if claims.NotBefore != nil && claims.NotBefore.Time.After(policy.Now) {
		return ErrToken
	}
	for _, scope := range policy.Scopes {
		if !slices.Contains(claims.Scope, scope) {
			return ErrToken
		}
	}
	switch kind {
	case "wrong-issuer", "wrong-audience", "expired", "wrong-algorithm", "bad-signature":
		return nil
	default:
		return ErrToken
	}
}
