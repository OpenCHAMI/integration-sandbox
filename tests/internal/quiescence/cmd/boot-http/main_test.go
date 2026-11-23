//go:build integration && declarative_boot

// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/openchami/integration-sandbox/tests/internal/quiescence"
)

func TestBootHTTPSeedUsesSharedLease(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lock")
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
		defer cancel()
		if capture, err := quiescence.AcquireCapture(ctx, path); capture != nil || !errors.Is(err, context.DeadlineExceeded) {
			t.Error("capture entered during seed")
		}
		if r.Method != "POST" || r.URL.Path != "/bootconfigurations" || r.Header.Get("Content-Type") != "application/json" {
			t.Error("wrong seed request")
		}
		w.WriteHeader(201)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()
	t.Setenv("SBX_DECLARATIVE_AUTH_OFF_URL", server.URL)
	t.Setenv("SBX_DECLARATIVE_QUIESCENCE_LOCK", path)
	var output bytes.Buffer
	err := run(context.Background(), []string{"-method=POST", "-status", server.URL + "/bootconfigurations"}, streams{input: strings.NewReader(`{"spec":{}}`), output: &output})
	if err != nil || output.String() != "{\"ok\":true}\n__SBX_HTTP_STATUS__201\n" {
		t.Fatalf("unexpected output %q: %v", output.String(), err)
	}
	capture, err := quiescence.AcquireCapture(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if err := capture.Close(); err != nil {
		t.Fatal(err)
	}
}
