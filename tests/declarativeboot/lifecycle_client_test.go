//go:build integration && declarative_boot

// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package declarativeboot

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type lifecycleAssignment struct {
	resource BootAssignment
	etag     string
	deleted  bool
}

func (c *lifecycleCase) createAssignment(suffix string, spec AssignmentSpec) *lifecycleAssignment {
	c.t.Helper()
	item, response := c.tryAssignment(suffix, spec)
	c.status(response, nil, http.StatusCreated)
	return item
}

func (c *lifecycleCase) tryAssignment(suffix string, spec AssignmentSpec) (*lifecycleAssignment, Response) {
	c.t.Helper()
	item := &lifecycleAssignment{resource: BootAssignment{Metadata: Metadata{Name: c.series + "-" + suffix}, Spec: spec}}
	response, err := c.boot.ApplyAssignment(c.ctx, ConditionalAssignment{Resource: item.resource, IfNoneMatch: "*"})
	if err != nil {
		c.t.Fatalf("assignment request: %v", err)
	}
	if response.Status != http.StatusCreated {
		return nil, response
	}
	c.t.Cleanup(func() {
		if item.deleted {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		current, err := c.boot.GetAssignment(ctx, item.resource.Metadata.Name)
		if err != nil || current.Status != http.StatusOK {
			c.t.Errorf("assignment cleanup read: status=%d err=%v", current.Status, err)
			return
		}
		stored, err := DecodeAssignment(current)
		if err != nil || (item.resource.Metadata.UID != "" && stored.Metadata.UID != item.resource.Metadata.UID) {
			c.t.Error("assignment cleanup identity mismatch")
			return
		}
		etag := current.Header.Get("ETag")
		if !lifecycleStrongETag(etag) {
			c.t.Error("assignment cleanup lacks strong ETag")
			return
		}
		deleted, err := c.boot.DeleteAssignment(ctx, ConditionalName{Name: item.resource.Metadata.Name, IfMatch: etag})
		if err != nil || (deleted.Status != http.StatusOK && deleted.Status != http.StatusNoContent) {
			c.t.Errorf("assignment cleanup delete: status=%d err=%v", deleted.Status, err)
		}
	})
	stored, err := DecodeAssignment(response)
	if err != nil || stored.Metadata.Name != item.resource.Metadata.Name {
		c.t.Fatal("invalid created assignment")
	}
	item.resource.Metadata.UID = stored.Metadata.UID
	item.etag = response.Header.Get("ETag")
	if !lifecycleStrongETag(item.etag) {
		c.t.Fatal("assignment lacks strong ETag")
	}
	return item, response
}

func lifecycleStrongETag(etag string) bool {
	return len(etag) >= 3 && strings.HasPrefix(etag, `"`) && strings.HasSuffix(etag, `"`)
}

func (c *lifecycleCase) updateAssignment(item *lifecycleAssignment, spec AssignmentSpec) {
	c.t.Helper()
	resource := item.resource
	resource.Spec = spec
	response, err := c.boot.ApplyAssignment(c.ctx, ConditionalAssignment{Resource: resource, IfMatch: item.etag})
	response = c.status(response, err, http.StatusOK)
	stored, err := DecodeAssignment(response)
	if err != nil || stored.Metadata.UID != item.resource.Metadata.UID {
		c.t.Fatal("assignment update changed identity")
	}
	item.resource, item.etag = resource, response.Header.Get("ETag")
	if !lifecycleStrongETag(item.etag) {
		c.t.Fatal("assignment update lacks strong ETag")
	}
}

func (c *lifecycleCase) deleteAssignment(item *lifecycleAssignment) {
	c.t.Helper()
	response, err := c.boot.DeleteAssignment(c.ctx, ConditionalName{Name: item.resource.Metadata.Name, IfMatch: item.etag})
	c.status(response, err, http.StatusOK, http.StatusNoContent)
	item.deleted = true
}

func (c *lifecycleCase) createA() RevisionRecord {
	c.t.Helper()
	response, err := c.boot.CreateConfig(c.ctx, c.config("A"))
	a := c.revision(c.status(response, err, http.StatusCreated))
	if a.Metadata.RevisionName != c.config("A").RevisionName || a.Resource.Spec != c.config("A").Resource.Spec {
		c.t.Fatal("A content differs from fixture")
	}
	return a
}

func (c *lifecycleCase) ensureB() RevisionRecord {
	c.t.Helper()
	response, err := c.boot.EnsureRevision(c.ctx, c.series, c.config("B"))
	b := c.revision(c.status(response, err, http.StatusCreated))
	if b.Metadata.RevisionName != c.config("B").RevisionName || b.Resource.Spec != c.config("B").Resource.Spec {
		c.t.Fatal("B content differs from fixture")
	}
	return b
}

func (c *lifecycleCase) promote(uid string) {
	c.t.Helper()
	_, etag := c.seriesState()
	response, err := c.boot.PromoteDefault(c.ctx, Promotion{Series: c.series, RevisionUID: uid, IfMatch: etag})
	c.status(response, err, http.StatusOK)
}

func (c *lifecycleCase) script(path string, n lifecycleNode) Response {
	c.t.Helper()
	query := url.Values{"mac": {n.mac}}
	response, err := c.boot.Raw(c.ctx, Request{Method: http.MethodGet, Path: path + "?" + query.Encode()})
	return c.status(response, err, http.StatusOK)
}

func (c *lifecycleCase) expectArtifact(n lifecycleNode, key string) {
	c.t.Helper()
	response := c.script("/bootscript", n)
	want := c.fixtures.Expected.Artifacts[key]
	other := c.fixtures.Expected.Artifacts[map[string]string{"A": "B", "B": "A"}[key]]
	text := string(response.Body)
	if !lifecycleArtifactMatches(text, want) {
		c.t.Fatalf("node %s did not receive fixture %s tuple", n.xname, key)
	}
	for _, token := range append([]string{other.Kernel, other.Initrd}, other.Params...) {
		if strings.Contains(text, token) {
			c.t.Fatal(fmt.Sprintf("node %s script contains alternate revision artifact", n.xname))
		}
	}
}
