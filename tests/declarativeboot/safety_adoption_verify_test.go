//go:build integration && declarative_boot

// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package declarativeboot

import (
	"net/http"
	"slices"
	"strings"
)

type safetyAdoptedSnapshot struct {
	series         []byte
	seriesETag     string
	assignment     []byte
	assignmentETag string
	revision       []byte
	uid            string
	revisionName   string
}

func safetyValidRevisionName(name string) bool {
	if len(name) == 0 || len(name) > 253 {
		return false
	}
	for _, label := range strings.Split(name, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, char := range label {
			if !((char >= 'a' && char <= 'z') || (char >= '0' && char <= '9') || char == '-') {
				return false
			}
		}
	}
	return true
}

func (f *safetyAdoptionFixture) adopted() safetyAdoptedSnapshot {
	t := f.h.t
	t.Helper()
	seriesResponse, err := f.h.admin.GetSeries(f.h.ctx, f.series)
	safetyStatus(t, seriesResponse, err, http.StatusOK)
	series, err := DecodeSeries(seriesResponse)
	if err != nil || series.Name != f.series || series.RevisionCount != 1 || series.DefaultRevisionUID != series.LatestRevisionUID {
		t.Fatal("adoption did not create exactly one default revision")
	}
	revisionsResponse, err := f.h.admin.ListRevisions(f.h.ctx, f.series)
	safetyStatus(t, revisionsResponse, err, http.StatusOK)
	revisions, err := DecodeRevisions(revisionsResponse)
	if err != nil || len(revisions) != 1 || revisions[0].Metadata.UID != series.DefaultRevisionUID {
		t.Fatal("adoption revision list is not unique")
	}
	revision, err := f.h.admin.GetRevision(f.h.ctx, RevisionID{Series: f.series, UID: series.DefaultRevisionUID})
	safetyStatus(t, revision, err, http.StatusOK)
	record, err := DecodeRevision(revision)
	want := f.h.fixtures.Legacy.Default
	if err != nil || record.Metadata.UID != revisions[0].Metadata.UID || record.Metadata.Digest != revisions[0].Metadata.Digest || record.Resource.Spec != (BootConfigSpec{Kernel: want.Kernel, Initrd: want.Initrd, Params: want.Params}) {
		t.Fatal("adopted revision differs from literal legacy content")
	}
	expected := "legacy-" + f.source.Metadata.UID
	if !safetyValidRevisionName(record.Metadata.RevisionName) || (safetyValidRevisionName(expected) && record.Metadata.RevisionName != expected) {
		t.Fatal("adopted revision name violates the legacy UID rule")
	}
	assignmentResponse, err := f.h.admin.GetAssignment(f.h.ctx, f.assignment)
	safetyStatus(t, assignmentResponse, err, http.StatusOK)
	assignment, err := DecodeAssignment(assignmentResponse)
	if err != nil || assignment.Spec.ConfigRef.Name != f.series || assignment.Spec.Selector == nil || !slices.Equal(assignment.Spec.Selector.Xnames, []string{f.node.Xname}) || len(assignment.Spec.Selector.MACs) != 0 || len(assignment.Spec.Selector.NIDs) != 0 || len(assignment.Spec.Selector.Groups) != 0 || assignment.Spec.Profile != f.source.Spec.Profile || (assignment.Spec.Priority == nil && f.source.Spec.Priority != 0) || (assignment.Spec.Priority != nil && *assignment.Spec.Priority != f.source.Spec.Priority) {
		t.Fatal("adopted assignment does not exactly translate the legacy selector")
	}
	resolved, err := f.h.admin.ResolveRevision(f.h.ctx, assignment.Spec.ConfigRef)
	safetyStatus(t, resolved, err, http.StatusOK)
	resolution, err := DecodeResolution(resolved)
	if err != nil || resolution.Revision.UID != record.Metadata.UID {
		t.Fatal("adopted assignment resolves to a different revision")
	}
	f.legacyArtifact()
	return safetyAdoptedSnapshot{series: seriesResponse.Body, seriesETag: safetyETag(t, seriesResponse), assignment: assignmentResponse.Body, assignmentETag: safetyETag(t, assignmentResponse), revision: revision.Body, uid: record.Metadata.UID, revisionName: record.Metadata.RevisionName}
}

func (f *safetyAdoptionFixture) sameAdoption(first, second safetyAdoptedSnapshot) {
	f.h.t.Helper()
	if first.uid != second.uid || first.seriesETag != second.seriesETag || first.assignmentETag != second.assignmentETag || !safetyJSONEqual(first.series, second.series) || !safetyJSONEqual(first.assignment, second.assignment) || !safetyJSONEqual(first.revision, second.revision) {
		f.h.t.Fatal("adoption replay changed identity, ETag or stored content")
	}
}
