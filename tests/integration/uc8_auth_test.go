//go:build integration

// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package integration

import (
	"crypto/rand"
	"crypto/rsa"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"
)

func TestUC8Auth_BootTokenEnforcement(t *testing.T) {
	if os.Getenv("UC8_AUTH_ENABLED") != "1" {
		t.Skip("run make uc8-auth in its isolated stack")
	}
	project := os.Getenv("UC8_AUTH_PROJECT")
	require.NotEmpty(t, project, "authentication stack ownership is required")
	r := uc8ContractRunner(t)
	mint := func(ttl string) string {
		cmd := exec.CommandContext(r.ctx, "docker", "compose", "-p", project, "-f", "../../compose/uc8-auth.yaml", "exec", "-T", "tokensmith", "tokensmith", "user-token", "create", "--subject", "uc8-auth", "--scopes", "admin,audit", "--enable-local-user-mint", "--key-file", "/tokensmith/keys/private.pem", "--issuer", "http://tokensmith:27780", "--audience", "boot-service", "--ttl", ttl)
		output, err := cmd.Output()
		require.NoError(t, err, "mint test token")
		return strings.TrimSpace(string(output))
	}
	valid := mint("10m")
	parsed, _, err := jwt.NewParser().ParseUnverified(valid, jwt.MapClaims{})
	require.NoError(t, err)
	claims := parsed.Claims.(jwt.MapClaims)
	keyCommand := exec.CommandContext(r.ctx, "docker", "compose", "-p", project, "-f", "../../compose/uc8-auth.yaml", "exec", "-T", "tokensmith", "cat", "/tokensmith/keys/private.pem")
	keyPEM, err := keyCommand.Output()
	require.NoError(t, err, "read ephemeral test signing key")
	privateKey, err := jwt.ParseRSAPrivateKeyFromPEM(keyPEM)
	require.NoError(t, err)
	// Exceed the validator's two-minute clock skew without waiting in CI.
	expiredClaims := jwt.MapClaims{}
	for name, value := range claims {
		expiredClaims[name] = value
	}
	expiredClaims["exp"] = time.Now().Add(-5 * time.Minute).Unix()
	expiredJWT := jwt.NewWithClaims(jwt.SigningMethodRS256, expiredClaims)
	expiredJWT.Header = parsed.Header
	expired, err := expiredJWT.SignedString(privateKey)
	require.NoError(t, err)
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	invalidJWT := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	invalidJWT.Header = parsed.Header
	invalid, err := invalidJWT.SignedString(key)
	require.NoError(t, err)
	verify := func(token string) error {
		_, err := jwt.Parse(token, func(*jwt.Token) (any, error) { return &privateKey.PublicKey, nil }, jwt.WithValidMethods([]string{"RS256"}), jwt.WithLeeway(2*time.Minute))
		return err
	}
	require.NoError(t, verify(valid))
	require.ErrorIs(t, verify(expired), jwt.ErrTokenExpired)
	require.ErrorIs(t, verify(invalid), jwt.ErrTokenSignatureInvalid)
	for _, test := range []struct {
		name, token string
		accepted    bool
	}{{"valid_signature", valid, true}, {"missing_token", "", false}, {"wrong_signature", invalid, false}, {"expired_token", expired, false}} {
		t.Run(test.name, func(t *testing.T) {
			req, err := http.NewRequestWithContext(r.ctx, http.MethodGet, Endpoints["boot"]+"/bootconfigurations", nil)
			require.NoError(t, err)
			if test.token != "" {
				req.Header.Set("Authorization", "Bearer "+test.token)
			}
			resp, err := httpClient.Do(req)
			require.NoError(t, err)
			defer resp.Body.Close()
			_, err = io.Copy(io.Discard, resp.Body)
			require.NoError(t, err)
			if test.accepted {
				require.Equal(t, http.StatusOK, resp.StatusCode)
			} else {
				require.Contains(t, []int{http.StatusUnauthorized, http.StatusForbidden}, resp.StatusCode, "service must enforce authentication independently of CLI validation")
			}
			globals := []string{"--no-token"}
			if test.token != "" {
				globals = []string{"--token", test.token}
			}
			args, err := r.envelopeArgs([]string{"boot", "--uri", uc8FixtureURL(Endpoints["boot"]), "--api-version", "v1", "config", "list", "-F", "json"})
			require.NoError(t, err)
			result := r.execute(CLIInput{IgnoreConfig: true, GlobalArgs: globals}, args...)
			if test.accepted {
				require.NoError(t, result.Err, result.Stderr)
				require.JSONEq(t, `[]`, result.Stdout)
			} else {
				requireUC8CLIFailure(t, r, result)
			}
		})
	}
}
