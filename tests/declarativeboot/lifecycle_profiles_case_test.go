//go:build integration && declarative_boot

// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package declarativeboot

import (
	"net/url"
	"reflect"
	"testing"
)

func lifecycleProfileCase(t *testing.T) {
	c := lifecycleNew(t)
	nodes := c.nodes(2)
	n := nodes[0]
	profileGroup := c.group("profile", n)
	c.seedLegacyNode(n, []string{profileGroup})
	legacy := c.legacy(n)
	groupLegacy, macLegacy := lifecycleAdditiveFixtures(c, nodes[1])
	unknown := url.Values{"host": {n.xname}, "profile": {"not-configured"}}
	c.legacyArtifact("/bootscript", unknown)
	c.legacyArtifact("/boot/v1/bootscript", unknown)
	c.expectLegacyAdditive(nodes[1])
	c.createA()
	c.ensureB()
	selector := &Selector{Xnames: []string{n.xname}}
	c.createAssignment("default", AssignmentSpec{Selector: selector, ConfigRef: ConfigReference{Name: c.series, RevisionName: "release-a"}})
	c.createAssignment("compute", AssignmentSpec{Selector: selector, Profile: "compute", ConfigRef: ConfigReference{Name: c.series, RevisionName: "release-b"}})
	c.expectQuery(url.Values{"host": {n.xname}}, "A")
	c.expectQuery(url.Values{"host": {n.xname}, "profile": {"default"}}, "A")
	c.expectQuery(url.Values{"host": {n.xname}, "profile": {"compute"}}, "B")
	c.legacyArtifact("/bootscript", unknown)
	c.legacyArtifact("/boot/v1/bootscript", unknown)
	c.expectLegacyAdditive(nodes[1])
	if !reflect.DeepEqual(legacy, c.legacyRead(legacy.Metadata.UID)) {
		t.Fatal("profile fallback changed the legacy configuration")
	}
	if !reflect.DeepEqual(groupLegacy, c.legacyRead(groupLegacy.Metadata.UID)) || !reflect.DeepEqual(macLegacy, c.legacyRead(macLegacy.Metadata.UID)) {
		t.Fatal("new assignments changed the additive legacy configurations")
	}
}
