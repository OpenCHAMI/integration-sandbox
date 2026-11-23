//go:build integration && declarative_boot

// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package declarativeboot

import (
	"net/http"
	"net/url"
	"testing"
)

type safetyProtectedRoute struct {
	name    string
	scope   string
	request Request
}

func (f *safetyFixture) securityRoutes(bUID string) []safetyProtectedRoute {
	f.t.Helper()
	state := f.snapshot()
	assignment, _, _ := safetyReadAssignment(f.t, f.ctx, f.client, f.assignment.Metadata.Name)
	series := "/bootconfigs/" + url.PathEscape(f.assignment.Spec.ConfigRef.Name)
	uid := "/bootassignments/" + url.PathEscape(assignment.Metadata.UID)
	name := "/bootassignments/by-name/" + url.PathEscape(assignment.Metadata.Name)
	config, err := EncodeJSON(BootConfig{Metadata: Metadata{Name: f.assignment.Spec.ConfigRef.Name}, Spec: safetyArtifactB})
	if err != nil {
		f.t.Fatal(err)
	}
	promote, err := EncodeJSON(promotionBody{RevisionUID: bUID})
	if err != nil {
		f.t.Fatal(err)
	}
	body := f.encode(f.assignment)
	request := func(method, path, etag string, value []byte, contentType string) Request {
		header := http.Header{}
		if etag != "" {
			header.Set("If-Match", etag)
		}
		if contentType != "" {
			header.Set("Content-Type", contentType)
		}
		return Request{Method: method, Path: path, Headers: header, Body: value}
	}
	newName := safetyName(f.t)
	newAssignment := f.assignment
	newAssignment.Metadata.Name = newName
	createHeader := http.Header{"If-None-Match": {"*"}, "Content-Type": {"application/json"}}
	return []safetyProtectedRoute{
		{"assignment-list", "read", request("GET", "/bootassignments", "", nil, "")},
		{"assignment-uid-read", "read", request("GET", uid, "", nil, "")},
		{"assignment-name-read", "read", request("GET", name, "", nil, "")},
		{"series-read", "read", request("GET", series, "", nil, "")},
		{"revisions-read", "read", request("GET", series+"/revisions", "", nil, "")},
		{"revision-read", "read", request("GET", series+"/revisions/by-uid/"+url.PathEscape(bUID), "", nil, "")},
		{"resolve", "read", request("GET", series+"/resolve?selector=default", "", nil, "")},
		{"revision-create", "propose", request("POST", series+"/revisions", "", config, "application/json")},
		{"revision-ensure", "propose", request("PUT", series+"/revisions/release-c", "", config, "application/json")},
		{"series-create", "propose", request("POST", "/bootconfigs?revisionName=release-a", "", f.configFor(safetyName(f.t), safetyArtifactA), "application/json")},
		{"default-promote", "promote", request("PUT", series+"/default", state.seriesETag, promote, "application/json")},
		{"assignment-post", "promote", request("POST", "/bootassignments", "", f.encode(newAssignment), "application/json")},
		{"assignment-named-create", "promote", Request{Method: "PUT", Path: "/bootassignments/by-name/" + url.PathEscape(newName), Headers: createHeader, Body: f.encode(newAssignment)}},
		{"assignment-named-update", "promote", request("PUT", name, state.assignETag, body, "application/json")},
		{"assignment-named-delete", "promote", request("DELETE", name, state.assignETag, nil, "")},
		{"assignment-uid-update", "promote", request("PUT", uid, state.assignETag, body, "application/json")},
		{"assignment-uid-patch", "promote", request("PATCH", uid, state.assignETag, []byte(`{"priority":99}`), "application/merge-patch+json")},
		{"assignment-uid-delete", "promote", request("DELETE", uid, state.assignETag, nil, "")},
		{"assignment-status-put", "promote", request("PUT", uid+"/status", state.assignETag, []byte(`{}`), "application/json")},
		{"assignment-status-patch", "promote", request("PATCH", uid+"/status", state.assignETag, []byte(`{}`), "application/merge-patch+json")},
		{"series-status-put", "promote", request("PUT", series+"/status", state.seriesETag, []byte(`{}`), "application/json")},
		{"series-status-patch", "promote", request("PATCH", series+"/status", state.seriesETag, []byte(`{}`), "application/merge-patch+json")},
		{"revision-retire", "promote", request("DELETE", series+"/revisions/by-uid/"+url.PathEscape(bUID), state.seriesETag, nil, "")},
	}
}

func (f *safetyFixture) configFor(name string, spec BootConfigSpec) []byte {
	f.t.Helper()
	body, err := EncodeJSON(BootConfig{Metadata: Metadata{Name: name}, Spec: spec})
	if err != nil {
		f.t.Fatal(err)
	}
	return body
}

func (f *safetyFixture) ensureB(client BootClient) RevisionRecord {
	f.t.Helper()
	series := f.assignment.Spec.ConfigRef.Name
	response, err := client.EnsureRevision(f.ctx, series, NamedConfig{RevisionName: "release-b", Resource: BootConfig{Metadata: Metadata{Name: series}, Spec: safetyArtifactB}})
	safetyStatus(f.t, response, err, http.StatusCreated)
	record, err := DecodeRevision(response)
	if err != nil || record.Metadata.SeriesName != series || record.Resource.Spec != safetyArtifactB {
		f.t.Fatal("B revision did not match fixture")
	}
	f.artifact(safetyArtifactA)
	return record
}

func safetyRequireResponse(t *testing.T, route safetyProtectedRoute, response Response, err error, want int) {
	t.Helper()
	if err != nil || response.Status != want {
		t.Fatalf("route %s returned HTTP %d, error %v; want %d", route.name, response.Status, err, want)
	}
}
