//go:build integration && declarative_boot

// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package declarativeboot

import (
	"context"
	"net/http"
	"net/url"
	"testing"
	"time"
)

func (f *safetyFixture) stableWith(before safetySnapshot, artifact BootConfigSpec) {
	f.t.Helper()
	after := f.snapshot()
	if before.configurationDigest != after.configurationDigest || !safetyJSONEqual(before.assignment, after.assignment) || before.assignETag != after.assignETag || !safetyJSONEqual(before.series, after.series) || before.seriesETag != after.seriesETag || !safetyCollectionEqual(before.collection, after.collection) {
		f.t.Fatal("operation changed protected state")
	}
	f.artifact(artifact)
}

func (f *safetyFixture) seriesInfo() SeriesStatus {
	f.t.Helper()
	response, err := f.client.GetSeries(f.ctx, f.assignment.Spec.ConfigRef.Name)
	safetyStatus(f.t, response, err, http.StatusOK)
	state, err := DecodeSeries(response)
	if err != nil {
		f.t.Fatal(err)
	}
	return state
}

func safetyLatestProposer(t *testing.T) {
	f := safetyFixtureNew(t)
	proposer := f.role([]string{"read:boot", "propose:boot"})
	initial := f.seriesInfo()
	b := f.ensureB(proposer)
	state := f.seriesInfo()
	if state.DefaultRevisionUID != initial.DefaultRevisionUID || state.LatestRevisionUID != b.Metadata.UID || state.RevisionCount != 2 {
		t.Fatal("proposal advanced default or failed to advance latest")
	}
	_, etag, _ := safetyReadAssignment(t, f.ctx, f.client, f.assignment.Metadata.Name)
	follower := f.assignment
	follower.Spec.ConfigRef.Selector = "latest"
	response, err := f.client.ApplyAssignment(f.ctx, ConditionalAssignment{Resource: follower, IfMatch: etag})
	safetyStatus(t, response, err, http.StatusOK)
	f.artifact(safetyArtifactB)
	before := f.snapshot()
	series := f.assignment.Spec.ConfigRef.Name
	c := BootConfigSpec{Kernel: "/safety-c-kernel", Initrd: "/safety-c-initrd", Params: "release=C"}
	response, err = proposer.EnsureRevision(f.ctx, series, NamedConfig{RevisionName: "release-c", Resource: BootConfig{Metadata: Metadata{Name: series}, Spec: c}})
	safetyStatus(t, response, err, http.StatusForbidden)
	f.stableWith(before, safetyArtifactB)
	response, err = proposer.Raw(f.ctx, Request{Method: http.MethodPost, Path: "/bootconfigs/" + url.PathEscape(series) + "/revisions", Headers: http.Header{"Content-Type": {"application/json"}}, Body: f.configFor(series, c)})
	safetyStatus(t, response, err, http.StatusForbidden)
	f.stableWith(before, safetyArtifactB)
	response, err = proposer.EnsureRevision(f.ctx, series, NamedConfig{RevisionName: "release-b", Resource: BootConfig{Metadata: Metadata{Name: series}, Spec: safetyArtifactB}})
	safetyStatus(t, response, err, http.StatusOK)
	replay, err := DecodeRevision(response)
	if err != nil || replay.Metadata.UID != b.Metadata.UID || replay.Metadata.Digest != b.Metadata.Digest {
		t.Fatal("identical replay changed revision identity")
	}
	f.stableWith(before, safetyArtifactB)
}

func (f *safetyFixture) cleanupName(name string) {
	f.t.Helper()
	f.t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		response, err := f.client.GetAssignment(ctx, name)
		if err != nil {
			f.t.Errorf("follower cleanup read: %v", err)
			return
		}
		if response.Status == http.StatusNotFound {
			return
		}
		if response.Status != http.StatusOK || !safetyStrongETag(response.Header.Get("ETag")) {
			f.t.Errorf("follower cleanup read HTTP %d or invalid ETag", response.Status)
			return
		}
		deleted, err := f.client.DeleteAssignment(ctx, ConditionalName{Name: name, IfMatch: response.Header.Get("ETag")})
		if err != nil || (deleted.Status != http.StatusOK && deleted.Status != http.StatusNoContent) {
			f.t.Errorf("follower cleanup delete HTTP %d: %v", deleted.Status, err)
		}
	})
}

func (f *safetyFixture) nodeArtifact(node string, expected BootConfigSpec) {
	f.t.Helper()
	public := BootClient{HTTP: f.client.HTTP}
	response, err := public.Raw(f.ctx, Request{Method: http.MethodGet, Path: "/bootscript?" + url.Values{"host": {node}}.Encode()})
	safetyStatus(f.t, response, err, http.StatusOK)
	if !safetyArtifactTuple(string(response.Body), expected) {
		f.t.Fatal("follower boot script has the wrong artifact tuple")
	}
}

func safetyLatestFollowerRace(t *testing.T) {
	f := safetyFixtureNew(t)
	proposer := f.role([]string{"read:boot", "propose:boot"})
	initial := f.seriesInfo()
	series := f.assignment.Spec.ConfigRef.Name
	name := safetyName(t)
	f.cleanupName(name)
	node := "x0c0s2b0n0"
	priority := 100
	follower := BootAssignment{Metadata: Metadata{Name: name}, Spec: AssignmentSpec{Selector: &Selector{Xnames: []string{node}}, Priority: &priority, ConfigRef: ConfigReference{Name: series, Selector: "latest"}}}
	jobs := []func(context.Context) (Response, error){
		func(ctx context.Context) (Response, error) {
			return proposer.EnsureRevision(ctx, series, NamedConfig{RevisionName: "release-b", Resource: BootConfig{Metadata: Metadata{Name: series}, Spec: safetyArtifactB}})
		},
		func(ctx context.Context) (Response, error) {
			return f.client.ApplyAssignment(ctx, ConditionalAssignment{Resource: follower, IfNoneMatch: "*"})
		},
	}
	results := safetyRace(f.ctx, jobs)
	if results[0].err != nil || results[1].err != nil || results[1].response.Status != http.StatusCreated || (results[0].response.Status != http.StatusCreated && results[0].response.Status != http.StatusForbidden) {
		t.Fatalf("follower race returned HTTP %d and %d", results[0].response.Status, results[1].response.Status)
	}
	actual, _, _ := safetyReadAssignment(t, f.ctx, f.client, name)
	if actual.Spec.ConfigRef != follower.Spec.ConfigRef || actual.Metadata.UID == "" {
		t.Fatal("promoter did not create the explicit latest follower")
	}
	state := f.seriesInfo()
	if state.DefaultRevisionUID != initial.DefaultRevisionUID {
		t.Fatal("race changed default without promotion")
	}
	expected := safetyArtifactA
	if results[0].response.Status == http.StatusCreated {
		b, err := DecodeRevision(results[0].response)
		if err != nil || state.LatestRevisionUID != b.Metadata.UID || state.RevisionCount != 2 {
			t.Fatal("proposal-first race has inconsistent latest")
		}
		expected = safetyArtifactB
	} else if state.LatestRevisionUID != initial.LatestRevisionUID || state.RevisionCount != 1 {
		t.Fatal("denied proposal advanced latest")
	}
	f.artifact(safetyArtifactA)
	f.nodeArtifact(node, expected)
	before := f.snapshot()
	response, err := proposer.EnsureRevision(f.ctx, series, NamedConfig{RevisionName: "release-c", Resource: BootConfig{Metadata: Metadata{Name: series}, Spec: BootConfigSpec{Kernel: "/safety-c-kernel", Initrd: "/safety-c-initrd", Params: "release=C"}}})
	safetyStatus(t, response, err, http.StatusForbidden)
	f.stableWith(before, safetyArtifactA)
	f.nodeArtifact(node, expected)
}
