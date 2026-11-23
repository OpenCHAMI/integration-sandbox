//go:build integration && declarative_boot

// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package declarativeboot

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/openchami/integration-sandbox/tests/internal/quiescence"
)

func sameOrigin(a, b *url.URL) bool {
	port := func(u *url.URL) string {
		if u.Port() != "" {
			return u.Port()
		}
		if strings.EqualFold(u.Scheme, "https") {
			return "443"
		}
		return "80"
	}
	return strings.EqualFold(a.Scheme, b.Scheme) && strings.EqualFold(a.Hostname(), b.Hostname()) && port(a) == port(b)
}

func validRequestHeaders(headers map[string][]string) bool {
	const punctuation = "!#$%&'*+-.^_`|~"
	for name, values := range headers {
		if name == "" {
			return false
		}
		for i := 0; i < len(name); i++ {
			c := name[i]
			if !(c >= 'a' && c <= 'z') && !(c >= 'A' && c <= 'Z') && !(c >= '0' && c <= '9') && !strings.ContainsRune(punctuation, rune(c)) {
				return false
			}
		}
		for _, value := range values {
			for i := 0; i < len(value); i++ {
				c := value[i]
				if (c < 32 && c != '\t') || c == 127 {
					return false
				}
			}
		}
	}
	return true
}

func beginHTTPLease(ctx context.Context, target *url.URL) (*quiescence.WriteLease, error) {
	for _, key := range []string{"SBX_DECLARATIVE_BOOT_URL", "SBX_DECLARATIVE_AUTH_OFF_URL"} {
		value := os.Getenv(key)
		if value == "" {
			continue
		}
		origin, err := url.Parse(value)
		if err != nil || origin.Host == "" || origin.User != nil || (origin.Scheme != "http" && origin.Scheme != "https") {
			return nil, fmt.Errorf("invalid configured boot origin: %w", ErrHTTP)
		}
		if sameOrigin(target, origin) {
			return quiescence.BeginWrite(ctx, os.Getenv("SBX_DECLARATIVE_QUIESCENCE_LOCK"), "http")
		}
	}
	return nil, nil
}

func finishHTTPLease(lease *quiescence.WriteLease, complete bool) error {
	if lease == nil {
		return nil
	}
	if complete {
		return lease.Complete()
	}
	return lease.Abandon()
}
