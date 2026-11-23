//go:build integration && declarative_boot

// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package declarativeboot

import (
	"context"
	"net/http"
	"net/url"
	"testing"
)

func safetyUIDRace(t *testing.T) {
	f := safetyFixtureNew(t)
	before := f.snapshot()
	stored, _, _ := safetyReadAssignment(t, f.ctx, f.client, f.assignment.Metadata.Name)
	path := "/bootassignments/" + url.PathEscape(stored.Metadata.UID)
	changed := f.assignment
	priority := 99
	changed.Spec.Priority = &priority
	put := f.encode(changed)
	patch := []byte(`{"priority":98}`)
	for _, condition := range []string{"", safetyStale(before.assignETag), "*"} {
		for _, request := range []Request{
			{Method: http.MethodPut, Path: path, Headers: http.Header{"Content-Type": {"application/json"}}, Body: put},
			{Method: http.MethodPatch, Path: path, Headers: http.Header{"Content-Type": {"application/merge-patch+json"}}, Body: patch},
			{Method: http.MethodDelete, Path: path},
		} {
			if request.Headers == nil {
				request.Headers = make(http.Header)
			}
			if condition != "" {
				request.Headers.Set("If-Match", condition)
			}
			response, err := f.client.Raw(f.ctx, request)
			safetyStatus(t, response, err, http.StatusPreconditionFailed)
			f.unchanged(before)
		}
	}
	badRef := changed
	badRef.Spec.ConfigRef.RevisionName = "missing-revision"
	response := f.raw(http.MethodPut, path, before.assignETag, f.encode(badRef))
	safetyStatus(t, response, nil, http.StatusUnprocessableEntity)
	f.unchanged(before)
	jobs := []func(context.Context) (Response, error){
		func(ctx context.Context) (Response, error) {
			return f.client.Raw(ctx, Request{Method: http.MethodPut, Path: path, Headers: http.Header{"Content-Type": {"application/json"}, "If-Match": {before.assignETag}}, Body: put})
		},
		func(ctx context.Context) (Response, error) {
			return f.client.Raw(ctx, Request{Method: http.MethodPatch, Path: path, Headers: http.Header{"Content-Type": {"application/merge-patch+json"}, "If-Match": {before.assignETag}}, Body: patch})
		},
	}
	results := safetyRace(f.ctx, jobs)
	winner := safetyCheckRace(t, results, http.StatusOK)
	current, etag, _ := safetyReadAssignment(t, f.ctx, f.client, f.assignment.Metadata.Name)
	want := []int{99, 98}[winner]
	if current.Metadata.UID != stored.Metadata.UID || current.Spec.Priority == nil || *current.Spec.Priority != want || etag == before.assignETag || safetyETag(t, results[winner].response) != etag {
		t.Fatal("UID race did not commit exactly the winning mutation")
	}
	f.artifact(safetyArtifactA)
	lastPriority := 97
	last := f.assignment
	last.Spec.Priority = &lastPriority
	lastBody := f.encode(last)
	jobs = []func(context.Context) (Response, error){
		func(ctx context.Context) (Response, error) {
			return f.client.Raw(ctx, Request{Method: http.MethodPut, Path: path, Headers: http.Header{"Content-Type": {"application/json"}, "If-Match": {etag}}, Body: lastBody})
		},
		func(ctx context.Context) (Response, error) {
			return f.client.Raw(ctx, Request{Method: http.MethodDelete, Path: path, Headers: http.Header{"If-Match": {etag}}})
		},
	}
	results = safetyRace(f.ctx, jobs)
	if results[0].err != nil || results[1].err != nil {
		t.Fatal("UID update/delete race transport failure")
	}
	updated := results[0].response.Status == http.StatusOK && results[1].response.Status == http.StatusPreconditionFailed
	deleted := results[0].response.Status == http.StatusPreconditionFailed && (results[1].response.Status == http.StatusOK || results[1].response.Status == http.StatusNoContent)
	if !updated && !deleted {
		t.Fatalf("UID update/delete race returned HTTP %d and %d", results[0].response.Status, results[1].response.Status)
	}
	if updated {
		current, etag, _ = safetyReadAssignment(t, f.ctx, f.client, f.assignment.Metadata.Name)
		if current.Metadata.UID != stored.Metadata.UID || current.Spec.Priority == nil || *current.Spec.Priority != lastPriority {
			t.Fatal("winning UID update did not persist")
		}
		f.artifact(safetyArtifactA)
		response = f.raw(http.MethodDelete, path, etag, nil)
		if response.Status != http.StatusOK && response.Status != http.StatusNoContent {
			t.Fatalf("guarded UID delete returned HTTP %d", response.Status)
		}
	}
	response, err := f.client.GetAssignment(f.ctx, f.assignment.Metadata.Name)
	safetyStatus(t, response, err, http.StatusNotFound)
}
