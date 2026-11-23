//go:build integration && declarative_boot

// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package declarativeboot

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"os"
	"testing"
	"time"
)

func safetyEnv(t *testing.T, name string) string {
	t.Helper()
	value := os.Getenv(name)
	if value == "" {
		t.Fatalf("required environment variable %s is missing", name)
	}
	return value
}

func safetyName(t *testing.T) string {
	t.Helper()
	var random [10]byte
	if _, err := rand.Read(random[:]); err != nil {
		t.Fatalf("generate independent resource name: %v", err)
	}
	return "safety-" + hex.EncodeToString(random[:])
}

func safetyClient(t *testing.T) BootClient {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	boot, err := NewHTTPClient(safetyEnv(t, "SBX_DECLARATIVE_BOOT_URL"), 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	ts, err := NewHTTPClient(safetyEnv(t, "SBX_DECLARATIVE_TOKENSMITH_URL"), 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	scopes := []string{"read:boot", "propose:boot", "promote:boot"}
	bootstrap, err := CreateBootstrapToken(ctx, DockerExec, BootstrapOptions{
		Container: safetyEnv(t, "SBX_DECLARATIVE_TOKENSMITH_CONTAINER"),
		Store:     safetyEnv(t, "SBX_DECLARATIVE_TOKENSMITH_STORE"), Subject: safetyName(t),
		Audience: "boot-service", Scopes: scopes, TTL: "5m",
	})
	if err != nil {
		t.Fatalf("mint bootstrap token: %v", err)
	}
	token, err := ExchangeToken(ctx, ts, bootstrap)
	if err != nil {
		t.Fatalf("exchange bootstrap token: %v", err)
	}
	issuer := os.Getenv("SBX_DECLARATIVE_ISSUER")
	if issuer == "" {
		issuer = "http://tokensmith:27780"
	}
	if err := VerifyToken(ctx, ts, token, TokenPolicy{Issuer: issuer, Audience: "boot-service", Scopes: scopes}); err != nil {
		t.Fatalf("verify exchanged token: %v", err)
	}
	return BootClient{HTTP: boot, Bearer: token}
}

func safetyStatus(t *testing.T, response Response, err error, want int) Response {
	t.Helper()
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if response.Status != want {
		t.Fatalf("HTTP status %d; want %d", response.Status, want)
	}
	return response
}

func safetyETag(t *testing.T, response Response) string {
	t.Helper()
	etag := response.Header.Get("ETag")
	if !safetyStrongETag(etag) {
		t.Fatal("response is missing a single strong ETag")
	}
	return etag
}

func safetySetup(t *testing.T) (context.Context, BootClient, BootAssignment) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	t.Cleanup(cancel)
	client := safetyClient(t)
	series := safetyName(t)
	response, err := client.CreateConfig(ctx, NamedConfig{RevisionName: "release-a", Resource: BootConfig{
		Metadata: Metadata{Name: series}, Spec: safetyArtifactA,
	}})
	safetyStatus(t, response, err, http.StatusCreated)
	if _, err := DecodeRevision(response); err != nil {
		t.Fatalf("initial revision: %v", err)
	}
	assignment := BootAssignment{Metadata: Metadata{Name: safetyName(t)}, Spec: AssignmentSpec{
		Selector: &Selector{Xnames: []string{"x999c0s0b0n0"}}, ConfigRef: ConfigReference{Name: series},
	}}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		current, err := client.GetAssignment(cleanupCtx, assignment.Metadata.Name)
		if err != nil {
			t.Errorf("cleanup read: %v", err)
			return
		}
		if current.Status == http.StatusNotFound {
			return
		}
		if current.Status != http.StatusOK || !safetyStrongETag(current.Header.Get("ETag")) {
			t.Errorf("cleanup read: HTTP %d or invalid ETag", current.Status)
			return
		}
		deleted, err := client.DeleteAssignment(cleanupCtx, ConditionalName{Name: assignment.Metadata.Name, IfMatch: current.Header.Get("ETag")})
		if err != nil || (deleted.Status != http.StatusOK && deleted.Status != http.StatusNoContent) {
			t.Errorf("cleanup delete: HTTP %d, error %v", deleted.Status, err)
		}
	})
	return ctx, client, assignment
}

func safetyReadAssignment(t *testing.T, ctx context.Context, client BootClient, name string) (BootAssignment, string, []byte) {
	t.Helper()
	response, err := client.GetAssignment(ctx, name)
	safetyStatus(t, response, err, http.StatusOK)
	value, err := DecodeAssignment(response)
	if err != nil {
		t.Fatalf("decode assignment: %v", err)
	}
	return value, safetyETag(t, response), response.Body
}
