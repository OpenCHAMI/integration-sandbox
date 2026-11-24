//go:build integration && declarative_boot

// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package declarativeboot

import (
	"net/url"
	"testing"
)

func lifecycleMembershipCase(t *testing.T) {
	c := lifecycleNew(t)
	nodes := c.nodes(2)
	red, blue := c.group("red", nodes[0]), c.group("blue")
	a, _ := c.createA(), c.ensureB()
	priority := 90
	c.createAssignment("fallback", AssignmentSpec{ConfigRef: ConfigReference{Name: c.series, RevisionName: "release-b"}})
	c.createAssignment("red", AssignmentSpec{Selector: &Selector{Groups: []string{red}}, ConfigRef: ConfigReference{Name: c.series, RevisionName: "release-a"}})
	c.createAssignment("blue", AssignmentSpec{Selector: &Selector{Groups: []string{blue}}, Priority: &priority, ConfigRef: ConfigReference{Name: c.series, RevisionName: "release-b"}})
	c.createAssignment("pinned", AssignmentSpec{Selector: &Selector{Xnames: []string{nodes[1].xname}}, ConfigRef: ConfigReference{Name: c.series, UID: a.Metadata.UID}})
	query := url.Values{"host": {nodes[0].xname}}
	c.requireMembershipTTL()
	c.warm(query, "A")
	c.expectArtifact(nodes[1], "A")
	changedAt := c.setGroup(red, []string{})
	c.await(query, lifecycleAwait{key: "B", changedAt: changedAt})
	c.expectArtifact(nodes[1], "A")
	changedAt = c.setGroup(red, []string{nodes[0].xname})
	c.await(query, lifecycleAwait{key: "A", changedAt: changedAt})
	c.expectArtifact(nodes[1], "A")
	changedAt = c.setGroup(blue, []string{nodes[0].xname})
	c.await(query, lifecycleAwait{key: "B", changedAt: changedAt})
	c.expectArtifact(nodes[1], "A")
	changedAt = c.setGroup(blue, []string{})
	c.await(query, lifecycleAwait{key: "A", changedAt: changedAt})
	c.expectArtifact(nodes[1], "A")
}
