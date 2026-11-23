//go:build integration && declarative_boot

// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package declarativeboot

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type lifecycleNode struct {
	xname, mac string
	nid        int32
}

func (c *lifecycleCase) smdCleanup(path string) {
	c.t.Helper()
	c.t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		response, err := c.smd.HTTP.Do(ctx, Request{Method: http.MethodDelete, Path: path})
		if err != nil {
			c.t.Errorf("SMD cleanup: %v", err)
			return
		}
		if err := response.RequireStatus(http.StatusOK, http.StatusNoContent); err != nil {
			c.t.Errorf("SMD cleanup: %v", err)
		}
	})
}

func (c *lifecycleCase) nodes(count int) []lifecycleNode {
	c.t.Helper()
	nodes, err := lifecycleReservedNodes(count, c.series)
	if err != nil {
		c.t.Fatal(err)
	}
	for _, n := range nodes {
		componentPath := "/hsm/v2/State/Components/" + url.PathEscape(n.xname)
		response, err := c.smd.HTTP.Do(c.ctx, Request{Method: http.MethodGet, Path: componentPath})
		c.status(response, err, http.StatusNotFound)
		if err := c.smd.PutComponents(c.ctx, Components{Components: []Component{{ID: n.xname, Type: "Node", State: "On", Flag: "OK", Enabled: true, Role: "Compute", NID: n.nid}}}); err != nil {
			c.t.Fatalf("create SMD component: %v", err)
		}
		c.smdCleanup(componentPath)
		interfaceID := strings.ReplaceAll(n.mac, ":", "")
		interfacePath := "/hsm/v2/Inventory/EthernetInterfaces/" + url.PathEscape(interfaceID)
		response, err = c.smd.HTTP.Do(c.ctx, Request{Method: http.MethodGet, Path: interfacePath})
		c.status(response, err, http.StatusNotFound)
		if err := c.smd.CreateInterface(c.ctx, EthernetInterface{ID: interfaceID, MACAddress: n.mac, ComponentID: n.xname, Type: "Node"}); err != nil {
			c.t.Fatalf("create SMD interface: %v", err)
		}
		c.smdCleanup(interfacePath)
	}
	return nodes
}

func lifecycleReservedNodes(count int, series string) ([]lifecycleNode, error) {
	if count < 1 || count > 8 {
		return nil, errors.New("fixture count must be between 1 and 8")
	}
	seed, err := hex.DecodeString(strings.TrimPrefix(series, "lifecycle-"))
	if err != nil || len(seed) != 8 || !strings.HasPrefix(series, "lifecycle-") {
		return nil, errors.New("invalid fixture identity")
	}
	nodes := make([]lifecycleNode, count)
	for i := range nodes {
		nodes[i] = lifecycleNode{xname: fmt.Sprintf("x1c0s%db0n0", i), nid: int32(10000 + i), mac: fmt.Sprintf("02:%02x:%02x:%02x:%02x:%02x", seed[4], seed[5], seed[6], seed[7], i)}
	}
	return nodes, nil
}

func (c *lifecycleCase) group(suffix string, nodes ...lifecycleNode) string {
	c.t.Helper()
	label := c.series + "-" + suffix
	group := Group{Label: label, Description: "lifecycle fixture", Members: GroupMembers{IDs: make([]string, 0, len(nodes))}}
	for _, n := range nodes {
		group.Members.IDs = append(group.Members.IDs, n.xname)
	}
	response, err := c.smd.GetGroup(c.ctx, label)
	c.status(response, err, http.StatusNotFound)
	response, err = c.smd.CreateGroup(c.ctx, group)
	c.status(response, err, http.StatusCreated, http.StatusOK, http.StatusNoContent)
	c.smdCleanup("/hsm/v2/groups/" + url.PathEscape(label))
	response, err = c.smd.GetGroup(c.ctx, label)
	response = c.status(response, err, http.StatusOK)
	got, err := DecodeGroup(response)
	if err != nil || got.Label != label || !equalLifecycleMembers(got.Members.IDs, group.Members.IDs) {
		c.t.Fatal("SMD group fixture did not round-trip")
	}
	return label
}

func equalLifecycleMembers(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for _, value := range b {
		found := false
		for _, item := range a {
			if item == value {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}
