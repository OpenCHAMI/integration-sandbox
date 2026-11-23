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

func lifecycleCoherenceCase(t *testing.T) {
	c := lifecycleNew(t)
	nodes := c.nodes(2)
	c.group("race", nodes...)
	a, b := c.createA(), c.ensureB()
	spec := AssignmentSpec{Selector: &Selector{Xnames: []string{nodes[0].xname}}, ConfigRef: ConfigReference{Name: c.series}}
	assignment := c.createAssignment("default", spec)
	c.createAssignment("pinned", AssignmentSpec{Selector: &Selector{Xnames: []string{nodes[1].xname}}, ConfigRef: ConfigReference{Name: c.series, UID: a.Metadata.UID}})
	values := url.Values{"host": {nodes[0].xname}}
	c.warm(values, "A")
	c.expectArtifact(nodes[1], "A")
	ctx, cancel := context.WithTimeout(c.ctx, 90*time.Second)
	defer cancel()
	readers := lifecycleStartReaders(ctx, c, values, url.Values{"host": {nodes[1].xname}})
	defer readers.stop(t)
	pointerOverlap, assignmentOverlap := false, false
	for round := range 12 {
		uid, key := b.Metadata.UID, "B"
		if round%2 == 1 {
			uid, key = a.Metadata.UID, "A"
		}
		_, etag := c.seriesState()
		readers.beforeWrite(t)
		writerCtx, window := readers.writerContext(ctx)
		r, err := c.boot.PromoteDefault(writerCtx, Promotion{Series: c.series, RevisionUID: uid, IfMatch: etag})
		overlapped := window.finish(err)
		c.status(r, err, http.StatusOK)
		pointerOverlap = pointerOverlap || overlapped
		c.expectQuery(values, key)
		c.expectArtifact(nodes[1], "A")
	}
	for round := range 12 {
		ref, key := ConfigReference{Name: c.series, UID: b.Metadata.UID}, "B"
		if round%2 == 1 {
			ref, key = ConfigReference{Name: c.series, UID: a.Metadata.UID}, "A"
		}
		spec.ConfigRef = ref
		resource := assignment.resource
		resource.Spec = spec
		readers.beforeWrite(t)
		writerCtx, window := readers.writerContext(ctx)
		response, err := c.boot.ApplyAssignment(writerCtx, ConditionalAssignment{Resource: resource, IfMatch: assignment.etag})
		overlapped := window.finish(err)
		response = c.status(response, err, http.StatusOK)
		stored, err := DecodeAssignment(response)
		if err != nil || stored.Metadata.UID != assignment.resource.Metadata.UID {
			t.Fatal("assignment update changed identity")
		}
		assignment.resource, assignment.etag = resource, response.Header.Get("ETag")
		if !lifecycleStrongETag(assignment.etag) {
			t.Fatal("assignment update lacks strong ETag")
		}
		assignmentOverlap = assignmentOverlap || overlapped
		c.expectQuery(values, key)
		c.expectArtifact(nodes[1], "A")
	}
	if !pointerOverlap || !assignmentOverlap {
		t.Fatal("readers did not make observable progress during both write types")
	}
}
