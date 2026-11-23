//go:build integration && declarative_boot

// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package declarativeboot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os/exec"
	"strings"
	"time"
)

var ErrToken = errors.New("token operation failed")

type CommandRunner func(context.Context, ...string) ([]byte, error)
type BootstrapOptions struct {
	Container string
	Store     string
	Subject   string
	Audience  string
	Scopes    []string
	TTL       string
}
type bootstrapResult struct {
	Token string `json:"bootstrap_token"`
}
type tokenResult struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
}

func DockerExec(ctx context.Context, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, "docker", args...).Output()
}

func CreateBootstrapToken(ctx context.Context, run CommandRunner, options BootstrapOptions) (string, error) {
	if run == nil || options.Container == "" || options.Store == "" || options.Subject == "" || options.Audience == "" || options.TTL == "" {
		return "", fmt.Errorf("bootstrap configuration: %w", ErrToken)
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	out, err := run(ctx, "exec", options.Container, "tokensmith", "bootstrap-token", "create", "--bootstrap-store="+options.Store, "--subject="+options.Subject, "--audience="+options.Audience, "--scopes="+strings.Join(options.Scopes, ","), "--ttl="+options.TTL, "--output-format=json")
	if err != nil {
		if ctx.Err() != nil {
			return "", fmt.Errorf("bootstrap: %w", ctx.Err())
		}
		return "", fmt.Errorf("bootstrap command: %w", ErrToken)
	}
	var result bootstrapResult
	if json.Unmarshal(out, &result) != nil || result.Token == "" {
		return "", fmt.Errorf("bootstrap output: %w", ErrToken)
	}
	return result.Token, nil
}

func ExchangeToken(ctx context.Context, client *HTTPClient, bootstrap string) (string, error) {
	if bootstrap == "" {
		return "", fmt.Errorf("empty bootstrap token: %w", ErrToken)
	}
	form := url.Values{"grant_type": {"urn:ietf:params:oauth:grant-type:token-exchange"}, "subject_token": {bootstrap}, "subject_token_type": {"urn:openchami:params:oauth:token-type:bootstrap-token"}}
	resp, err := client.Do(ctx, Request{Method: http.MethodPost, Path: "/oauth/token", Headers: http.Header{"Content-Type": {"application/x-www-form-urlencoded"}}, Body: []byte(form.Encode())})
	if err != nil {
		return "", err
	}
	if err := resp.RequireStatus(http.StatusOK); err != nil {
		return "", err
	}
	result, err := DecodeJSON[tokenResult](resp)
	if err != nil || result.AccessToken == "" || !strings.EqualFold(result.TokenType, "bearer") {
		return "", fmt.Errorf("token exchange response: %w", ErrToken)
	}
	return result.AccessToken, nil
}
