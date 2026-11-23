// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"time"
)

func loopbackURL(value string) (*url.URL, error) {
	u, err := url.Parse(value)
	if err != nil || u.Scheme != "http" || u.Hostname() != "127.0.0.1" || u.Port() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return nil, errInput
	}
	return u, nil
}

func tokenFixture(ctx context.Context, _ controller, raw json.RawMessage) (json.RawMessage, error) {
	req, err := decodeRequest[struct {
		Kind   string   `json:"kind"`
		Scopes []string `json:"scopes"`
	}](raw)
	if err != nil {
		return nil, err
	}
	switch req.Kind {
	case "wrong-issuer", "wrong-audience", "expired", "wrong-algorithm", "bad-signature":
	default:
		return nil, errInput
	}
	if len(req.Scopes) == 0 {
		return nil, errInput
	}
	for _, scope := range req.Scopes {
		if scope != "read:boot" && scope != "propose:boot" && scope != "promote:boot" {
			return nil, errInput
		}
	}
	u, err := loopbackURL(os.Getenv("SBX_DECLARATIVE_FIXTURE_URL"))
	if err != nil {
		return nil, err
	}
	u.Path = "/__fixtures/token"
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewReader(raw))
	if err != nil {
		return nil, errInput
	}
	request.Header.Set("Content-Type", "application/json")
	client := http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{Proxy: nil, DialContext: (&net.Dialer{Timeout: 5 * time.Second}).DialContext}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(request)
	if err != nil {
		return nil, errInput
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, 16385))
	if err != nil || len(data) > 16384 || response.StatusCode != http.StatusOK {
		return nil, errInput
	}
	value, err := decodeRequest[struct {
		Token string `json:"token"`
	}](bytes.TrimSpace(data))
	if err != nil || value.Token == "" {
		return nil, errInput
	}
	return json.Marshal(value)
}
