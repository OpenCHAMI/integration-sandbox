//go:build integration && declarative_boot

// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package declarativeboot

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var ErrHTTP = errors.New("http request failed")
var ErrJSON = errors.New("invalid JSON response")

type HTTPClient struct {
	base   *url.URL
	client *http.Client
}
type Request struct {
	Method  string
	Path    string
	Headers http.Header
	Body    []byte
}
type Response struct {
	Status int
	Header http.Header
	Body   []byte
}
type StatusError struct {
	Status   int
	Expected []int
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("unexpected HTTP status %d (expected %v)", e.Status, e.Expected)
}

func NewHTTPClient(base string, timeout time.Duration) (*HTTPClient, error) {
	u, err := url.Parse(base)
	if err != nil || u == nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || timeout <= 0 {
		return nil, fmt.Errorf("invalid HTTP client configuration: %w", ErrHTTP)
	}
	transport := &http.Transport{Proxy: http.ProxyFromEnvironment, DialContext: (&net.Dialer{Timeout: timeout, KeepAlive: 30 * time.Second}).DialContext, TLSHandshakeTimeout: timeout, ResponseHeaderTimeout: timeout, IdleConnTimeout: 30 * time.Second, MaxIdleConns: 32, MaxIdleConnsPerHost: 8}
	return &HTTPClient{base: u, client: &http.Client{Transport: transport, Timeout: timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}

func (c *HTTPClient) Do(ctx context.Context, input Request) (Response, error) {
	rel, err := url.Parse(input.Path)
	if err != nil || rel == nil || rel.IsAbs() || rel.Host != "" || !strings.HasPrefix(rel.Path, "/") || rel.Fragment != "" {
		return Response{}, fmt.Errorf("invalid request path: %w", ErrHTTP)
	}
	target := c.base.ResolveReference(rel)
	req, err := http.NewRequestWithContext(ctx, input.Method, target.String(), bytes.NewReader(input.Body))
	if err != nil {
		return Response{}, fmt.Errorf("construct request: %w", ErrHTTP)
	}
	req.Header = input.Headers.Clone()
	if req.Header == nil {
		req.Header = make(http.Header)
	}
	if !validRequestHeaders(req.Header) {
		return Response{}, fmt.Errorf("invalid request headers: %w", ErrHTTP)
	}
	lease, err := beginHTTPLease(ctx, target)
	if err != nil {
		return Response{}, fmt.Errorf("acquire request lease: %w", err)
	}
	if ctx.Err() != nil {
		return Response{}, errors.Join(ctx.Err(), finishHTTPLease(lease, true))
	}
	resp, err := c.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return Response{}, errors.Join(fmt.Errorf("request canceled: %w", ctx.Err()), finishHTTPLease(lease, false))
		}
		return Response{}, errors.Join(fmt.Errorf("request transport: %w", ErrHTTP), finishHTTPLease(lease, false))
	}
	const maxBody = 4 << 20
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	closeErr := resp.Body.Close()
	if err != nil || closeErr != nil || len(body) > maxBody || ctx.Err() != nil {
		return Response{}, errors.Join(fmt.Errorf("read response: %w", ErrHTTP), finishHTTPLease(lease, false))
	}
	if err := finishHTTPLease(lease, true); err != nil {
		return Response{}, fmt.Errorf("complete request lease: %w", err)
	}
	return Response{Status: resp.StatusCode, Header: resp.Header.Clone(), Body: body}, nil
}

func (r Response) RequireStatus(expected ...int) error {
	for _, status := range expected {
		if r.Status == status {
			return nil
		}
	}
	return &StatusError{Status: r.Status, Expected: append([]int(nil), expected...)}
}

func DecodeJSON[T any](r Response) (T, error) {
	var value T
	if err := json.Unmarshal(r.Body, &value); err != nil {
		return value, fmt.Errorf("decode response: %w", ErrJSON)
	}
	return value, nil
}

func EncodeJSON[T any](value T) ([]byte, error) {
	body, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("encode request: %w", ErrJSON)
	}
	return body, nil
}
