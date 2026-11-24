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
	"strings"
	"testing"
	"time"
)

type lifecycleCase struct {
	t        *testing.T
	ctx      context.Context
	boot     BootClient
	smd      SMDClient
	fixtures Fixtures
	series   string
}

func lifecycleNew(t *testing.T) *lifecycleCase {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	required := func(key string) string {
		value := os.Getenv(key)
		if value == "" {
			t.Fatalf("required environment variable %s is missing", key)
		}
		return value
	}
	client := func(key string) *HTTPClient {
		c, err := NewHTTPClient(required(key), 10*time.Second)
		if err != nil {
			t.Fatalf("invalid %s: %v", key, err)
		}
		return c
	}
	bootHTTP := client("SBX_DECLARATIVE_BOOT_URL")
	smdHTTP := client("SBX_DECLARATIVE_SMD_URL")
	tokenHTTP := client("SBX_DECLARATIVE_TOKENSMITH_URL")
	container := required("SBX_DECLARATIVE_TOKENSMITH_CONTAINER")
	store := required("SBX_DECLARATIVE_TOKENSMITH_STORE")
	var id [8]byte
	if _, err := rand.Read(id[:]); err != nil {
		t.Fatalf("fixture identity: %v", err)
	}
	name := "lifecycle-" + hex.EncodeToString(id[:])
	scopes := []string{"read:boot", "propose:boot", "promote:boot"}
	bootstrap, err := CreateBootstrapToken(ctx, DockerExec, BootstrapOptions{Container: container, Store: store, Subject: name, Audience: "boot-service", Scopes: scopes, TTL: "5m"})
	if err != nil {
		t.Fatalf("bootstrap token: %v", err)
	}
	token, err := ExchangeToken(ctx, tokenHTTP, bootstrap)
	if err != nil {
		t.Fatalf("token exchange: %v", err)
	}
	issuer := os.Getenv("SBX_DECLARATIVE_ISSUER")
	if issuer == "" {
		issuer = "http://tokensmith:27780"
	}
	if err := VerifyToken(ctx, tokenHTTP, token, TokenPolicy{Issuer: issuer, Audience: "boot-service", Scopes: scopes}); err != nil {
		t.Fatalf("token verification: %v", err)
	}
	fixtures, err := LoadFixtures("../../fixtures/declarative-boot")
	if err != nil {
		t.Fatalf("load fixture: %v", err)
	}
	return &lifecycleCase{t: t, ctx: ctx, boot: BootClient{HTTP: bootHTTP, Bearer: token}, smd: SMDClient{HTTP: smdHTTP}, fixtures: fixtures, series: name}
}

func (c *lifecycleCase) status(response Response, err error, expected ...int) Response {
	c.t.Helper()
	if err != nil {
		c.t.Fatalf("HTTP request: %v", err)
	}
	if err := response.RequireStatus(expected...); err != nil {
		c.t.Fatal(err)
	}
	return response
}

func (c *lifecycleCase) config(key string) NamedConfig {
	rev := c.fixtures.Configs.Revisions[key]
	return NamedConfig{RevisionName: rev.RevisionName, Resource: BootConfig{Metadata: Metadata{Name: c.series}, Spec: rev.Spec}}
}

func (c *lifecycleCase) revision(response Response) RevisionRecord {
	c.t.Helper()
	record, err := DecodeRevision(response)
	if err != nil {
		c.t.Fatalf("decode revision: %v", err)
	}
	if record.Metadata.SeriesName != c.series {
		c.t.Fatal("revision belongs to another series")
	}
	return record
}

func (c *lifecycleCase) seriesState() (SeriesStatus, string) {
	c.t.Helper()
	response, err := c.boot.GetSeries(c.ctx, c.series)
	response = c.status(response, err, http.StatusOK)
	state, err := DecodeSeries(response)
	if err != nil {
		c.t.Fatalf("decode series: %v", err)
	}
	etag := response.Header.Get("ETag")
	if !strings.HasPrefix(etag, `"`) || !strings.HasSuffix(etag, `"`) || len(etag) < 3 {
		c.t.Fatal("series has no strong ETag")
	}
	return state, etag
}

func (c *lifecycleCase) pointers(defaultUID, latestUID string, count uint64) {
	c.t.Helper()
	state, _ := c.seriesState()
	if state.DefaultRevisionUID != defaultUID || state.LatestRevisionUID != latestUID || state.RevisionCount != count {
		c.t.Fatalf("unexpected series state: %+v", state)
	}
}
