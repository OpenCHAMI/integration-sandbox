//go:build integration && declarative_boot

// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package declarativeboot

import (
	"context"
	"net/http"
	"testing"
	"time"
)

type safetyState struct {
	Series         string
	InitialUID     string
	DefaultUID     string
	LatestUID      string
	AssignmentName string
	AssignmentUID  string
	PinName        string
	PinUID         string
	PinNode        FixtureNode
	Node           FixtureNode
}

func (h *safetyHarness) createDeclarativeState(node FixtureNode) safetyState {
	h.t.Helper()
	series := safetyName(h.t)
	aResp, err := h.admin.CreateConfig(h.ctx, NamedConfig{RevisionName: h.fixtures.Configs.Revisions["A"].RevisionName, Resource: BootConfig{Metadata: Metadata{Name: series}, Spec: h.fixtures.Configs.Revisions["A"].Spec}})
	safetyStatus(h.t, aResp, err, http.StatusCreated)
	a := mustRevision(h.t, aResp)
	bResp, err := h.admin.EnsureRevision(h.ctx, series, NamedConfig{RevisionName: h.fixtures.Configs.Revisions["B"].RevisionName, Resource: BootConfig{Metadata: Metadata{Name: series}, Spec: h.fixtures.Configs.Revisions["B"].Spec}})
	safetyStatus(h.t, bResp, err, http.StatusCreated)
	b := mustRevision(h.t, bResp)
	_, etag := h.seriesState(series)
	promoted, err := h.admin.PromoteDefault(h.ctx, Promotion{Series: series, RevisionUID: b.Metadata.UID, IfMatch: etag})
	safetyStatus(h.t, promoted, err, http.StatusOK)
	pinName, pinUID, pinNode := h.createPinnedA(series, a.Metadata.UID)
	name := safetyName(h.t)
	priority := 100
	created, err := h.admin.ApplyAssignment(h.ctx, ConditionalAssignment{IfNoneMatch: "*", Resource: BootAssignment{Metadata: Metadata{Name: name}, Spec: AssignmentSpec{Selector: &Selector{Xnames: []string{node.Xname}}, Priority: &priority, ConfigRef: ConfigReference{Name: series}}}})
	safetyStatus(h.t, created, err, http.StatusCreated)
	assignment, err := DecodeAssignment(created)
	if err != nil {
		h.t.Fatalf("decode assignment: %v", err)
	}
	h.t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		current, readErr := h.admin.GetAssignment(ctx, name)
		if readErr != nil {
			h.t.Errorf("recovery cleanup read: %v", readErr)
			return
		}
		if current.Status == http.StatusNotFound {
			return
		}
		if current.Status != http.StatusOK || !safetyStrongETag(current.Header.Get("ETag")) {
			h.t.Errorf("recovery cleanup read HTTP %d or invalid ETag", current.Status)
			return
		}
		deleted, deleteErr := h.admin.DeleteAssignment(ctx, ConditionalName{Name: name, IfMatch: current.Header.Get("ETag")})
		if deleteErr != nil || (deleted.Status != http.StatusOK && deleted.Status != http.StatusNoContent) {
			h.t.Errorf("recovery cleanup delete HTTP %d: %v", deleted.Status, deleteErr)
		}
	})
	script := h.bootscript(safetyEnv(h.t, "SBX_DECLARATIVE_BOOT_URL"), node)
	safetyStatus(h.t, script, nil, http.StatusOK)
	if !lifecycleArtifactMatches(string(script.Body), h.fixtures.Expected.Artifacts["B"]) {
		h.t.Fatal("bootscript does not match promoted B artifact")
	}
	return safetyState{Series: series, InitialUID: a.Metadata.UID, DefaultUID: b.Metadata.UID, LatestUID: b.Metadata.UID, AssignmentName: name, AssignmentUID: assignment.Metadata.UID, PinName: pinName, PinUID: pinUID, PinNode: pinNode, Node: node}
}

func (h *safetyHarness) seriesState(series string) (SeriesStatus, string) {
	h.t.Helper()
	response, err := h.admin.GetSeries(h.ctx, series)
	response = safetyStatus(h.t, response, err, http.StatusOK)
	value, err := DecodeSeries(response)
	if err != nil {
		h.t.Fatalf("decode series: %v", err)
	}
	return value, safetyETag(h.t, response)
}

func (h *safetyHarness) waitHealthy(base string) {
	h.t.Helper()
	client, err := NewHTTPClient(base, 10*time.Second)
	if err != nil {
		h.t.Fatalf("health client: %v", err)
	}
	deadline := time.Now().Add(45 * time.Second)
	for time.Now().Before(deadline) {
		response, reqErr := client.Do(h.ctx, Request{Method: http.MethodGet, Path: "/health"})
		if reqErr == nil && (response.Status == http.StatusOK || response.Status == http.StatusNoContent) {
			return
		}
		time.Sleep(time.Second)
	}
	h.t.Fatalf("service at %s did not return healthy before timeout", base)
}

func mustRevision(t *testing.T, response Response) RevisionRecord {
	t.Helper()
	value, err := DecodeRevision(response)
	if err != nil {
		t.Fatalf("decode revision: %v", err)
	}
	return value
}
