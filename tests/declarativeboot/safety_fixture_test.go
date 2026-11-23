//go:build integration && declarative_boot

// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package declarativeboot

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

const safetyNode = "x0c0s3b0n0"

var safetyArtifactA = BootConfigSpec{Kernel: "https://images.example.invalid/boot-images/acceptance-a-kernel", Initrd: "https://images.example.invalid/boot-images/acceptance-a-initrd", Params: "console=ttyS0 root=live:s3://boot-images/acceptance-a-rootfs.squashfs release=A ip=dhcp"}
var safetyArtifactB = BootConfigSpec{Kernel: "https://images.example.invalid/boot-images/acceptance-b-kernel", Initrd: "https://images.example.invalid/boot-images/acceptance-b-initrd", Params: "console=ttyS1 root=live:s3://boot-images/acceptance-b-rootfs.squashfs release=B ip=static"}

func safetySMDIdentity(body []byte) bool {
	var response struct {
		ID         string      `json:"ID"`
		Components []Component `json:"Components"`
	}
	if json.Unmarshal(body, &response) != nil {
		return false
	}
	if response.ID == safetyNode {
		return true
	}
	return len(response.Components) == 1 && response.Components[0].ID == safetyNode
}

func safetyArtifactTuple(script string, want BootConfigSpec) bool {
	return strings.HasPrefix(script, "#!ipxe") && lifecycleArtifactMatches(script, Artifact{Kernel: want.Kernel, Initrd: want.Initrd, Params: strings.Fields(want.Params)})
}

type safetyFixture struct {
	t          *testing.T
	ctx        context.Context
	client     BootClient
	assignment BootAssignment
}

type safetySnapshot struct {
	assignment          []byte
	assignETag          string
	series              []byte
	seriesETag          string
	collection          []byte
	configurationDigest string
}

func safetyFixtureSeeded(t *testing.T) *safetyFixture {
	t.Helper()
	ctx, client, assignment := safetySetup(t)
	smd, err := NewHTTPClient(safetyEnv(t, "SBX_DECLARATIVE_SMD_URL"), 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	response, err := smd.Do(ctx, Request{Method: http.MethodGet, Path: "/hsm/v2/State/Components/" + safetyNode})
	safetyStatus(t, response, err, http.StatusOK)
	if !safetySMDIdentity(response.Body) {
		t.Fatal("SMD did not return the expected fixture node")
	}
	assignment.Spec.Selector = &Selector{Xnames: []string{safetyNode}}
	priority := 100
	assignment.Spec.Priority = &priority
	return &safetyFixture{t: t, ctx: ctx, client: client, assignment: assignment}
}

func safetyFixtureNew(t *testing.T) *safetyFixture {
	t.Helper()
	f := safetyFixtureSeeded(t)
	ctx, client, assignment := f.ctx, f.client, f.assignment
	response, err := client.ApplyAssignment(ctx, ConditionalAssignment{Resource: assignment, IfNoneMatch: "*"})
	safetyStatus(t, response, err, http.StatusCreated)
	value, err := DecodeAssignment(response)
	if err != nil || value.Metadata.UID == "" {
		t.Fatal("create fixture assignment returned incomplete identity")
	}
	_, readETag, _ := safetyReadAssignment(t, ctx, client, assignment.Metadata.Name)
	if safetyETag(t, response) != readETag {
		t.Fatal("create and read ETags differ")
	}
	f.artifact(safetyArtifactA)
	return f
}

func (f *safetyFixture) artifact(want BootConfigSpec) {
	f.t.Helper()
	public := BootClient{HTTP: f.client.HTTP}
	response, err := public.Raw(f.ctx, Request{Method: http.MethodGet, Path: "/bootscript?" + url.Values{"host": {safetyNode}}.Encode()})
	safetyStatus(f.t, response, err, http.StatusOK)
	if !safetyArtifactTuple(string(response.Body), want) {
		f.t.Fatal("boot script does not contain the expected complete artifact tuple")
	}
}

func (f *safetyFixture) snapshot() safetySnapshot {
	f.t.Helper()
	_, etag, body := safetyReadAssignment(f.t, f.ctx, f.client, f.assignment.Metadata.Name)
	series, err := f.client.GetSeries(f.ctx, f.assignment.Spec.ConfigRef.Name)
	safetyStatus(f.t, series, err, http.StatusOK)
	if _, err := DecodeSeries(series); err != nil {
		f.t.Fatalf("decode series: %v", err)
	}
	collection, err := f.client.Raw(f.ctx, Request{Method: http.MethodGet, Path: "/bootassignments"})
	safetyStatus(f.t, collection, err, http.StatusOK)
	values, err := DecodeJSON[[]BootAssignment](collection)
	if err != nil || values == nil {
		f.t.Fatal("invalid assignment collection")
	}
	digest, _ := safetyStoreDigest(f.t, f.ctx, "main")
	return safetySnapshot{assignment: body, assignETag: etag, series: series.Body, seriesETag: safetyETag(f.t, series), collection: collection.Body, configurationDigest: digest}
}

func (f *safetyFixture) unchanged(before safetySnapshot) {
	f.t.Helper()
	after := f.snapshot()
	if before.configurationDigest != after.configurationDigest || !safetyJSONEqual(before.assignment, after.assignment) || before.assignETag != after.assignETag || !safetyJSONEqual(before.series, after.series) || before.seriesETag != after.seriesETag || !safetyCollectionEqual(before.collection, after.collection) {
		f.t.Fatal("denied operation changed assignment or series")
	}
	f.artifact(safetyArtifactA)
}

func (f *safetyFixture) raw(method, path, etag string, body []byte) Response {
	f.t.Helper()
	headers := http.Header{"Content-Type": {"application/json"}}
	if etag != "" {
		headers.Set("If-Match", etag)
	}
	response, err := f.client.Raw(f.ctx, Request{Method: method, Path: path, Headers: headers, Body: body})
	if err != nil {
		f.t.Fatalf("request: %v", err)
	}
	return response
}

func (f *safetyFixture) encode(value BootAssignment) []byte {
	f.t.Helper()
	body, err := EncodeJSON(value)
	if err != nil {
		f.t.Fatal(err)
	}
	return body
}
