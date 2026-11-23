//go:build integration && declarative_boot

// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package declarativeboot

import (
	"context"
	"net/http"
	"time"
)

func (h *safetyHarness) createPinnedA(series, uid string) (string, string, FixtureNode) {
	h.t.Helper()
	node := h.node("pinned")
	name := safetyName(h.t)
	h.t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		current, err := h.admin.GetAssignment(ctx, name)
		if err != nil {
			h.t.Errorf("pin cleanup read: %v", err)
			return
		}
		if current.Status == http.StatusNotFound {
			return
		}
		if current.Status != http.StatusOK || !safetyStrongETag(current.Header.Get("ETag")) {
			h.t.Errorf("pin cleanup read HTTP %d or invalid ETag", current.Status)
			return
		}
		deleted, err := h.admin.DeleteAssignment(ctx, ConditionalName{Name: name, IfMatch: current.Header.Get("ETag")})
		if err != nil || (deleted.Status != http.StatusOK && deleted.Status != http.StatusNoContent) {
			h.t.Errorf("pin cleanup delete HTTP %d: %v", deleted.Status, err)
		}
	})
	priority := 100
	assignment := BootAssignment{Metadata: Metadata{Name: name}, Spec: AssignmentSpec{Selector: &Selector{Xnames: []string{node.Xname}}, Priority: &priority, ConfigRef: ConfigReference{Name: series, UID: uid}}}
	response, err := h.admin.ApplyAssignment(h.ctx, ConditionalAssignment{Resource: assignment, IfNoneMatch: "*"})
	safetyStatus(h.t, response, err, http.StatusCreated)
	created, err := DecodeAssignment(response)
	if err != nil || created.Spec.ConfigRef.UID != uid {
		h.t.Fatal("A pin creation returned wrong reference")
	}
	safetyETag(h.t, response)
	return name, created.Metadata.UID, node
}

func (f *safetyFixture) assertPinnedRevisions(aUID, bUID string) {
	f.t.Helper()
	series := f.assignment.Spec.ConfigRef.Name
	state := f.seriesInfo()
	if state.DefaultRevisionUID != bUID || state.LatestRevisionUID != bUID || state.RevisionCount != 2 {
		f.t.Fatal("retirement changed default/latest pointers")
	}
	for _, expected := range []struct {
		uid  string
		spec BootConfigSpec
	}{{aUID, safetyArtifactA}, {bUID, safetyArtifactB}} {
		response, err := f.client.GetRevision(f.ctx, RevisionID{Series: series, UID: expected.uid})
		safetyStatus(f.t, response, err, http.StatusOK)
		record, err := DecodeRevision(response)
		if err != nil || record.Metadata.UID != expected.uid || record.Metadata.RetiredAt != nil || record.Resource.Spec != expected.spec {
			f.t.Fatal("retirement changed a captured immutable revision")
		}
	}
}
