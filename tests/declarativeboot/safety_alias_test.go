//go:build integration && declarative_boot

// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package declarativeboot

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"
)

func safetyAliasPaths(path string) []string {
	base, query, found := strings.Cut(path, "?")
	suffix := ""
	if found {
		suffix = "?" + query
	}
	encoded := "/" + fmt.Sprintf("%%%02X", base[1]) + base[2:]
	return []string{base + "/" + suffix, encoded + suffix}
}

func safetyStatusAlias(t *testing.T) {
	f := safetyFixtureNew(t)
	b := f.ensureB(f.client)
	reader := f.role([]string{"read:boot"})
	before := f.snapshot()
	for _, route := range f.securityRoutes(b.Metadata.UID) {
		for _, path := range safetyAliasPaths(route.request.Path) {
			request := route.request
			request.Path = path
			response, err := reader.Raw(f.ctx, request)
			want := http.StatusForbidden
			if route.scope == "read" {
				want = http.StatusOK
			}
			safetyRequireResponse(t, route, response, err, want)
			f.unchanged(before)
		}
	}
	assignment, _, _ := safetyReadAssignment(t, f.ctx, f.client, f.assignment.Metadata.Name)
	series := f.assignment.Spec.ConfigRef.Name
	seriesResponse, err := f.client.GetSeries(f.ctx, series)
	safetyStatus(t, seriesResponse, err, http.StatusOK)
	prior, err := DecodeSeries(seriesResponse)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		"/bootconfigs/" + url.PathEscape(series) + "/status",
		"/bootassignments/" + url.PathEscape(assignment.Metadata.UID) + "/status",
	} {
		for _, method := range []string{http.MethodPut, http.MethodPatch} {
			body, err := json.Marshal(struct {
				Default string         `json:"defaultRevisionUid"`
				Latest  string         `json:"latestRevisionUid"`
				Spec    AssignmentSpec `json:"spec"`
			}{Default: b.Metadata.UID, Latest: b.Metadata.UID, Spec: AssignmentSpec{ConfigRef: ConfigReference{Name: series, Selector: "latest"}}})
			if err != nil {
				t.Fatal(err)
			}
			tag := before.seriesETag
			if strings.HasPrefix(path, "/bootassignments/") {
				tag = before.assignETag
			}
			response := f.raw(method, path, tag, body)
			if response.Status != http.StatusOK && response.Status != http.StatusBadRequest && response.Status != http.StatusUnprocessableEntity {
				t.Fatalf("status mutation HTTP %d", response.Status)
			}
			current, _, _ := safetyReadAssignment(t, f.ctx, f.client, f.assignment.Metadata.Name)
			if current.Metadata.UID != assignment.Metadata.UID || !reflect.DeepEqual(current.Spec, assignment.Spec) {
				t.Fatal("status mutated assignment identity or spec")
			}
			seriesResponse, err = f.client.GetSeries(f.ctx, series)
			safetyStatus(t, seriesResponse, err, http.StatusOK)
			currentSeries, err := DecodeSeries(seriesResponse)
			if err != nil || currentSeries.DefaultRevisionUID != prior.DefaultRevisionUID || currentSeries.LatestRevisionUID != prior.LatestRevisionUID || currentSeries.RevisionCount != prior.RevisionCount {
				t.Fatal("status mutated revision pointers")
			}
			f.artifact(safetyArtifactA)
			before = f.snapshot()
		}
	}
}

func safetyErrorCode(body []byte) string {
	var value struct {
		Code  string          `json:"code"`
		Error json.RawMessage `json:"error"`
	}
	if json.Unmarshal(body, &value) != nil {
		return ""
	}
	if value.Code != "" {
		return value.Code
	}
	var nested struct {
		Code string `json:"code"`
	}
	if json.Unmarshal(value.Error, &nested) == nil && nested.Code != "" {
		return nested.Code
	}
	var text string
	if json.Unmarshal(value.Error, &text) == nil {
		return text
	}
	return ""
}

func safetyRetirementAliases(t *testing.T) {
	f := safetyFixtureNew(t)
	series := f.assignment.Spec.ConfigRef.Name
	response, err := f.client.GetSeries(f.ctx, series)
	safetyStatus(t, response, err, http.StatusOK)
	state, err := DecodeSeries(response)
	if err != nil {
		t.Fatal(err)
	}
	aUID := state.DefaultRevisionUID
	_, assignmentETag, _ := safetyReadAssignment(t, f.ctx, f.client, f.assignment.Metadata.Name)
	pin := f.assignment
	pin.Spec.ConfigRef.UID = aUID
	response, err = f.client.ApplyAssignment(f.ctx, ConditionalAssignment{Resource: pin, IfMatch: assignmentETag})
	safetyStatus(t, response, err, http.StatusOK)
	pinned, pinETag, _ := safetyReadAssignment(t, f.ctx, f.client, pin.Metadata.Name)
	if pinned.Spec.ConfigRef.UID != aUID {
		t.Fatal("A pin was not retained")
	}
	b := f.ensureB(f.client)
	seriesResponse, err := f.client.GetSeries(f.ctx, series)
	safetyStatus(t, seriesResponse, err, http.StatusOK)
	response, err = f.client.PromoteDefault(f.ctx, Promotion{Series: series, RevisionUID: b.Metadata.UID, IfMatch: safetyETag(t, seriesResponse)})
	safetyStatus(t, response, err, http.StatusOK)
	seriesState := f.seriesInfo()
	if seriesState.DefaultRevisionUID != b.Metadata.UID || seriesState.LatestRevisionUID != b.Metadata.UID {
		t.Fatal("B was not promoted as expected")
	}
	defaultName := safetyName(t)
	f.cleanupName(defaultName)
	priority := 100
	defaultAssignment := BootAssignment{Metadata: Metadata{Name: defaultName}, Spec: AssignmentSpec{Selector: &Selector{Xnames: []string{"x0c0s2b0n0"}}, Priority: &priority, ConfigRef: ConfigReference{Name: series}}}
	response, err = f.client.ApplyAssignment(f.ctx, ConditionalAssignment{Resource: defaultAssignment, IfNoneMatch: "*"})
	safetyStatus(t, response, err, http.StatusCreated)
	f.nodeArtifact("x0c0s2b0n0", safetyArtifactB)
	before := f.snapshot()
	base := "/bootconfigs/" + url.PathEscape(series) + "/revisions/by-uid/" + url.PathEscape(aUID)
	for _, path := range append([]string{base}, safetyAliasPaths(base)...) {
		response = f.raw(http.MethodDelete, path, before.seriesETag, nil)
		safetyStatus(t, response, nil, http.StatusConflict)
		if safetyErrorCode(response.Body) != "boot_config_retirement_disabled" {
			t.Fatal("retirement denial missing contract code")
		}
		f.unchanged(before)
		current, currentETag, _ := safetyReadAssignment(t, f.ctx, f.client, pin.Metadata.Name)
		if current.Metadata.UID != pinned.Metadata.UID || current.Spec.ConfigRef.UID != aUID || currentETag != pinETag {
			t.Fatal("retirement changed A pin")
		}
		f.nodeArtifact("x0c0s2b0n0", safetyArtifactB)
		f.assertPinnedRevisions(aUID, b.Metadata.UID)
	}
	for _, req := range []Request{
		{Method: http.MethodDelete, Path: "/bootconfigs/" + url.PathEscape(series)},
		{Method: http.MethodPut, Path: "/bootconfigs/" + url.PathEscape(aUID), Body: f.configFor(series, safetyArtifactB)},
		{Method: http.MethodPatch, Path: "/bootconfigs/" + url.PathEscape(aUID), Body: []byte(`{"kernel":"/changed"}`)},
	} {
		response, err := f.client.Raw(f.ctx, req)
		safetyStatus(t, response, err, http.StatusMethodNotAllowed)
		f.unchanged(before)
		current, currentETag, _ := safetyReadAssignment(t, f.ctx, f.client, pin.Metadata.Name)
		if current.Metadata.UID != pinned.Metadata.UID || current.Spec.ConfigRef.UID != aUID || currentETag != pinETag {
			t.Fatal("immutable mutation changed A pin")
		}
		f.nodeArtifact("x0c0s2b0n0", safetyArtifactB)
		f.assertPinnedRevisions(aUID, b.Metadata.UID)
	}
}
