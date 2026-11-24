//go:build integration && declarative_boot

// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package declarativeboot

import (
	"net/http"
	"testing"
)

func lifecycleRollout(t *testing.T) {
	c := lifecycleNew(t)
	nodes := c.nodes(3)
	red := c.group("red", nodes...)
	blue := c.group("blue", nodes[0], nodes[1])
	a := c.createA()
	c.pointers(a.Metadata.UID, a.Metadata.UID, 1)
	priority := 100
	c.createAssignment("group", AssignmentSpec{Selector: &Selector{Groups: []string{red, blue}}, Priority: &priority, ConfigRef: ConfigReference{Name: c.series, Selector: "default"}})
	c.createAssignment("pinned", AssignmentSpec{Selector: &Selector{Xnames: []string{nodes[2].xname}}, ConfigRef: ConfigReference{Name: c.series, RevisionName: "release-a"}})
	c.expectArtifact(nodes[0], "A")
	c.expectArtifact(nodes[1], "A")
	c.expectArtifact(nodes[2], "A")
	b := c.ensureB()
	c.pointers(a.Metadata.UID, b.Metadata.UID, 2)
	c.expectArtifact(nodes[1], "A")
	c.expectArtifact(nodes[2], "A")
	response, err := c.boot.EnsureRevision(c.ctx, c.series, c.config("B"))
	replay := c.revision(c.status(response, err, http.StatusOK))
	if replay.Metadata.UID != b.Metadata.UID || replay.Metadata.Digest != b.Metadata.Digest {
		t.Fatal("B replay changed identity")
	}
	c.pointers(a.Metadata.UID, b.Metadata.UID, 2)
	c.expectArtifact(nodes[2], "A")
	c.createAssignment("canary", AssignmentSpec{Selector: &Selector{MACs: []string{nodes[0].mac}}, ConfigRef: ConfigReference{Name: c.series, RevisionName: "release-b"}})
	c.expectArtifact(nodes[0], "B")
	c.expectArtifact(nodes[1], "A")
	c.expectArtifact(nodes[2], "A")
	_, stale := c.seriesState()
	c.promote(b.Metadata.UID)
	c.pointers(b.Metadata.UID, b.Metadata.UID, 2)
	c.expectArtifact(nodes[0], "B")
	c.expectArtifact(nodes[1], "B")
	c.expectArtifact(nodes[2], "A")
	response, err = c.boot.PromoteDefault(c.ctx, Promotion{Series: c.series, RevisionUID: a.Metadata.UID, IfMatch: stale})
	c.status(response, err, http.StatusPreconditionFailed)
	c.pointers(b.Metadata.UID, b.Metadata.UID, 2)
	c.expectArtifact(nodes[1], "B")
	c.expectArtifact(nodes[2], "A")
	c.promote(a.Metadata.UID)
	c.pointers(a.Metadata.UID, b.Metadata.UID, 2)
	c.expectArtifact(nodes[0], "B")
	c.expectArtifact(nodes[1], "A")
	c.expectArtifact(nodes[2], "A")
}

func lifecycleReferences(t *testing.T) {
	c := lifecycleNew(t)
	nodes := c.nodes(5)
	c.group("references", nodes...)
	a := c.createA()
	refs := []ConfigReference{{Name: c.series}, {Name: c.series, Selector: "default"}, {Name: c.series, Selector: "latest"}, {Name: c.series, RevisionName: "release-a"}, {Name: c.series, UID: a.Metadata.UID}}
	for i, ref := range refs {
		c.createAssignment("ref-"+string(rune('a'+i)), AssignmentSpec{Selector: &Selector{Xnames: []string{nodes[i].xname}}, ConfigRef: ref})
		c.expectArtifact(nodes[i], "A")
	}
	b := c.ensureB()
	c.pointers(a.Metadata.UID, b.Metadata.UID, 2)
	for i, ref := range refs {
		wantUID, wantArtifact := a.Metadata.UID, "A"
		if i == 2 {
			wantUID, wantArtifact = b.Metadata.UID, "B"
		}
		response, err := c.boot.ResolveRevision(c.ctx, ref)
		resolved, err := DecodeResolution(c.status(response, err, http.StatusOK))
		if err != nil || resolved.Revision.UID != wantUID {
			t.Fatalf("reference %d resolved incorrectly", i)
		}
		c.expectArtifact(nodes[i], wantArtifact)
	}
	c.promote(b.Metadata.UID)
	c.pointers(b.Metadata.UID, b.Metadata.UID, 2)
	for i, key := range []string{"B", "B", "B", "A", "A"} {
		c.expectArtifact(nodes[i], key)
	}
}
