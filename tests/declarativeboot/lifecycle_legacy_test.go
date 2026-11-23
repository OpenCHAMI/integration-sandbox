//go:build integration && declarative_boot

// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package declarativeboot

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

type lifecycleLegacySpec struct {
	Hosts    []string `json:"hosts,omitempty"`
	Groups   []string `json:"groups,omitempty"`
	MACs     []string `json:"macs,omitempty"`
	Priority int      `json:"priority,omitempty"`
	Kernel   string   `json:"kernel"`
	Initrd   string   `json:"initrd"`
	Params   string   `json:"params"`
}

type lifecycleLegacyResource struct {
	Metadata Metadata            `json:"metadata"`
	Spec     lifecycleLegacySpec `json:"spec"`
}

func (c *lifecycleCase) legacy(n lifecycleNode) lifecycleLegacyResource {
	c.t.Helper()
	fixture := c.fixtures.Legacy.Default
	return c.createLegacy("legacy", lifecycleLegacySpec{Hosts: []string{n.xname}, Kernel: fixture.Kernel, Initrd: fixture.Initrd, Params: fixture.Params})
}

func (c *lifecycleCase) createLegacy(suffix string, spec lifecycleLegacySpec) lifecycleLegacyResource {
	c.t.Helper()
	want := lifecycleLegacyResource{Metadata: Metadata{Name: c.series + "-" + suffix}, Spec: spec}
	body, err := EncodeJSON(want)
	if err != nil {
		c.t.Fatal(err)
	}
	response, err := c.boot.Raw(c.ctx, Request{Method: http.MethodPost, Path: "/bootconfigurations", Headers: http.Header{"Content-Type": {"application/json"}}, Body: body})
	response = c.status(response, err, http.StatusCreated)
	created, err := DecodeJSON[lifecycleLegacyResource](response)
	if err != nil || created.Metadata.Name != want.Metadata.Name || created.Metadata.UID == "" || !reflect.DeepEqual(created.Spec, want.Spec) {
		c.t.Fatal("invalid legacy create response")
	}
	c.t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		r, err := c.boot.Raw(ctx, Request{Method: http.MethodDelete, Path: "/bootconfigurations/" + url.PathEscape(created.Metadata.UID)})
		if err != nil || (r.Status != http.StatusOK && r.Status != http.StatusNoContent) {
			c.t.Errorf("legacy cleanup: status=%d err=%v", r.Status, err)
		}
	})
	return created
}

func (c *lifecycleCase) legacyArtifact(path string, query url.Values) {
	c.t.Helper()
	response, err := c.boot.Raw(c.ctx, Request{Method: http.MethodGet, Path: path + "?" + query.Encode()})
	response = c.status(response, err, http.StatusOK)
	f := c.fixtures.Legacy.Default
	want := Artifact{Kernel: f.Kernel, Initrd: f.Initrd, Params: strings.Fields(f.Params)}
	text := string(response.Body)
	if !lifecycleArtifactMatches(text, want) {
		c.t.Fatal("legacy boot did not deliver the fixture tuple")
	}
	for _, key := range []string{"A", "B"} {
		alt := c.fixtures.Expected.Artifacts[key]
		if strings.Contains(text, alt.Kernel) || strings.Contains(text, alt.Initrd) || strings.Contains(text, "release="+key) {
			c.t.Fatal("legacy script contains declarative artifact")
		}
	}
}

func (c *lifecycleCase) legacyRead(uid string) lifecycleLegacyResource {
	c.t.Helper()
	response, err := c.boot.Raw(c.ctx, Request{Method: http.MethodGet, Path: "/bootconfigurations/" + url.PathEscape(uid)})
	value, err := DecodeJSON[lifecycleLegacyResource](c.status(response, err, http.StatusOK))
	if err != nil || value.Metadata.UID != uid {
		c.t.Fatal("invalid legacy read")
	}
	return value
}

func (c *lifecycleCase) jsonSnapshot(path string) json.RawMessage {
	c.t.Helper()
	response, err := c.boot.Raw(c.ctx, Request{Method: http.MethodGet, Path: path})
	response = c.status(response, err, http.StatusOK)
	var value json.RawMessage
	if err := json.Unmarshal(response.Body, &value); err != nil || len(value) == 0 || string(value) == "null" {
		c.t.Fatalf("invalid JSON from %s", path)
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, value); err != nil {
		c.t.Fatal(err)
	}
	return json.RawMessage(compact.String())
}

func lifecycleRoutesLegacy(t *testing.T) {
	c := lifecycleNew(t)
	nodes := c.nodes(2)
	group := c.group("routes", nodes...)
	c.seedLegacyNode(nodes[0], []string{group})
	legacy := c.legacy(nodes[0])
	paths := []string{"/nodes", "/bmcs", "/bootconfigurations", "/bootparameters", "/boot/v1/bootparameters"}
	assertRedirects := func() {
		t.Helper()
		for _, path := range []string{"/bootparameters", "/boot/v1/bootparameters"} {
			response, err := c.boot.Raw(c.ctx, Request{Method: http.MethodGet, Path: path + "/"})
			response = c.status(response, err, http.StatusMovedPermanently)
			if location := response.Header.Get("Location"); location != path {
				t.Fatalf("%s/ redirect Location = %q, want %q", path, location, path)
			}
		}
	}
	assertRedirects()
	before := make([]json.RawMessage, len(paths))
	for i, path := range paths {
		before[i] = c.jsonSnapshot(path)
	}
	query := url.Values{"host": {nodes[0].xname}}
	c.legacyArtifact("/bootscript", query)
	c.legacyArtifact("/boot/v1/bootscript", query)
	c.createA()
	c.createAssignment("declarative", AssignmentSpec{Selector: &Selector{Xnames: []string{nodes[1].xname}}, ConfigRef: ConfigReference{Name: c.series}})
	c.expectArtifact(nodes[1], "A")
	assertRedirects()
	for i, path := range paths {
		after := c.jsonSnapshot(path)
		var oldValue, newValue interface{}
		if json.Unmarshal(before[i], &oldValue) != nil || json.Unmarshal(after, &newValue) != nil || !reflect.DeepEqual(oldValue, newValue) {
			t.Fatalf("legacy route %s changed after new resource creation", path)
		}
	}
	if !reflect.DeepEqual(legacy, c.legacyRead(legacy.Metadata.UID)) {
		t.Fatal("legacy configuration changed")
	}
	for _, identifier := range []url.Values{{"host": {nodes[0].xname}}, {"mac": {nodes[0].mac}}, {"nid": {strconv.FormatInt(int64(nodes[0].nid), 10)}}} {
		c.legacyArtifact("/bootscript", identifier)
		c.legacyArtifact("/boot/v1/bootscript", identifier)
	}
}

func lifecycleUnknownLegacy(t *testing.T) {
	c := lifecycleNew(t)
	nodes := c.nodes(2)
	group := c.group("unknown", nodes...)
	c.seedLegacyNode(nodes[0], []string{group})
	legacy := c.legacy(nodes[0])
	query := url.Values{"host": {nodes[0].xname}}
	c.legacyArtifact("/bootscript", query)
	c.legacyArtifact("/boot/v1/bootscript", query)
	unknown := url.Values{"host": {strings.Replace(nodes[0].xname, "n0", "n99", 1)}}
	paths := []string{"/bootscript", "/boot/v1/bootscript"}
	before := make([]string, len(paths))
	for i, path := range paths {
		response, err := c.boot.Raw(c.ctx, Request{Method: http.MethodGet, Path: path + "?" + unknown.Encode()})
		response = c.status(response, err, http.StatusOK)
		before[i] = string(response.Body)
		if !strings.HasPrefix(before[i], "#!ipxe") || !strings.Contains(before[i], "halt") {
			t.Fatal("unknown node did not receive legacy refusal")
		}
	}
	c.createA()
	c.createAssignment("other-node", AssignmentSpec{Selector: &Selector{Xnames: []string{nodes[1].xname}}, ConfigRef: ConfigReference{Name: c.series}})
	c.expectArtifact(nodes[1], "A")
	c.legacyArtifact("/bootscript", query)
	c.legacyArtifact("/boot/v1/bootscript", query)
	if !reflect.DeepEqual(legacy, c.legacyRead(legacy.Metadata.UID)) {
		t.Fatal("unmatched assignment changed legacy configuration")
	}
	for i, path := range paths {
		response, err := c.boot.Raw(c.ctx, Request{Method: http.MethodGet, Path: path + "?" + unknown.Encode()})
		response = c.status(response, err, http.StatusOK)
		if string(response.Body) != before[i] {
			t.Fatal("unknown node response changed after unrelated assignment")
		}
	}
}
