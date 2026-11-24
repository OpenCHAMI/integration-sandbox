//go:build integration && declarative_boot

// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package declarativeboot

import (
	"context"
	"net/http"
	"testing"
)

func safetyPromotionRace(t *testing.T) {
	f := safetyFixtureNew(t)
	series := f.assignment.Spec.ConfigRef.Name
	third := BootConfigSpec{Kernel: "https://images.example.invalid/boot-images/acceptance-c-kernel", Initrd: "https://images.example.invalid/boot-images/acceptance-c-initrd", Params: "release=C"}
	revisions := []NamedConfig{
		{RevisionName: "release-b", Resource: BootConfig{Metadata: Metadata{Name: series}, Spec: safetyArtifactB}},
		{RevisionName: "release-c", Resource: BootConfig{Metadata: Metadata{Name: series}, Spec: third}},
	}
	ids := make([]string, len(revisions))
	for i, revision := range revisions {
		response, err := f.client.EnsureRevision(f.ctx, series, revision)
		safetyStatus(t, response, err, http.StatusCreated)
		record, err := DecodeRevision(response)
		if err != nil || record.Metadata.SeriesName != series || record.Resource.Spec != revision.Resource.Spec {
			t.Fatal("ensure returned incorrect revision")
		}
		ids[i] = record.Metadata.UID
	}
	if ids[0] == ids[1] {
		t.Fatal("distinct revisions have the same UID")
	}
	before := f.snapshot()
	for _, condition := range []string{"", safetyStale(before.seriesETag), "*"} {
		response, err := f.client.PromoteDefault(f.ctx, Promotion{Series: series, RevisionUID: ids[0], IfMatch: condition})
		safetyStatus(t, response, err, http.StatusPreconditionFailed)
		f.unchanged(before)
	}
	jobs := make([]func(context.Context) (Response, error), len(ids))
	for i, uid := range ids {
		jobs[i] = func(ctx context.Context) (Response, error) {
			return f.client.PromoteDefault(ctx, Promotion{Series: series, RevisionUID: uid, IfMatch: before.seriesETag})
		}
	}
	results := safetyRace(f.ctx, jobs)
	winner := safetyCheckRace(t, results, http.StatusOK)
	response, err := f.client.GetSeries(f.ctx, series)
	safetyStatus(t, response, err, http.StatusOK)
	state, err := DecodeSeries(response)
	if err != nil || state.DefaultRevisionUID != ids[winner] || state.LatestRevisionUID != ids[1] || state.RevisionCount != 3 {
		t.Fatal("promotion race did not retain the single winning pointer")
	}
	_, currentETag, current := safetyReadAssignment(t, f.ctx, f.client, f.assignment.Metadata.Name)
	safetySameAssignment(t, before.assignment, current, before.assignETag, currentETag)
	if winner == 0 {
		f.artifact(safetyArtifactB)
	} else {
		f.artifact(third)
	}
}
