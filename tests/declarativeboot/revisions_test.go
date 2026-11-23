//go:build integration && declarative_boot

// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package declarativeboot

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHTTPHelpersRevisionResponses(t *testing.T) {
	response := Response{Status: 201, Body: []byte(`{"metadata":{"uid":"rev-1","seriesName":"os","revisionName":"a","number":1,"digest":"sha256:opaque","createdAt":"2026-01-01T00:00:00Z"},"resource":{"metadata":{"name":"os"},"spec":{"kernel":"s3://fixture"}}}`)}
	record, err := DecodeRevision(response)
	if err != nil || record.Metadata.UID != "rev-1" || record.Metadata.Digest != "sha256:opaque" || record.Resource.Spec.Kernel != "s3://fixture" {
		t.Fatalf("decode record: %+v %v", record, err)
	}
	if _, err := DecodeRevision(Response{Body: []byte(`{"metadata":{"uid":"rev-1"},"resource":{}}`)}); err == nil {
		t.Fatal("missing required revision fields accepted")
	}
	if _, err := DecodeSeries(Response{Body: []byte(`{"name":"os"}`)}); err == nil {
		t.Fatal("incomplete series accepted")
	}
	if _, err := DecodeResolution(Response{Body: []byte(`{"seriesName":"os","revision":{}}`)}); err == nil {
		t.Fatal("incomplete resolution accepted")
	}
}

func TestHTTPHelpersRevisionRoutes(t *testing.T) {
	requests := make(chan *http.Request, 8)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests <- r.Clone(r.Context()); w.WriteHeader(200) }))
	defer server.Close()
	c, err := NewHTTPClient(server.URL, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	b := BootClient{HTTP: c}
	calls := []struct {
		name, method, path, query, match string
		run                              func() (Response, error)
	}{
		{"series", "GET", "/bootconfigs/os", "", "", func() (Response, error) { return b.GetSeries(context.Background(), "os") }},
		{"list", "GET", "/bootconfigs/os/revisions", "", "", func() (Response, error) { return b.ListRevisions(context.Background(), "os") }},
		{"revision", "GET", "/bootconfigs/os/revisions/by-uid/rev-1", "", "", func() (Response, error) {
			return b.GetRevision(context.Background(), RevisionID{Series: "os", UID: "rev-1"})
		}},
		{"resolve", "GET", "/bootconfigs/os/resolve", "selector=latest", "", func() (Response, error) {
			return b.ResolveRevision(context.Background(), ConfigReference{Name: "os", Selector: "latest"})
		}},
		{"promote", "PUT", "/bootconfigs/os/default", "", `"etag"`, func() (Response, error) {
			return b.PromoteDefault(context.Background(), Promotion{Series: "os", RevisionUID: "rev-1", IfMatch: `"etag"`})
		}},
		{"read assignment", "GET", "/bootassignments/by-name/canary", "", "", func() (Response, error) { return b.GetAssignment(context.Background(), "canary") }},
		{"delete assignment", "DELETE", "/bootassignments/by-name/canary", "", `"assignment"`, func() (Response, error) {
			return b.DeleteAssignment(context.Background(), ConditionalName{Name: "canary", IfMatch: `"assignment"`})
		}},
	}
	for _, tc := range calls {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := tc.run(); err != nil {
				t.Fatal(err)
			}
			got := <-requests
			if got.Method != tc.method || got.URL.Path != tc.path || got.URL.RawQuery != tc.query || got.Header.Get("If-Match") != tc.match {
				t.Fatalf("incorrect request: %s %s %q", got.Method, got.URL.String(), got.Header.Get("If-Match"))
			}
		})
	}
}

func TestFixtureCompletePacket(t *testing.T) {
	f, err := LoadFixtures("../../fixtures/declarative-boot")
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Negative.Cases) != 6 || len(f.Scenarios.Scenarios) != 36 || f.Legacy.Adoption.SourceUIDBinding != "legacySource.uid" || f.Assignments.ReferenceVariants.UIDA.Bind["/uid"] != "revisionA.uid" {
		t.Fatalf("incomplete fixture packet: %+v", f)
	}
}

func TestFixtureRejectsDuplicateScenario(t *testing.T) {
	f, err := LoadFixtures("../../fixtures/declarative-boot")
	if err != nil {
		t.Fatal(err)
	}
	f.Scenarios.Scenarios[1].ID = f.Scenarios.Scenarios[0].ID
	if err := f.Validate(); err == nil {
		t.Fatal("duplicate manifest scenario accepted")
	}
}
