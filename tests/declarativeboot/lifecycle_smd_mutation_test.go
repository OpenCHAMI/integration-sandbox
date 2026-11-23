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

func (c *lifecycleCase) setNID(n lifecycleNode, nid int32) time.Time {
	c.t.Helper()
	body, err := EncodeJSON(struct {
		NID int32 `json:"NID"`
	}{NID: nid})
	if err != nil {
		c.t.Fatal(err)
	}
	path := "/hsm/v2/State/Components/" + url.PathEscape(n.xname)
	r, err := c.smd.HTTP.Do(c.ctx, Request{Method: http.MethodPatch, Path: path + "/NID", Headers: http.Header{"Content-Type": {"application/json"}}, Body: body})
	c.status(r, err, http.StatusNoContent, http.StatusOK)
	changedAt := time.Now()
	r, err = c.smd.HTTP.Do(c.ctx, Request{Method: http.MethodGet, Path: path})
	stored, err := DecodeJSON[Component](c.status(r, err, http.StatusOK))
	if err != nil || stored.ID != n.xname || stored.NID != nid {
		c.t.Fatal("SMD NID update did not persist")
	}
	return changedAt
}

func (c *lifecycleCase) changeMAC(n lifecycleNode) (lifecycleNode, time.Time) {
	c.t.Helper()
	oldID := strings.ReplaceAll(n.mac, ":", "")
	path := "/hsm/v2/Inventory/EthernetInterfaces/" + url.PathEscape(oldID)
	r, err := c.smd.HTTP.Do(c.ctx, Request{Method: http.MethodDelete, Path: path})
	c.status(r, err, http.StatusOK, http.StatusNoContent)
	c.t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := c.smd.CreateInterface(ctx, EthernetInterface{ID: oldID, MACAddress: n.mac, ComponentID: n.xname, Type: "Node"}); err != nil {
			c.t.Errorf("restore SMD interface: %v", err)
		}
	})
	changed := n
	changed.mac = n.mac[:strings.LastIndex(n.mac, ":")+1] + "f0"
	newID := strings.ReplaceAll(changed.mac, ":", "")
	if err := c.smd.CreateInterface(c.ctx, EthernetInterface{ID: newID, MACAddress: changed.mac, ComponentID: n.xname, Type: "Node"}); err != nil {
		c.t.Fatalf("create replacement SMD interface: %v", err)
	}
	changedAt := time.Now()
	c.smdCleanup("/hsm/v2/Inventory/EthernetInterfaces/" + url.PathEscape(newID))
	return changed, changedAt
}

func (c *lifecycleCase) moveInterface(n lifecycleNode, xname string) time.Time {
	c.t.Helper()
	body, err := EncodeJSON(struct {
		ComponentID string `json:"ComponentID"`
	}{ComponentID: xname})
	if err != nil {
		c.t.Fatal(err)
	}
	id := strings.ReplaceAll(n.mac, ":", "")
	path := "/hsm/v2/Inventory/EthernetInterfaces/" + url.PathEscape(id)
	r, err := c.smd.HTTP.Do(c.ctx, Request{Method: http.MethodPatch, Path: path, Headers: http.Header{"Content-Type": {"application/json"}}, Body: body})
	c.status(r, err, http.StatusOK)
	changedAt := time.Now()
	r, err = c.smd.HTTP.Do(c.ctx, Request{Method: http.MethodGet, Path: path})
	stored, err := DecodeJSON[EthernetInterface](c.status(r, err, http.StatusOK))
	if err != nil || stored.ID != id || stored.ComponentID != xname {
		c.t.Fatal("SMD interface identity update did not persist")
	}
	return changedAt
}

func (c *lifecycleCase) restoreInterface(n lifecycleNode) {
	c.t.Helper()
	c.t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		body, err := EncodeJSON(struct {
			ComponentID string `json:"ComponentID"`
		}{ComponentID: n.xname})
		if err != nil {
			c.t.Error("cannot encode interface restoration")
			return
		}
		path := "/hsm/v2/Inventory/EthernetInterfaces/" + url.PathEscape(strings.ReplaceAll(n.mac, ":", ""))
		r, err := c.smd.HTTP.Do(ctx, Request{Method: http.MethodPatch, Path: path, Headers: http.Header{"Content-Type": {"application/json"}}, Body: body})
		if err != nil || r.Status != http.StatusOK {
			c.t.Error("cannot restore SMD interface identity")
		}
	})
}
