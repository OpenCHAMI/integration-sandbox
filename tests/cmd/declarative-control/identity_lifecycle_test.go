// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/openchami/integration-sandbox/tests/internal/quiescence"
)

func identityHealthServer(t *testing.T) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
	t.Cleanup(s.Close)
	return s
}

func TestAuthenticatedRestartsMintFreshCredentials(t *testing.T) {
	server := identityHealthServer(t)
	c, fake := identityLifecycleFixture(t, strings.TrimPrefix(server.URL, "http://"))
	for range 2 {
		if err := c.lifecycle(context.Background(), "service-restart", "boot"); err != nil {
			t.Fatal(err)
		}
	}
	if fake.starts != 2 || fake.backend.issued != 2 || len(fake.consumed) != 3 {
		t.Fatal("two restarts did not consume distinct credentials")
	}
}

func TestIdentityFailureDoesNotStopWorkingBoot(t *testing.T) {
	for _, mode := range []string{"issuer", "ownership", "permissions"} {
		t.Run(mode, func(t *testing.T) {
			server := identityHealthServer(t)
			c, fake := identityLifecycleFixture(t, strings.TrimPrefix(server.URL, "http://"))
			switch mode {
			case "issuer":
				fake.backend.failure = true
			case "ownership":
				fake.backend.project = "unrelated"
			case "permissions":
				if err := os.Chmod(fake.configDir, 0777); err != nil {
					t.Fatal(err)
				}
			}
			if err := c.lifecycle(context.Background(), "service-restart", "boot"); err == nil {
				t.Fatal("unsafe restart succeeded")
			}
			if fake.starts != 0 || !fake.boot.State.Running {
				t.Fatal("boot changed before successful provisioning")
			}
		})
	}
}

func TestOrphanRecoveryMintsFreshIdentityAndPreservesMarker(t *testing.T) {
	server := identityHealthServer(t)
	c, fake := identityLifecycleFixture(t, strings.TrimPrefix(server.URL, "http://"))
	path, err := c.newLease(fake.boot)
	if err != nil {
		t.Fatal(err)
	}
	lease, err := c.readLease(path)
	if err != nil {
		t.Fatal(err)
	}
	lock := os.Getenv("SBX_DECLARATIVE_QUIESCENCE_LOCK")
	writer, err := quiescence.BeginWrite(context.Background(), lock, "http")
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.Abandon(); err != nil {
		t.Fatal(err)
	}
	fake.boot.State.Running = false
	if err := c.restoreOrphan(path, lease); err != nil {
		t.Fatal(err)
	}
	if fake.starts != 1 || fake.backend.issued != 1 {
		t.Fatal("recovery did not mint a fresh credential")
	}
	if capture, err := quiescence.AcquireCapture(context.Background(), lock); !errors.Is(err, quiescence.ErrInFlight) {
		if capture != nil {
			capture.Close()
		}
		t.Fatal("recovery cleared the writer marker")
	}
}
