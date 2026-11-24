//go:build integration && declarative_boot

// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package declarativeboot

import (
	"encoding/json"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func safetyAssignmentShape(t *testing.T) {
	f := safetyFixtureNew(t)
	before := f.snapshot()
	series := f.assignment.Spec.ConfigRef.Name
	for _, tc := range []struct {
		name         string
		resourceName string
		spec         string
	}{
		{"leading-hyphen", "-invalid-name", `{"configRef":{"name":` + strconv.Quote(series) + `}}`},
		{"invalid-name", "invalid_name", `{"configRef":{"name":` + strconv.Quote(series) + `}}`},
		{"long-name", strings.Repeat("a", 254), `{"configRef":{"name":` + strconv.Quote(series) + `}}`},
		{"missing-ref", safetyName(t), `{}`},
		{"negative-priority", safetyName(t), `{"priority":-1,"configRef":{"name":` + strconv.Quote(series) + `}}`},
		{"high-priority", safetyName(t), `{"priority":101,"configRef":{"name":` + strconv.Quote(series) + `}}`},
		{"unknown-selector-field", safetyName(t), `{"selector":{"unknown":["x"]},"configRef":{"name":` + strconv.Quote(series) + `}}`},
		{"unknown-ref-field", safetyName(t), `{"configRef":{"name":` + strconv.Quote(series) + `,"unknown":"x"}}`},
		{"unknown-reference-selector", safetyName(t), `{"configRef":{"name":` + strconv.Quote(series) + `,"selector":"other"}}`},
	} {
		body, err := json.Marshal(struct {
			Metadata Metadata        `json:"metadata"`
			Spec     json.RawMessage `json:"spec"`
		}{Metadata: Metadata{Name: tc.resourceName}, Spec: json.RawMessage(tc.spec)})
		if err != nil {
			t.Fatal(err)
		}
		response := f.raw(http.MethodPost, "/bootassignments", "", body)
		if response.Status != http.StatusBadRequest && response.Status != http.StatusUnprocessableEntity {
			t.Fatalf("%s returned HTTP %d", tc.name, response.Status)
		}
		f.unchanged(before)
	}
	seriesResponse, err := f.client.GetSeries(f.ctx, series)
	safetyStatus(t, seriesResponse, err, http.StatusOK)
	seriesState, err := DecodeSeries(seriesResponse)
	if err != nil {
		t.Fatal(err)
	}
	stored, etag, _ := safetyReadAssignment(t, f.ctx, f.client, f.assignment.Metadata.Name)
	zero, hundred := 0, 100
	for i, ref := range []ConfigReference{
		{Name: series},
		{Name: series, Selector: "default"},
		{Name: series, Selector: "latest"},
		{Name: series, RevisionName: "release-a"},
		{Name: series, UID: seriesState.DefaultRevisionUID},
	} {
		changed := f.assignment
		changed.Spec.ConfigRef = ref
		changed.Spec.Selector = &Selector{MACs: []string{"02:00:00:00:00:03"}, NIDs: []int32{1003}, Xnames: []string{safetyNode}, Groups: []string{"safety-unmatched"}}
		changed.Spec.Priority = &zero
		if i%2 == 1 {
			changed.Spec.Priority = &hundred
		}
		response, err := f.client.ApplyAssignment(f.ctx, ConditionalAssignment{Resource: changed, IfMatch: etag})
		safetyStatus(t, response, err, http.StatusOK)
		current, currentETag, _ := safetyReadAssignment(t, f.ctx, f.client, f.assignment.Metadata.Name)
		if current.Metadata.UID != stored.Metadata.UID || !safetyEquivalentSpec(current.Spec, changed.Spec) || safetyETag(t, response) != currentETag {
			t.Fatalf("valid assignment form %d did not retain its spec and UID", i)
		}
		etag = currentETag
		f.artifact(safetyArtifactA)
	}
}

func safetyMalformedInput(t *testing.T) {
	f := safetyFixtureNew(t)
	before := f.snapshot()
	assignment, _, _ := safetyReadAssignment(t, f.ctx, f.client, f.assignment.Metadata.Name)
	conflict := f.assignment
	conflict.Spec.ConfigRef.RevisionName = "release-a"
	conflict.Spec.ConfigRef.Selector = "latest"
	for _, tc := range []struct {
		name   string
		method string
		path   string
		body   []byte
	}{
		{"malformed-post", http.MethodPost, "/bootassignments", []byte(`{"spec":`)},
		{"malformed-uid-put", http.MethodPut, "/bootassignments/" + url.PathEscape(assignment.Metadata.UID), []byte(`{"spec":`)},
		{"multiple-discriminators", http.MethodPut, "/bootassignments/" + url.PathEscape(assignment.Metadata.UID), f.encode(conflict)},
	} {
		response := f.raw(tc.method, tc.path, before.assignETag, tc.body)
		if response.Status != http.StatusBadRequest && response.Status != http.StatusUnprocessableEntity {
			t.Fatalf("%s returned HTTP %d", tc.name, response.Status)
		}
		f.unchanged(before)
	}
}

func safetyInvalidReferences(t *testing.T) {
	f := safetyFixtureNew(t)
	other := safetyName(t)
	response, err := f.client.CreateConfig(f.ctx, NamedConfig{RevisionName: "release-a", Resource: BootConfig{Metadata: Metadata{Name: other}, Spec: safetyArtifactB}})
	safetyStatus(t, response, err, http.StatusCreated)
	otherRevision, err := DecodeRevision(response)
	if err != nil || otherRevision.Metadata.SeriesName != other {
		t.Fatal("wrong-series fixture has invalid revision identity")
	}
	before := f.snapshot()
	for _, tc := range []struct {
		name string
		ref  ConfigReference
	}{
		{"missing-series", ConfigReference{Name: safetyName(t)}},
		{"missing-revision", ConfigReference{Name: f.assignment.Spec.ConfigRef.Name, RevisionName: "missing-revision"}},
		{"wrong-series", ConfigReference{Name: f.assignment.Spec.ConfigRef.Name, UID: otherRevision.Metadata.UID}},
	} {
		changed := f.assignment
		changed.Spec.ConfigRef = tc.ref
		response, err := f.client.ApplyAssignment(f.ctx, ConditionalAssignment{Resource: changed, IfMatch: before.assignETag})
		if err != nil || response.Status != http.StatusUnprocessableEntity {
			t.Fatalf("%s returned HTTP %d, error %v", tc.name, response.Status, err)
		}
		f.unchanged(before)
	}
	retired := f.retiredReference()
	afterFixture := f.snapshot()
	if !safetyJSONEqual(before.assignment, afterFixture.assignment) || before.assignETag != afterFixture.assignETag || !safetyJSONEqual(before.series, afterFixture.series) || before.seriesETag != afterFixture.seriesETag || !safetyCollectionEqual(before.collection, afterFixture.collection) {
		t.Fatal("retired fixture changed existing assignment or series")
	}
	f.artifact(safetyArtifactA)
	before = afterFixture
	changed := f.assignment
	changed.Spec.ConfigRef = ConfigReference{Name: retired.SeriesName, UID: retired.RevisionUID}
	response, err = f.client.ApplyAssignment(f.ctx, ConditionalAssignment{Resource: changed, IfMatch: before.assignETag})
	safetyStatus(t, response, err, http.StatusUnprocessableEntity)
	f.unchanged(before)
}

// Zero is the wire default for omitted priority. Compare that default while
// retaining exact selector, profile, and reference assertions.
func safetyEquivalentSpec(a, b AssignmentSpec) bool {
	zero := 0
	if a.Priority == nil {
		a.Priority = &zero
	}
	if b.Priority == nil {
		b.Priority = &zero
	}
	return reflect.DeepEqual(a, b)
}

func TestSafetyHelpersPriorityWireDefault(t *testing.T) {
	zero, one := 0, 1
	baseline := AssignmentSpec{ConfigRef: ConfigReference{Name: "os"}}
	explicit := baseline
	explicit.Priority = &zero
	if !safetyEquivalentSpec(baseline, explicit) {
		t.Fatal("omitted priority must equal zero")
	}
	explicit.Priority = &one
	if safetyEquivalentSpec(baseline, explicit) {
		t.Fatal("nonzero priority was ignored")
	}
	explicit = baseline
	explicit.ConfigRef.Name = "different"
	if safetyEquivalentSpec(baseline, explicit) {
		t.Fatal("reference difference was ignored")
	}
}
