//go:build integration

// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package integration

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"
)

const uc8AuthSkew = 2 * time.Minute

// Never infer the accepted signing algorithm from an untrusted token header.
func uc8VerifyPS256(raw string, keyfunc jwt.Keyfunc, options ...jwt.ParserOption) (*jwt.Token, error) {
	opts := []jwt.ParserOption{jwt.WithValidMethods([]string{"PS256"}), jwt.WithLeeway(uc8AuthSkew)}
	return jwt.Parse(raw, keyfunc, append(opts, options...)...)
}

func uc8JWKSKeyfunc(data []byte) (jwt.Keyfunc, error) {
	var document struct {
		Keys []struct{ Kty, Kid, Alg, Use, N, E string }
	}
	if err := json.Unmarshal(data, &document); err != nil {
		return nil, fmt.Errorf("decode public JWKS: %w", err)
	}
	keys := map[string]*rsa.PublicKey{}
	for _, key := range document.Keys {
		if key.Kty != "RSA" || key.Kid == "" || (key.Alg != "" && key.Alg != "PS256") || (key.Use != "" && key.Use != "sig") {
			continue
		}
		n, err := base64.RawURLEncoding.DecodeString(key.N)
		if err != nil {
			return nil, errors.New("invalid JWKS modulus")
		}
		e, err := base64.RawURLEncoding.DecodeString(key.E)
		if err != nil || len(e) == 0 || len(e) > 4 {
			return nil, errors.New("invalid JWKS exponent")
		}
		exponent := new(big.Int).SetBytes(e).Int64()
		if exponent < 3 || exponent%2 == 0 || len(n) < 256 {
			return nil, errors.New("invalid RSA public key")
		}
		if _, exists := keys[key.Kid]; exists {
			return nil, errors.New("duplicate JWKS key ID")
		}
		keys[key.Kid] = &rsa.PublicKey{N: new(big.Int).SetBytes(n), E: int(exponent)}
	}
	if len(keys) == 0 {
		return nil, errors.New("no PS256 signing keys in JWKS")
	}
	return func(token *jwt.Token) (any, error) {
		kid, _ := token.Header["kid"].(string)
		key := keys[kid]
		if key == nil {
			return nil, errors.New("token key ID absent from issuer JWKS")
		}
		return key, nil
	}, nil
}

func uc8AuthFixtures(valid string, issuer *rsa.PrivateKey, keyfunc jwt.Keyfunc, now time.Time) (string, string, error) {
	parsed, err := uc8VerifyPS256(valid, keyfunc)
	if err != nil {
		return "", "", fmt.Errorf("real token failed issuer JWKS verification: %w", err)
	}
	kid, ok := parsed.Header["kid"].(string)
	if !ok || kid == "" {
		return "", "", errors.New("issuer token has no key ID")
	}
	claims := parsed.Claims.(jwt.MapClaims)
	expiredClaims := jwt.MapClaims{}
	for name, value := range claims {
		expiredClaims[name] = value
	}
	expiredClaims["exp"] = now.Add(-5 * time.Minute).Unix()
	sign := func(claims jwt.MapClaims, key *rsa.PrivateKey) (string, error) {
		token := jwt.NewWithClaims(jwt.SigningMethodPS256, claims)
		token.Header["kid"] = kid
		return token.SignedString(key)
	}
	expired, err := sign(expiredClaims, issuer)
	if err != nil {
		return "", "", err
	}
	if _, err = uc8VerifyPS256(expired, keyfunc, jwt.WithoutClaimsValidation()); err != nil {
		return "", "", fmt.Errorf("expired fixture has invalid issuer signature: %w", err)
	}
	if _, err = uc8VerifyPS256(expired, keyfunc); !errors.Is(err, jwt.ErrTokenExpired) {
		return "", "", errors.New("fixture must fail expiration validation")
	}
	wrong, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return "", "", err
	}
	invalid, err := sign(claims, wrong)
	if err != nil {
		return "", "", err
	}
	if _, err = uc8VerifyPS256(invalid, keyfunc); !errors.Is(err, jwt.ErrTokenSignatureInvalid) {
		return "", "", errors.New("wrong-key fixture must fail signature validation")
	}
	return expired, invalid, nil
}

func TestUC8_TokenFixtureRegression(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	jwks, err := json.Marshal(map[string]any{"keys": []any{map[string]string{
		"kty": "RSA", "kid": "issuer", "alg": "PS256", "use": "sig",
		"n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()), "e": "AQAB",
	}}})
	require.NoError(t, err)
	verify, err := uc8JWKSKeyfunc(jwks)
	require.NoError(t, err)
	claims := jwt.MapClaims{"sub": "fixture", "exp": time.Now().Add(time.Hour).Unix()}
	token := jwt.NewWithClaims(jwt.SigningMethodPS256, claims)
	token.Header["kid"] = "issuer"
	valid, err := token.SignedString(key)
	require.NoError(t, err)
	expired, invalid, err := uc8AuthFixtures(valid, key, verify, time.Now())
	require.NoError(t, err)
	_, err = uc8VerifyPS256(expired, verify, jwt.WithoutClaimsValidation())
	require.NoError(t, err)
	_, err = uc8VerifyPS256(invalid, verify)
	require.ErrorIs(t, err, jwt.ErrTokenSignatureInvalid)
	// Reproduce the old bug: RS256 signature with a copied PS256 header.
	mismatched := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	mismatched.Header = token.Header
	raw, err := mismatched.SignedString(key)
	require.NoError(t, err)
	_, _, err = uc8AuthFixtures(raw, key, verify, time.Now())
	require.ErrorContains(t, err, "real token failed issuer JWKS verification")
	token.Header["kid"] = "unknown"
	raw, err = token.SignedString(key)
	require.NoError(t, err)
	_, err = uc8VerifyPS256(raw, verify)
	require.Error(t, err)
}
