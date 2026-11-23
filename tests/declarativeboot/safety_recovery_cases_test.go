//go:build integration && declarative_boot

// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package declarativeboot

import (
	"encoding/json"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"
)

func safetyBrokenWinningReference(t *testing.T) {
	h := newSafetyHarness(t)
	state := h.createDeclarativeState(h.node("default"))
	before := h.persistenceSnapshot(safetyEnv(t, "SBX_DECLARATIVE_BOOT_URL"), state)
	var broken struct {
		BaseURL            string `json:"baseURL"`
		MissingRevisionUID string `json:"missingRevisionUid"`
	}
	h.runControl("broken-reference-fixture", brokenReferenceRequest{AssignmentName: state.AssignmentName, SeriesName: state.Series, RevisionUID: state.DefaultUID, NodeIdentifier: state.Node.Xname}, &broken)
	h.requireSeparateBase(broken.BaseURL)
	h.waitHealthy(broken.BaseURL)
	brokenHTTP, err := NewHTTPClient(broken.BaseURL, 10*time.Second)
	if err != nil {
		t.Fatal("invalid broken fixture endpoint")
	}
	client := BootClient{HTTP: brokenHTTP, Bearer: h.admin.Bearer}
	seriesResponse, err := client.GetSeries(h.ctx, state.Series)
	safetyStatus(t, seriesResponse, err, http.StatusOK)
	series, err := DecodeSeries(seriesResponse)
	if err != nil || series.DefaultRevisionUID != state.DefaultUID || series.LatestRevisionUID != state.LatestUID || series.RevisionCount != 2 {
		t.Fatal("broken copy has unexpected series pointers")
	}
	missing, err := client.GetRevision(h.ctx, RevisionID{Series: state.Series, UID: state.DefaultUID})
	// A committed UID whose record was lost is operational corruption, not an
	// unknown user-supplied UID. Require both the independent filesystem receipt
	// and a closed read response; no successful or arbitrary error is accepted.
	var failure struct {
		Code string `json:"code"`
	}
	closedCorrupt := missing.Status == http.StatusServiceUnavailable && json.Unmarshal(missing.Body, &failure) == nil && failure.Code == "unavailable"
	if err != nil || broken.MissingRevisionUID != state.DefaultUID || (missing.Status != http.StatusNotFound && missing.Status != http.StatusGone && !closedCorrupt) {
		t.Fatal("intended winning revision is not independently missing")
	}
	assignmentResponse, err := client.GetAssignment(h.ctx, state.AssignmentName)
	safetyStatus(t, assignmentResponse, err, http.StatusOK)
	assignment, err := DecodeAssignment(assignmentResponse)
	if err != nil || assignment.Metadata.UID != state.AssignmentUID || assignment.Spec.ConfigRef.Name != state.Series || assignment.Spec.Selector == nil || !slices.Contains(assignment.Spec.Selector.Xnames, state.Node.Xname) || assignment.Spec.Priority == nil || *assignment.Spec.Priority != 100 {
		t.Fatal("broken copy did not retain the intended winning assignment")
	}
	safetyETag(t, assignmentResponse)
	legacyResponse, err := client.Raw(h.ctx, Request{Method: http.MethodGet, Path: "/bootconfigurations"})
	safetyStatus(t, legacyResponse, err, http.StatusOK)
	legacy, err := DecodeJSON[[]legacyBootConfiguration](legacyResponse)
	if err != nil || legacy == nil {
		t.Fatal("broken copy has no readable legacy configurations")
	}
	fallback := false
	for _, item := range legacy {
		if slices.Contains(item.Spec.Hosts, state.Node.Xname) && item.Spec.Priority < 100 && item.Spec.Kernel == h.fixtures.Legacy.Default.Kernel && item.Spec.Initrd == h.fixtures.Legacy.Default.Initrd && item.Spec.Params == h.fixtures.Legacy.Default.Params {
			fallback = true
		}
	}
	if !fallback {
		t.Fatal("broken copy lacks the lower-ranked matching legacy fallback")
	}
	response := h.bootscript(broken.BaseURL, state.Node)
	safetyStatus(t, response, nil, http.StatusServiceUnavailable)
	body := string(response.Body)
	if !strings.Contains(body, state.AssignmentName) || !strings.Contains(body, state.DefaultUID) {
		t.Fatal("broken-reference refusal omitted assignment/reference identity")
	}
	forbidden := []string{h.fixtures.Expected.Artifacts["A"].Kernel, h.fixtures.Expected.Artifacts["A"].Initrd, h.fixtures.Expected.Artifacts["B"].Kernel, h.fixtures.Expected.Artifacts["B"].Initrd, h.fixtures.Legacy.Default.Kernel, h.fixtures.Legacy.Default.Initrd}
	if !safetyNonBootingRefusal(body, forbidden) {
		t.Fatal("broken-reference response was bootable or leaked a fallback artifact")
	}
	safetyComparePersistence(t, before, h.persistenceSnapshot(safetyEnv(t, "SBX_DECLARATIVE_BOOT_URL"), state))
}

func safetyDurableRestartRecreation(t *testing.T) {
	h := newSafetyHarness(t)
	state := h.createDeclarativeState(h.node("default"))
	before := h.persistenceSnapshot(safetyEnv(t, "SBX_DECLARATIVE_BOOT_URL"), state)
	for _, action := range []string{"service-restart", "service-recreate"} {
		safetyControlAction(t, h.ctx, action, serviceActionRequest{Service: "boot"}, &struct{}{})
		h.waitHealthy(safetyEnv(t, "SBX_DECLARATIVE_BOOT_URL"))
		safetyComparePersistence(t, before, h.persistenceSnapshot(safetyEnv(t, "SBX_DECLARATIVE_BOOT_URL"), state))
	}
}

func safetyMatchingVersionBackupRestore(t *testing.T) {
	h := newSafetyHarness(t)
	state := h.createDeclarativeState(h.node("default"))
	before := h.persistenceSnapshot(safetyEnv(t, "SBX_DECLARATIVE_BOOT_URL"), state)
	var restored baseURLResponse
	safetyControlAction(t, h.ctx, "backup-restore", struct{}{}, &restored)
	h.requireSeparateBase(restored.BaseURL)
	h.waitHealthy(restored.BaseURL)
	safetyComparePersistence(t, before, h.persistenceSnapshot(restored.BaseURL, state))
	safetyComparePersistence(t, before, h.persistenceSnapshot(safetyEnv(t, "SBX_DECLARATIVE_BOOT_URL"), state))
}

func (h *safetyHarness) requireSeparateBase(base string) {
	h.t.Helper()
	copyURL, err := url.Parse(base)
	if err != nil || (copyURL.Scheme != "http" && copyURL.Scheme != "https") || copyURL.Host == "" || copyURL.User != nil {
		h.t.Fatal("invalid owned copy endpoint")
	}
	mainURL, err := url.Parse(safetyEnv(h.t, "SBX_DECLARATIVE_BOOT_URL"))
	if err != nil || copyURL.Host == mainURL.Host {
		h.t.Fatal("owned copy must be a separate service endpoint")
	}
}
