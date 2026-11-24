package main

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type faultProxy struct {
	gate      chan struct{}
	triggered bool
	upstream  http.Handler
}

func newFaultProxy(upstream *url.URL) http.Handler {
	p := &faultProxy{gate: make(chan struct{}, 1)}
	proxy := reverseProxy(upstream)
	proxy.ModifyResponse = func(response *http.Response) error {
		if safeMethod(response.Request.Method) || p.triggered || response.StatusCode < 200 || response.StatusCode >= 300 {
			return nil
		}
		// The upstream response must finish before hiding its successful result.
		_, err := io.Copy(io.Discard, response.Body)
		if err != nil {
			return err
		}
		if err := response.Body.Close(); err != nil {
			return err
		}
		p.triggered = true
		method := response.Request.Method
		switch method {
		case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		default:
			method = "OTHER"
		}
		slog.Info("fixture mutation interrupted", "method", method, "upstream_status", response.StatusCode)
		response.StatusCode = http.StatusBadGateway
		response.Status = "502 Bad Gateway"
		response.Header = make(http.Header)
		response.Header.Set("Content-Type", "text/plain; charset=utf-8")
		response.Body = io.NopCloser(strings.NewReader("injected upstream response failure\n"))
		response.ContentLength = int64(len("injected upstream response failure\n"))
		return nil
	}
	p.upstream = proxy
	return p
}

func (p *faultProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/__fixtures/health" {
		health(w, r)
		return
	}
	if !safeMethod(r.Method) {
		select {
		case p.gate <- struct{}{}:
			defer func() { <-p.gate }()
		case <-r.Context().Done():
			return
		}
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	p.upstream.ServeHTTP(w, r.WithContext(ctx))
}

func safeMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace:
		return true
	default:
		return false
	}
}
