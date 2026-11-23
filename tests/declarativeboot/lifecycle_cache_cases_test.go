//go:build integration && declarative_boot

// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package declarativeboot

import (
	"net/url"
	"testing"
)

func lifecycleCacheWritesCase(t *testing.T) {
	c := lifecycleNew(t)
	nodes := c.nodes(2)
	c.group("cache", nodes...)
	a, b := c.createA(), c.ensureB()
	spec := AssignmentSpec{Selector: &Selector{Xnames: []string{nodes[0].xname}}, ConfigRef: ConfigReference{Name: c.series}}
	assignment := c.createAssignment("default", spec)
	c.createAssignment("pinned", AssignmentSpec{Selector: &Selector{Xnames: []string{nodes[1].xname}}, ConfigRef: ConfigReference{Name: c.series, UID: a.Metadata.UID}})
	query := url.Values{"host": {nodes[0].xname}}
	c.warm(query, "A")
	c.expectArtifact(nodes[1], "A")
	c.promote(b.Metadata.UID)
	c.expectQuery(query, "B")
	c.expectArtifact(nodes[1], "A")
	spec.ConfigRef = ConfigReference{Name: c.series, UID: a.Metadata.UID}
	c.updateAssignment(assignment, spec)
	c.expectQuery(query, "A")
	c.expectArtifact(nodes[1], "A")
	spec.ConfigRef = ConfigReference{Name: c.series, RevisionName: "release-b"}
	c.updateAssignment(assignment, spec)
	c.expectQuery(query, "B")
	c.expectArtifact(nodes[1], "A")
}

func lifecycleFingerprintCase(t *testing.T) {
	c := lifecycleNew(t)
	nodes := c.nodes(3)
	n := nodes[0]
	red, blue := c.group("red", n), c.group("blue")
	a, _ := c.createA(), c.ensureB()
	low, high := 10, 90
	refA := ConfigReference{Name: c.series, RevisionName: "release-a"}
	refB := ConfigReference{Name: c.series, RevisionName: "release-b"}
	c.createAssignment("red", AssignmentSpec{Selector: &Selector{Groups: []string{red}}, Priority: &low, ConfigRef: refA})
	c.createAssignment("blue", AssignmentSpec{Selector: &Selector{Groups: []string{blue}}, Priority: &high, ConfigRef: refB})
	c.createAssignment("pinned", AssignmentSpec{Selector: &Selector{Xnames: []string{nodes[1].xname}}, ConfigRef: ConfigReference{Name: c.series, UID: a.Metadata.UID}})
	c.createAssignment("debug", AssignmentSpec{Selector: &Selector{Xnames: []string{n.xname}}, Profile: "debug", ConfigRef: refB})
	identitySpec := AssignmentSpec{Selector: &Selector{Xnames: []string{nodes[2].xname}}, ConfigRef: refB}
	identity := c.createAssignment("identity", identitySpec)
	query := url.Values{"host": {n.xname}}
	c.warm(query, "A")
	c.expectQuery(url.Values{"host": {n.xname}, "profile": {"debug"}}, "B")
	c.expectQuery(query, "A")
	c.expectArtifact(nodes[1], "A")
	macQuery := url.Values{"mac": {n.mac}}
	c.warm(macQuery, "A")
	c.restoreInterface(n)
	changedAt := c.moveInterface(n, nodes[2].xname)
	c.await(macQuery, lifecycleAwait{key: "B", requiredXname: nodes[2].xname, changedAt: changedAt})
	changedAt = c.moveInterface(n, n.xname)
	c.await(macQuery, lifecycleAwait{key: "A", requiredXname: n.xname, changedAt: changedAt})
	identitySpec.ConfigRef = refA
	c.updateAssignment(identity, identitySpec)
	identityQuery := url.Values{"host": {nodes[2].xname}}
	c.warm(identityQuery, "A")
	oldUID := identity.resource.Metadata.UID
	c.deleteAssignment(identity)
	identitySpec.ConfigRef = refB
	recreated := c.createAssignment("identity", identitySpec)
	if recreated.resource.Metadata.UID == oldUID {
		t.Fatal("assignment recreation reused the original UID")
	}
	c.expectQuery(identityQuery, "B")
	c.expectArtifact(nodes[1], "A")
	changedAt = c.setGroup(blue, []string{n.xname})
	c.await(query, lifecycleAwait{key: "B", changedAt: changedAt})
	c.expectArtifact(nodes[1], "A")
	changedAt = c.setGroup(blue, []string{})
	c.await(query, lifecycleAwait{key: "A", changedAt: changedAt})
	c.expectArtifact(nodes[1], "A")
	newNID := n.nid + 1000000000
	c.createAssignment("nid", AssignmentSpec{Selector: &Selector{NIDs: []int32{newNID}}, ConfigRef: refB})
	c.expectQuery(query, "A")
	changedAt = c.setNID(n, newNID)
	c.await(query, lifecycleAwait{key: "B", changedAt: changedAt})
	c.expectArtifact(nodes[1], "A")
	changed, changedAt := c.changeMAC(n)
	c.await(query, lifecycleAwait{key: "B", requiredMAC: changed.mac, changedAt: changedAt})
	c.expectArtifact(nodes[1], "A")
}
