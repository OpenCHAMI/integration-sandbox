//go:build integration && declarative_boot

// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package declarativeboot

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"
)

func safetyLegacyAdoptionDryRun(t *testing.T) {
	f := safetyNewAdoption(t, false)
	before := f.snapshot()
	f.unchanged(before)
	for _, flags := range [][]string{nil, {"--dry-run"}} {
		output, err := f.run(flags...)
		if err != nil || len(bytes.TrimSpace(output)) == 0 {
			t.Fatal("adoption dry-run failed or returned no plan")
		}
		f.unchanged(before)
	}
	_, err := f.run("--dry-run", "--execute")
	if err == nil {
		t.Fatal("mutually exclusive adoption flags succeeded")
	}
	f.unchanged(before)
}

func safetyLegacyAdoptionReplay(t *testing.T) {
	f := safetyNewAdoption(t, false)
	before := f.snapshot()
	f.unchanged(before)
	var proxy struct {
		ProxyURL string `json:"proxyURL"`
	}
	f.h.runControl("adoption-interrupt-once", struct {
		SourceUID      string `json:"sourceUid"`
		SeriesName     string `json:"seriesName"`
		AssignmentName string `json:"assignmentName"`
	}{SourceUID: f.source.Metadata.UID, SeriesName: f.series, AssignmentName: f.assignment}, &proxy)
	f.h.requireSeparateBase(proxy.ProxyURL)
	output, runErr := f.runAt(proxy.ProxyURL, "--execute")
	if runErr == nil {
		t.Fatal("interrupted adoption unexpectedly succeeded")
	}
	var interrupted struct {
		Phase string `json:"phase"`
		Error string `json:"error"`
	}
	for _, line := range bytes.Split(output, []byte("\n")) {
		var value struct {
			Phase string `json:"phase"`
			Error string `json:"error"`
		}
		if json.Unmarshal(line, &value) == nil && value.Phase != "" {
			interrupted.Phase, interrupted.Error = value.Phase, value.Error
		}
	}
	if interrupted.Phase != "response" || interrupted.Error != "POST returned HTTP 502" {
		t.Fatalf("adoption did not reach the injected post-commit failure: phase=%q error=%q", interrupted.Phase, interrupted.Error)
	}
	partialSeries, err := f.h.admin.GetSeries(f.h.ctx, f.series)
	safetyStatus(t, partialSeries, err, http.StatusOK)
	partial, err := DecodeSeries(partialSeries)
	if err != nil || partial.RevisionCount != 1 || partial.DefaultRevisionUID != partial.LatestRevisionUID {
		t.Fatal("interrupted adoption did not leave one reusable revision")
	}
	partialAssignment, err := f.h.admin.GetAssignment(f.h.ctx, f.assignment)
	safetyStatus(t, partialAssignment, err, http.StatusNotFound)
	partialState := f.snapshot()
	if before.configurationDigest == partialState.configurationDigest || !safetyJSONEqual(before.source, partialState.source) || !safetyCollectionEqual(before.legacy, partialState.legacy) || !safetyCollectionEqual(before.assignments, partialState.assignments) {
		t.Fatal("interrupted adoption changed unrelated state or lost its first write")
	}
	if _, err := f.run("--execute"); err != nil {
		t.Fatal("adoption execute failed")
	}
	first := f.adopted()
	if first.uid != partial.DefaultRevisionUID {
		t.Fatal("retry replaced the interrupted revision UID")
	}
	firstState := f.snapshot()
	without, err := safetyWithoutAssignment(firstState.assignments, f.assignment)
	if err != nil || !safetyCollectionEqual(before.assignments, without) || !safetyJSONEqual(before.source, firstState.source) || !safetyCollectionEqual(before.legacy, firstState.legacy) {
		t.Fatal("adoption changed legacy or unrelated assignment records")
	}
	if _, err := f.run("--execute"); err != nil {
		t.Fatal("adoption replay failed")
	}
	second := f.adopted()
	secondState := f.snapshot()
	f.sameAdoption(first, second)
	if firstState.configurationDigest != secondState.configurationDigest || !safetyJSONEqual(firstState.source, secondState.source) || !safetyCollectionEqual(firstState.legacy, secondState.legacy) || !safetyCollectionEqual(firstState.assignments, secondState.assignments) {
		t.Fatal("adoption replay changed legacy or assignment collection")
	}
	if !safetyValidRevisionName("legacy-" + f.source.Metadata.UID) {
		secondTarget := *f
		secondTarget.series = safetyName(t)
		secondTarget.assignment = safetyName(t)
		secondTarget.cleanupAssignment()
		if _, err := secondTarget.run("--execute"); err != nil {
			t.Fatal("independent fallback-name adoption failed")
		}
		other := secondTarget.adopted()
		if other.revisionName != first.revisionName {
			t.Fatal("fallback revision name is not deterministic across target series")
		}
		f.sameAdoption(first, f.adopted())
		alternateState := f.snapshot()
		withoutAlternate, err := safetyWithoutAssignment(alternateState.assignments, secondTarget.assignment)
		if err != nil || !safetyCollectionEqual(firstState.assignments, withoutAlternate) || !safetyJSONEqual(firstState.source, alternateState.source) || !safetyCollectionEqual(firstState.legacy, alternateState.legacy) {
			t.Fatal("fallback adoption changed source or unrelated assignments")
		}
	}
}

func safetyLegacyAdoptionUnsafePreflight(t *testing.T) {
	f := safetyNewAdoption(t, true)
	before := f.snapshot()
	f.unchanged(before)
	output, err := f.run("--execute")
	if err == nil {
		t.Fatal("unsafe multi-family adoption succeeded")
	}
	if !bytes.Contains(output, []byte(f.source.Metadata.UID)) {
		t.Fatal("unsafe preflight did not identify the source UID")
	}
	f.unchanged(before)
}
