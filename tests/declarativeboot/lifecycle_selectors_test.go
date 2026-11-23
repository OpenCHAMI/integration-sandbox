//go:build integration && declarative_boot

// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package declarativeboot

import (
	"net/http"
	"strings"
	"testing"
)

func lifecycleStrongest(t *testing.T) {
	c := lifecycleNew(t)
	nodes := c.nodes(2)
	n := nodes[0]
	red := c.group("red", n)
	blue := c.group("blue", n)
	a := c.createA()
	b := c.ensureB()
	high, low := 100, 0
	refA := ConfigReference{Name: c.series, RevisionName: "release-a"}
	refB := ConfigReference{Name: c.series, RevisionName: "release-b"}
	c.createAssignment("catchall", AssignmentSpec{Priority: &high, ConfigRef: refB})
	c.expectArtifact(n, "B")
	group := c.createAssignment("groups", AssignmentSpec{Selector: &Selector{Xnames: []string{nodes[1].xname}, Groups: []string{red, blue}}, Priority: &high, ConfigRef: refA})
	c.createAssignment("not-exact", AssignmentSpec{Selector: &Selector{Xnames: []string{nodes[1].xname}, Groups: []string{strings.ToUpper(red), red + "*"}}, Priority: &high, ConfigRef: refB})
	c.expectArtifact(n, "A")
	wildcard, response := c.tryAssignment("wildcard", AssignmentSpec{Selector: &Selector{Xnames: []string{n.xname + "*"}}, Priority: &high, ConfigRef: refB})
	if wildcard == nil {
		c.status(response, nil, http.StatusBadRequest, http.StatusUnprocessableEntity)
		read, err := c.boot.GetAssignment(c.ctx, c.series+"-wildcard")
		c.status(read, err, http.StatusNotFound)
	} else {
		c.expectArtifact(n, "A")
		c.deleteAssignment(wildcard)
	}
	c.pointers(a.Metadata.UID, b.Metadata.UID, 2)
	c.expectArtifact(n, "A")
	xname := c.createAssignment("xname", AssignmentSpec{Selector: &Selector{Xnames: []string{nodes[1].xname, n.xname}}, Priority: &low, ConfigRef: refB})
	c.expectArtifact(n, "B")
	nid := c.createAssignment("nid", AssignmentSpec{Selector: &Selector{NIDs: []int32{n.nid}}, Priority: &low, ConfigRef: refA})
	c.expectArtifact(n, "A")
	normalized := strings.ToUpper(strings.ReplaceAll(n.mac, ":", "-"))
	mac := c.createAssignment("mac", AssignmentSpec{Selector: &Selector{MACs: []string{normalized}}, Priority: &low, ConfigRef: refB})
	c.expectArtifact(n, "B")
	c.deleteAssignment(mac)
	c.expectArtifact(n, "A")
	c.deleteAssignment(nid)
	c.expectArtifact(n, "B")
	c.deleteAssignment(xname)
	c.expectArtifact(n, "A")
	c.deleteAssignment(group)
	c.expectArtifact(n, "B")
}

func lifecyclePriorityName(t *testing.T) {
	c := lifecycleNew(t)
	n := c.nodes(1)[0]
	c.group("tie", n)
	c.createA()
	c.ensureB()
	selector := &Selector{Xnames: []string{n.xname}}
	low, high := 10, 90
	aSpec := AssignmentSpec{Selector: selector, Priority: &low, ConfigRef: ConfigReference{Name: c.series, RevisionName: "release-a"}}
	bSpec := AssignmentSpec{Selector: selector, Priority: &high, ConfigRef: ConfigReference{Name: c.series, RevisionName: "release-b"}}
	z := c.createAssignment("z", bSpec)
	a := c.createAssignment("a", aSpec)
	c.expectArtifact(n, "B")
	bSpec.Priority = &low
	c.updateAssignment(z, bSpec)
	c.expectArtifact(n, "A")
	c.deleteAssignment(a)
	c.deleteAssignment(z)
	c.createAssignment("a", aSpec)
	c.createAssignment("z", bSpec)
	c.expectArtifact(n, "A")
}
