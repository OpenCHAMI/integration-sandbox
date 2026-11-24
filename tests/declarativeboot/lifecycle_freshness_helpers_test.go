//go:build integration && declarative_boot

// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package declarativeboot

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func (c *lifecycleCase) metric() float64 {
	c.t.Helper()
	r, err := c.boot.Raw(c.ctx, Request{Method: http.MethodGet, Path: "/metrics"})
	r = c.status(r, err, http.StatusOK)
	value, err := lifecycleCacheMetric(string(r.Body))
	if err != nil {
		c.t.Fatalf("cache metric: %v", err)
	}
	return value
}

func (c *lifecycleCase) query(ctx context.Context, values url.Values) (Response, error) {
	return c.boot.Raw(ctx, Request{Method: http.MethodGet, Path: "/bootscript?" + values.Encode()})
}

func (c *lifecycleCase) matches(body []byte, key string) bool {
	text := string(body)
	if !lifecycleArtifactMatches(text, c.fixtures.Expected.Artifacts[key]) {
		return false
	}
	other := c.fixtures.Expected.Artifacts[map[string]string{"A": "B", "B": "A"}[key]]
	for _, token := range append([]string{other.Kernel, other.Initrd}, other.Params...) {
		if strings.Contains(text, token) {
			return false
		}
	}
	return true
}

func (c *lifecycleCase) expectQuery(values url.Values, key string) {
	c.t.Helper()
	r, err := c.query(c.ctx, values)
	r = c.status(r, err, http.StatusOK)
	if !c.matches(r.Body, key) {
		c.t.Fatalf("bootscript did not contain the complete %s tuple", key)
	}
}

func (c *lifecycleCase) warm(values url.Values, key string) {
	c.t.Helper()
	c.expectQuery(values, key)
	before := c.metric()
	c.expectQuery(values, key)
	if after := c.metric(); after <= before {
		c.t.Fatalf("cache hits did not increase: before=%v after=%v", before, after)
	}
}

type lifecycleAwait struct {
	key           string
	requiredMAC   string
	requiredXname string
	changedAt     time.Time
}

func (c *lifecycleCase) await(values url.Values, expected lifecycleAwait) {
	c.t.Helper()
	ctx, cancel := context.WithDeadline(c.ctx, expected.changedAt.Add(5*time.Second))
	defer cancel()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		r, err := c.query(ctx, values)
		if err != nil {
			c.t.Fatalf("refresh request failed: %v", err)
		}
		if r.Status != http.StatusOK {
			c.t.Fatalf("refresh returned HTTP %d", r.Status)
		}
		if c.matches(r.Body, expected.key) && (expected.requiredMAC == "" || lifecycleHasBootIF(string(r.Body), expected.requiredMAC)) && (expected.requiredXname == "" || strings.Contains(string(r.Body), expected.requiredXname)) {
			return
		}
		if !c.matches(r.Body, "A") && !c.matches(r.Body, "B") {
			c.t.Fatal("refresh returned an unexpected or mixed tuple")
		}
		select {
		case <-ctx.Done():
			c.t.Fatalf("SMD update was not visible within five seconds: %v", ctx.Err())
		case <-ticker.C:
		}
	}
}

func (c *lifecycleCase) requireMembershipTTL() {
	c.t.Helper()
	r, err := c.boot.Raw(c.ctx, Request{Method: http.MethodGet, Path: "/metrics"})
	r = c.status(r, err, http.StatusOK)
	if _, err := lifecycleMembershipTTL(string(r.Body)); err != nil {
		c.t.Fatalf("membership TTL diagnostic: %v", err)
	}
}

func (c *lifecycleCase) setGroup(label string, members []string) time.Time {
	c.t.Helper()
	r, err := c.smd.GetGroup(c.ctx, label)
	group, err := DecodeGroup(c.status(r, err, http.StatusOK))
	if err != nil || group.Label != label {
		c.t.Fatal("invalid SMD group response")
	}
	group.Members.IDs = append([]string{}, members...)
	r, err = c.smd.UpdateGroup(c.ctx, group)
	c.status(r, err, http.StatusOK, http.StatusCreated, http.StatusNoContent)
	changedAt := time.Now()
	r, err = c.smd.GetGroup(c.ctx, label)
	stored, err := DecodeGroup(c.status(r, err, http.StatusOK))
	if err != nil || stored.Label != label || !equalLifecycleMembers(stored.Members.IDs, members) {
		c.t.Fatal("SMD group membership update did not persist")
	}
	return changedAt
}
