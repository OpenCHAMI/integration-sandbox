//go:build integration && declarative_boot

// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package declarativeboot

import (
	"context"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v5"
)

type TokenPolicy struct {
	Issuer   string
	Audience string
	Scopes   []string
}
type jwks struct {
	Keys []jwk `json:"keys"`
}
type jwk struct {
	Type      string `json:"kty"`
	ID        string `json:"kid"`
	N         string `json:"n"`
	E         string `json:"e"`
	Use       string `json:"use"`
	Algorithm string `json:"alg"`
}
type scopes []string

func (s *scopes) UnmarshalJSON(data []byte) error {
	var list []string
	if err := json.Unmarshal(data, &list); err == nil {
		*s = list
		return nil
	}
	var text string
	if err := json.Unmarshal(data, &text); err != nil {
		return err
	}
	*s = strings.Fields(text)
	return nil
}

type tokenClaims struct {
	Scope scopes `json:"scope"`
	jwt.RegisteredClaims
}

func VerifyToken(ctx context.Context, client *HTTPClient, raw string, policy TokenPolicy) error {
	if raw == "" || policy.Issuer == "" || policy.Audience == "" {
		return fmt.Errorf("token verification configuration: %w", ErrToken)
	}
	response, err := client.Do(ctx, Request{Method: http.MethodGet, Path: "/.well-known/jwks.json"})
	if err != nil {
		return err
	}
	if err := response.RequireStatus(http.StatusOK); err != nil {
		return err
	}
	set, err := DecodeJSON[jwks](response)
	if err != nil {
		return err
	}
	claims := &tokenClaims{}
	parsed, err := jwt.ParseWithClaims(raw, claims, func(token *jwt.Token) (interface{}, error) {
		kid, ok := token.Header["kid"].(string)
		if !ok || kid == "" {
			return nil, ErrToken
		}
		var found *rsa.PublicKey
		for _, key := range set.Keys {
			if key.ID != kid || key.Type != "RSA" || (key.Use != "" && key.Use != "sig") || (key.Algorithm != "" && key.Algorithm != token.Method.Alg()) {
				continue
			}
			n, nErr := base64.RawURLEncoding.DecodeString(key.N)
			e, eErr := base64.RawURLEncoding.DecodeString(key.E)
			exponent := new(big.Int).SetBytes(e)
			if nErr != nil || eErr != nil || len(n) < 256 || !exponent.IsInt64() || exponent.Int64() < 3 || exponent.Int64() > 2147483647 || exponent.Int64()%2 == 0 || found != nil {
				return nil, ErrToken
			}
			found = &rsa.PublicKey{N: new(big.Int).SetBytes(n), E: int(exponent.Int64())}
		}
		if found == nil {
			return nil, ErrToken
		}
		return found, nil
	}, jwt.WithValidMethods([]string{"RS256", "RS384", "RS512", "PS256", "PS384", "PS512"}), jwt.WithIssuer(policy.Issuer), jwt.WithAudience(policy.Audience), jwt.WithExpirationRequired())
	if err != nil || parsed == nil || !parsed.Valid {
		return fmt.Errorf("verify token: %w", ErrToken)
	}
	for _, wanted := range policy.Scopes {
		found := false
		for _, scope := range claims.Scope {
			if scope == wanted {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("verify token scopes: %w", ErrToken)
		}
	}
	return nil
}
