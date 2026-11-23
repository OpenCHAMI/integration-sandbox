//go:build integration && declarative_boot

// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package declarativeboot

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHTTPHelpersBootRoutes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/bootconfigs" || r.URL.Query().Get("revisionName") != "release-a" {
			t.Errorf("wrong create request: %s %s", r.Method, r.URL.String())
		}
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Error("missing bearer")
		}
		var request BootConfig
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil || request.Metadata.Name != "acceptance-os" || request.Spec.Kernel != "fixture-a" {
			t.Errorf("wrong request: %+v %v", request, err)
		}
		w.Header().Set("ETag", `"opaque"`)
		w.WriteHeader(201)
		_, _ = w.Write([]byte(`{"metadata":{"uid":"server-uid","seriesName":"acceptance-os","revisionName":"release-a","number":1,"digest":"digest-a","createdAt":"2026-01-01T00:00:00Z"},"resource":{"metadata":{"name":"acceptance-os"},"spec":{"kernel":"fixture-a"}}}`))
	}))
	defer server.Close()
	client, err := NewHTTPClient(server.URL, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	boot := BootClient{HTTP: client, Bearer: "test-token"}
	response, err := boot.CreateConfig(context.Background(), NamedConfig{RevisionName: "release-a", Resource: BootConfig{Metadata: Metadata{Name: "acceptance-os"}, Spec: BootConfigSpec{Kernel: "fixture-a"}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := response.RequireStatus(201); err != nil {
		t.Fatal(err)
	}
	record, err := DecodeRevision(response)
	if err != nil || record.Metadata.UID != "server-uid" || record.Metadata.SeriesName != "acceptance-os" || record.Metadata.RevisionName != "release-a" || record.Metadata.Number != 1 || record.Metadata.Digest != "digest-a" || !record.Metadata.CreatedAt.Equal(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)) || record.Resource.Metadata.Name != "acceptance-os" || record.Resource.Spec.Kernel != "fixture-a" || response.Header.Get("ETag") != `"opaque"` {
		t.Fatalf("incorrect revision record: %+v %v", record, err)
	}
}

func TestFixtureReadsIndependentExpected(t *testing.T) {
	fixture, err := LoadFixtures("../../fixtures/declarative-boot")
	if err != nil {
		t.Fatal(err)
	}
	if fixture.Configs.Revisions["A"].Spec.Kernel != "https://images.example.invalid/boot-images/acceptance-a-kernel" || fixture.Expected.Artifacts["B"].Kernel != "https://images.example.invalid/boot-images/acceptance-b-kernel" || len(fixture.Nodes.Nodes) != 4 {
		t.Fatalf("wrong fixture: %+v", fixture)
	}
}

func TestFixtureRejectsIncomplete(t *testing.T) {
	if err := (Fixtures{}).Validate(); err == nil {
		t.Fatal("empty fixture accepted as ready")
	}
}

func TestHTTPHelpersEnsureRevisionRoute(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
	}{{"created", http.StatusCreated}, {"replay", http.StatusOK}} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPut || r.URL.Path != "/bootconfigs/acceptance-os/revisions/release-b" {
					t.Errorf("wrong ensure path: %s %s", r.Method, r.URL.Path)
				}
				var body BootConfig
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Spec.Kernel != "fixture-b" {
					t.Errorf("wrong ensure body: %+v %v", body, err)
				}
				w.Header().Set("ETag", `"opaque-b"`)
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(`{"metadata":{"uid":"revision-b","seriesName":"acceptance-os","revisionName":"release-b","number":2,"digest":"digest-b","createdAt":"2026-01-01T00:00:00Z"},"resource":{"metadata":{"name":"acceptance-os"},"spec":{"kernel":"fixture-b"}}}`))
			}))
			defer server.Close()
			client, err := NewHTTPClient(server.URL, time.Second)
			if err != nil {
				t.Fatal(err)
			}
			response, err := (BootClient{HTTP: client}).EnsureRevision(context.Background(), "acceptance-os", NamedConfig{RevisionName: "release-b", Resource: BootConfig{Spec: BootConfigSpec{Kernel: "fixture-b"}}})
			if err != nil {
				t.Fatal(err)
			}
			if err := response.RequireStatus(tc.status); err != nil {
				t.Fatal(err)
			}
			record, err := DecodeRevision(response)
			if err != nil || record.Metadata.UID != "revision-b" || record.Metadata.Digest != "digest-b" || record.Metadata.RevisionName != "release-b" || record.Resource.Spec.Kernel != "fixture-b" || response.Header.Get("ETag") != `"opaque-b"` {
				t.Fatalf("wrong ensure record: %+v %v", record, err)
			}
		})
	}
}

func TestHTTPHelpersRevisionRejectsPlainResourceOrMissingMetadata(t *testing.T) {
	for _, tc := range []struct{ name, body string }{
		{"plain resource", `{"metadata":{"name":"acceptance-os","uid":"server-uid"},"spec":{"kernel":"fixture-a"}}`},
		{"missing digest", `{"metadata":{"uid":"server-uid","seriesName":"acceptance-os","revisionName":"release-a","number":1,"createdAt":"2026-01-01T00:00:00Z"},"resource":{"metadata":{"name":"acceptance-os"},"spec":{"kernel":"fixture-a"}}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := DecodeRevision(Response{Status: http.StatusCreated, Body: []byte(tc.body)}); err == nil {
				t.Fatal("invalid revision record accepted")
			}
		})
	}
}

func TestHTTPHelpersNamedAssignmentPreconditions(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/bootassignments/by-name/group-default" || r.Header.Get("If-None-Match") != "*" {
			t.Error("incorrect assignment request")
		}
		var body BootAssignment
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Metadata.Name != "group-default" || body.Spec.ConfigRef.Name != "acceptance-os" {
			t.Errorf("incorrect assignment body: %+v %v", body, err)
		}
		w.Header().Set("ETag", `"assignment-etag"`)
		w.WriteHeader(201)
	}))
	defer server.Close()
	client, err := NewHTTPClient(server.URL, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	response, err := (BootClient{HTTP: client}).ApplyAssignment(context.Background(), ConditionalAssignment{Resource: BootAssignment{Metadata: Metadata{Name: "group-default"}, Spec: AssignmentSpec{ConfigRef: ConfigReference{Name: "acceptance-os"}}}, IfNoneMatch: "*"})
	if err != nil {
		t.Fatal(err)
	}
	if err := response.RequireStatus(201); err != nil {
		t.Fatal(err)
	}
	if response.Header.Get("ETag") != `"assignment-etag"` {
		t.Fatal("missing ETag")
	}
}
