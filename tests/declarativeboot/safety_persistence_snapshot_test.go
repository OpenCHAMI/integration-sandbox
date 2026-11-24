//go:build integration && declarative_boot

// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package declarativeboot

import (
	"net/http"
	"testing"
	"time"
)

type safetyPersistenceSnapshot struct {
	version        []byte
	series         []byte
	seriesETag     string
	assignment     []byte
	assignmentETag string
	pin            []byte
	pinETag        string
	revisions      map[string][]byte
}

func (h *safetyHarness) persistenceSnapshot(base string, state safetyState) safetyPersistenceSnapshot {
	h.t.Helper()
	client, err := NewHTTPClient(base, 10*time.Second)
	if err != nil {
		h.t.Fatal("invalid recovery endpoint")
	}
	boot := BootClient{HTTP: client, Bearer: h.admin.Bearer}
	version, err := client.Do(h.ctx, Request{Method: http.MethodGet, Path: "/service/version"})
	safetyStatus(h.t, version, err, http.StatusOK)
	service, err := DecodeJSON[struct {
		Name    string `json:"service_name"`
		Version string `json:"service_version"`
	}](version)
	if err != nil || service.Name == "" || service.Version == "" {
		h.t.Fatal("recovery version endpoint is incomplete")
	}
	series, err := boot.GetSeries(h.ctx, state.Series)
	safetyStatus(h.t, series, err, http.StatusOK)
	status, err := DecodeSeries(series)
	if err != nil || status.Name != state.Series || status.DefaultRevisionUID != state.DefaultUID || status.LatestRevisionUID != state.LatestUID || status.RevisionCount != 2 {
		h.t.Fatal("recovery series identity or pointers changed")
	}
	assignment, err := boot.GetAssignment(h.ctx, state.AssignmentName)
	safetyStatus(h.t, assignment, err, http.StatusOK)
	value, err := DecodeAssignment(assignment)
	if err != nil || value.Metadata.UID != state.AssignmentUID || value.Spec.ConfigRef.Name != state.Series {
		h.t.Fatal("recovery assignment identity or reference changed")
	}
	pin, err := boot.GetAssignment(h.ctx, state.PinName)
	safetyStatus(h.t, pin, err, http.StatusOK)
	pinned, err := DecodeAssignment(pin)
	if err != nil || pinned.Metadata.UID != state.PinUID || pinned.Spec.ConfigRef.Name != state.Series || pinned.Spec.ConfigRef.UID != state.InitialUID {
		h.t.Fatal("recovery changed explicit A pin")
	}
	pinScript := h.bootscript(base, state.PinNode)
	safetyStatus(h.t, pinScript, nil, http.StatusOK)
	if !safetyArtifactTuple(string(pinScript.Body), safetyArtifactA) {
		h.t.Fatal("recovery pin did not deliver A")
	}
	list, err := boot.ListRevisions(h.ctx, state.Series)
	safetyStatus(h.t, list, err, http.StatusOK)
	revisions, err := DecodeRevisions(list)
	if err != nil || len(revisions) != 2 {
		h.t.Fatal("recovery revision count changed")
	}
	records := make(map[string][]byte, len(revisions))
	for _, item := range revisions {
		if item.Metadata.UID != state.InitialUID && item.Metadata.UID != state.DefaultUID {
			h.t.Fatal("unexpected recovery revision UID")
		}
		if _, exists := records[item.Metadata.UID]; exists {
			h.t.Fatal("duplicate recovery revision UID")
		}
		response, getErr := boot.GetRevision(h.ctx, RevisionID{Series: state.Series, UID: item.Metadata.UID})
		safetyStatus(h.t, response, getErr, http.StatusOK)
		got, decodeErr := DecodeRevision(response)
		if decodeErr != nil || got.Metadata.UID != item.Metadata.UID || got.Metadata.Digest != item.Metadata.Digest {
			h.t.Fatal("recovery revision lookup changed identity or digest")
		}
		records[item.Metadata.UID] = response.Body
	}
	script := h.bootscript(base, state.Node)
	safetyStatus(h.t, script, nil, http.StatusOK)
	if !safetyArtifactTuple(string(script.Body), safetyArtifactB) {
		h.t.Fatal("recovery boot script differs from the B artifact tuple")
	}
	return safetyPersistenceSnapshot{version: version.Body, series: series.Body, seriesETag: safetyETag(h.t, series), assignment: assignment.Body, assignmentETag: safetyETag(h.t, assignment), pin: pin.Body, pinETag: safetyETag(h.t, pin), revisions: records}
}

func safetyComparePersistence(t *testing.T, before, after safetyPersistenceSnapshot) {
	t.Helper()
	if !safetyJSONEqual(before.version, after.version) || before.seriesETag != after.seriesETag || before.assignmentETag != after.assignmentETag || before.pinETag != after.pinETag || !safetyJSONEqual(before.pin, after.pin) || !safetyJSONEqual(before.series, after.series) || !safetyJSONEqual(before.assignment, after.assignment) || len(before.revisions) != len(after.revisions) {
		t.Fatal("recovery changed series, assignment or strong ETag")
	}
	for uid, record := range before.revisions {
		other, exists := after.revisions[uid]
		if !exists || !safetyJSONEqual(record, other) {
			t.Fatal("recovery changed an immutable revision")
		}
	}
}
