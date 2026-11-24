//go:build integration && declarative_boot

// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package declarativeboot

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

func lifecycleAdditiveFixtures(c *lifecycleCase, n lifecycleNode) (lifecycleLegacyResource, lifecycleLegacyResource) {
	c.t.Helper()
	groups := make([]string, 0, 5)
	for i := range 5 {
		groups = append(groups, c.group(fmt.Sprintf("additive-%d", i), n))
	}
	c.seedLegacyNode(n, groups)
	a, b := c.fixtures.Expected.Artifacts["A"], c.fixtures.Expected.Artifacts["B"]
	group := c.createLegacy("group-score", lifecycleLegacySpec{Groups: groups, Kernel: a.Kernel, Initrd: a.Initrd, Params: strings.Join(a.Params, " ")})
	mac := c.createLegacy("mac-score", lifecycleLegacySpec{MACs: []string{n.mac}, Priority: 100, Kernel: b.Kernel, Initrd: b.Initrd, Params: strings.Join(b.Params, " ")})
	return group, mac
}

func (c *lifecycleCase) expectLegacyAdditive(n lifecycleNode) {
	c.t.Helper()
	query := url.Values{"host": {n.xname}}
	for _, path := range []string{"/bootscript", "/boot/v1/bootscript"} {
		r, err := c.boot.Raw(c.ctx, Request{Method: http.MethodGet, Path: path + "?" + query.Encode()})
		r = c.status(r, err, http.StatusOK)
		if !c.matches(r.Body, "A") {
			c.t.Fatal("legacy additive group score did not beat MAC score")
		}
	}
}
