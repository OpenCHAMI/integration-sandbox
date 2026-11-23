//go:build integration && declarative_boot

// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package declarativeboot

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"reflect"
	"time"
)

type lifecycleLegacySeedInput struct {
	node   lifecycleNode
	groups []string
}

type lifecycleLegacyNode struct {
	Metadata struct {
		Name string `json:"name"`
		UID  string `json:"uid"`
	} `json:"metadata"`
	Spec json.RawMessage `json:"spec"`
}

type lifecycleLegacyNodeSpec struct {
	Xname    string   `json:"xname"`
	NID      int32    `json:"nid"`
	BootMAC  string   `json:"bootMac"`
	Role     string   `json:"role"`
	Hostname string   `json:"hostname"`
	Groups   []string `json:"groups"`
}

func lifecycleLegacyNodes(ctx context.Context, boot BootClient, xname string) ([]lifecycleLegacyNode, error) {
	r, err := boot.Raw(ctx, Request{Method: http.MethodGet, Path: "/nodes"})
	if err != nil {
		return nil, err
	}
	if err := r.RequireStatus(http.StatusOK); err != nil {
		return nil, err
	}
	var records []lifecycleLegacyNode
	if err := json.Unmarshal(r.Body, &records); err != nil || records == nil {
		return nil, errors.New("invalid legacy node list")
	}
	var matches []lifecycleLegacyNode
	for _, record := range records {
		var spec lifecycleLegacyNodeSpec
		if err := json.Unmarshal(record.Spec, &spec); err != nil {
			return nil, errors.New("invalid legacy node spec")
		}
		if spec.Xname == xname {
			if record.Metadata.UID == "" {
				return nil, errors.New("legacy node lacks UID")
			}
			matches = append(matches, record)
		}
	}
	return matches, nil
}

func lifecycleSeedLegacyNode(ctx context.Context, boot BootClient, input lifecycleLegacySeedInput) (func(context.Context) error, error) {
	matches, err := lifecycleLegacyNodes(ctx, boot, input.node.xname)
	if err != nil {
		return nil, err
	}
	if len(matches) > 1 {
		return nil, errors.New("duplicate legacy node xname")
	}
	want := lifecycleLegacyNodeSpec{Xname: input.node.xname, NID: input.node.nid, BootMAC: input.node.mac, Role: "Compute", Hostname: input.node.xname, Groups: append([]string(nil), input.groups...)}
	spec, err := json.Marshal(want)
	if err != nil {
		return nil, err
	}
	created := len(matches) == 0
	var node lifecycleLegacyNode
	var original json.RawMessage
	var etag string
	if created {
		node.Metadata.Name = input.node.xname
	} else {
		uid := matches[0].Metadata.UID
		r, err := boot.Raw(ctx, Request{Method: http.MethodGet, Path: "/nodes/" + url.PathEscape(uid)})
		if err != nil {
			return nil, err
		}
		if err := r.RequireStatus(http.StatusOK); err != nil {
			return nil, err
		}
		var currentSpec lifecycleLegacyNodeSpec
		if err := json.Unmarshal(r.Body, &node); err != nil || node.Metadata.UID != uid || json.Unmarshal(node.Spec, &currentSpec) != nil || currentSpec.Xname != input.node.xname {
			return nil, errors.New("legacy node changed during inspection")
		}
		original = append(json.RawMessage(nil), node.Spec...)
		etag = r.Header.Get("ETag")
	}
	node.Spec = spec
	body, err := json.Marshal(node)
	if err != nil {
		return nil, err
	}
	method, uid, expected := http.MethodPut, node.Metadata.UID, http.StatusOK
	if created {
		method, uid, expected = http.MethodPost, "", http.StatusCreated
	}
	var applied json.RawMessage = spec
	var cleanup func(context.Context) error
	if !created {
		cleanup = lifecycleLegacyCleanup(boot, node.Metadata.UID, input.node.xname, original, &applied, false)
	}
	path := "/nodes"
	if uid != "" {
		path += "/" + url.PathEscape(uid)
	}
	header := http.Header{"Content-Type": {"application/json"}}
	if lifecycleStrongETag(etag) {
		header.Set("If-Match", etag)
	}
	r, err := boot.Raw(ctx, Request{Method: method, Path: path, Headers: header, Body: body})
	if err != nil {
		return cleanup, err
	}
	if err := r.RequireStatus(expected); err != nil {
		return cleanup, err
	}
	var stored lifecycleLegacyNode
	if err := json.Unmarshal(r.Body, &stored); err != nil || stored.Metadata.UID == "" {
		return cleanup, errors.New("invalid legacy node write response")
	}
	if created {
		cleanup = lifecycleLegacyCleanup(boot, stored.Metadata.UID, input.node.xname, nil, &applied, true)
	}
	if !created && stored.Metadata.UID != uid {
		return cleanup, errors.New("legacy node UID changed")
	}
	var storedSpec lifecycleLegacyNodeSpec
	if err := json.Unmarshal(stored.Spec, &storedSpec); err != nil || !reflect.DeepEqual(storedSpec, want) {
		return cleanup, errors.New("legacy node identity did not round-trip")
	}
	applied = append(json.RawMessage(nil), stored.Spec...)
	current, err := lifecycleLegacyNodes(ctx, boot, input.node.xname)
	if err != nil {
		return cleanup, err
	}
	if len(current) != 1 || current[0].Metadata.UID != stored.Metadata.UID {
		return cleanup, errors.New("legacy node identity is ambiguous")
	}
	return cleanup, nil
}

func (c *lifecycleCase) seedLegacyNode(n lifecycleNode, groups []string) {
	c.t.Helper()
	cleanup, err := lifecycleSeedLegacyNode(c.ctx, c.boot, lifecycleLegacySeedInput{node: n, groups: groups})
	if cleanup != nil {
		c.t.Cleanup(func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := cleanup(ctx); err != nil {
				c.t.Errorf("legacy node cleanup: %v", err)
			}
		})
	}
	if err != nil {
		c.t.Fatalf("legacy node fixture: %v", err)
	}
}
