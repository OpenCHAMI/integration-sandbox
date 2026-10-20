//go:build integration

// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package integration

import (
	"context"
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

func TestUC8Auth_ServiceTokenEnforcement(t *testing.T) {
	if os.Getenv("UC8_AUTH_ENABLED") != "1" {
		t.Skip("run make uc8-auth in its isolated stack")
	}
	project := os.Getenv("UC8_AUTH_PROJECT")
	require.NotEmpty(t, project, "authentication stack ownership is required")
	r := uc8ContractRunner(t)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	r.ctx = ctx
	service := envOr("UC8_AUTH_SERVICE", "boot")
	audience := "boot-service"
	uri := Endpoints["boot"] + "/bootconfigurations"
	cliArgs := []string{"boot", "--uri", uc8FixtureURL(Endpoints["boot"]), "--api-version", "v1", "config", "list", "-F", "json"}
	if service == "bss" {
		audience = "bss"
		base := envOr("SBX_BSS_URL", "http://127.0.0.1:27778/boot/v1")
		uri = base + "/bootparameters"
		cliArgs = []string{"bss", "--uri", uc8FixtureURL(base), "boot", "params", "get", "-F", "json"}
	} else {
		require.Equal(t, "boot", service, "unsupported authentication deployment")
	}
	mint := func(ttl string) string {
		ctx, cancel := context.WithTimeout(r.ctx, 10*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, "docker", "compose", "-p", project, "-f", "../../compose/uc8-auth.yaml", "exec", "-T", "tokensmith", "tokensmith", "user-token", "create", "--subject", "uc8-auth", "--scopes", "admin,audit", "--enable-local-user-mint", "--key-file", "/tokensmith/keys/private.pem", "--issuer", "http://tokensmith:27780", "--audience", audience, "--ttl", ttl)
		output, err := cmd.Output()
		require.NoError(t, err, "mint test token")
		return strings.TrimSpace(string(output))
	}
	valid := mint("10m")
	keyCommand := exec.CommandContext(r.ctx, "docker", "compose", "-p", project, "-f", "../../compose/uc8-auth.yaml", "exec", "-T", "tokensmith", "cat", "/tokensmith/keys/private.pem")
	keyPEM, err := keyCommand.Output()
	require.NoError(t, err, "read ephemeral test signing key")
	privateKey, err := jwt.ParseRSAPrivateKeyFromPEM(keyPEM)
	require.NoError(t, err)
	jwksCommand := exec.CommandContext(r.ctx, "docker", "compose", "-p", project, "-f", "../../compose/uc8-auth.yaml", "exec", "-T", "tokensmith", "curl", "-fsS", "--max-time", "5", "http://localhost:27780/.well-known/jwks.json")
	jwks, err := jwksCommand.Output()
	require.NoError(t, err, "fetch issuer public JWKS")
	keyfunc, err := uc8JWKSKeyfunc(jwks)
	require.NoError(t, err)
	expired, invalid, err := uc8AuthFixtures(valid, privateKey, keyfunc, time.Now())
	require.NoError(t, err, "fixture validation must pass before service assertions")
	type authCase struct {
		name, token string
		accepted    bool
	}
	cases := []authCase{{"valid_signature", valid, true}, {"missing_token", "", false}, {"wrong_signature", invalid, false}, {"expired_token", expired, false}}
	if service == "bss" {
		parsed, err := uc8VerifyPS256(valid, keyfunc)
		require.NoError(t, err)
		for _, name := range []string{"future_nbf", "missing_sub", "missing_iss", "missing_aud"} {
			claims := jwt.MapClaims{}
			for key, value := range parsed.Claims.(jwt.MapClaims) {
				claims[key] = value
			}
			if name == "future_nbf" {
				claims["nbf"] = time.Now().Add(10 * time.Minute).Unix()
			} else {
				delete(claims, strings.TrimPrefix(name, "missing_"))
			}
			fixture := jwt.NewWithClaims(jwt.SigningMethodPS256, claims)
			fixture.Header["kid"] = parsed.Header["kid"]
			raw, err := fixture.SignedString(privateKey)
			require.NoError(t, err)
			_, err = uc8VerifyPS256(raw, keyfunc, jwt.WithoutClaimsValidation())
			require.NoError(t, err, "policy fixtures require valid issuer signatures")
			cases = append(cases, authCase{name, raw, false})
		}
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			req, err := http.NewRequestWithContext(r.ctx, http.MethodGet, uri, nil)
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
			args, err := r.envelopeArgs(cliArgs)
			require.NoError(t, err)
			result := r.execute(CLIInput{IgnoreConfig: true, GlobalArgs: globals}, args...)
			if test.accepted {
				require.NoError(t, result.Err, result.Stderr)
				require.True(t, strings.HasPrefix(strings.TrimSpace(result.Stdout), "["), "expected collection response")
			} else {
				requireUC8CLIFailure(t, r, result)
			}
		})
	}
}
