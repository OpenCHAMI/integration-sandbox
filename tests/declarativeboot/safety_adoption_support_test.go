//go:build integration && declarative_boot

// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package declarativeboot

import (
	"context"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"slices"
	"testing"
	"time"
)

type safetyAdoptionFixture struct {
	h          *safetyHarness
	source     legacyBootConfiguration
	node       FixtureNode
	series     string
	assignment string
}

type safetyAdoptionSnapshot struct {
	source              []byte
	legacy              []byte
	assignments         []byte
	configurationDigest string
}

func safetyNewAdoption(t *testing.T, unsafe bool) *safetyAdoptionFixture {
	t.Helper()
	h := newSafetyHarness(t)
	node := h.node("legacy")
	groups := []string(nil)
	if unsafe {
		groups = []string{"acceptance-red", "acceptance-blue"}
	}
	source := h.createLegacySource(node, groups)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		response, err := h.admin.Raw(ctx, Request{Method: http.MethodDelete, Path: "/bootconfigurations/" + url.PathEscape(source.Metadata.UID)})
		if err != nil || (response.Status != http.StatusOK && response.Status != http.StatusNoContent) {
			t.Errorf("legacy source cleanup HTTP %d: %v", response.Status, err)
		}
	})
	if source.Metadata.UID == "" || !slices.Equal(source.Spec.Hosts, []string{node.Xname}) || !slices.Equal(source.Spec.Groups, groups) || source.Spec.Kernel != h.fixtures.Legacy.Default.Kernel || source.Spec.Initrd != h.fixtures.Legacy.Default.Initrd || source.Spec.Params != h.fixtures.Legacy.Default.Params {
		t.Fatal("legacy source does not match the literal fixture")
	}
	f := &safetyAdoptionFixture{h: h, source: source, node: node, series: safetyName(t), assignment: safetyName(t)}
	f.cleanupAssignment()
	probeName := safetyName(t)
	created, err := h.admin.CreateConfig(h.ctx, NamedConfig{RevisionName: "release-a", Resource: BootConfig{Metadata: Metadata{Name: probeName}, Spec: safetyArtifactA}})
	safetyStatus(t, created, err, http.StatusCreated)
	revision, err := DecodeRevision(created)
	if err != nil {
		t.Fatal("adoption preflight could not decode a known revision")
	}
	probe, err := h.admin.GetSeries(h.ctx, probeName)
	safetyStatus(t, probe, err, http.StatusOK)
	series, err := DecodeSeries(probe)
	if err != nil || series.DefaultRevisionUID != revision.Metadata.UID {
		t.Fatal("adoption preflight could not read a known series")
	}
	safetyETag(t, probe)
	return f
}

func (f *safetyAdoptionFixture) legacyArtifact() {
	f.h.t.Helper()
	response := f.h.bootscript(safetyEnv(f.h.t, "SBX_DECLARATIVE_BOOT_URL"), f.node)
	safetyStatus(f.h.t, response, nil, http.StatusOK)
	want := f.h.fixtures.Legacy.Default
	if !safetyArtifactTuple(string(response.Body), BootConfigSpec{Kernel: want.Kernel, Initrd: want.Initrd, Params: want.Params}) {
		f.h.t.Fatal("adoption witness did not deliver the literal legacy artifact")
	}
}

func (f *safetyAdoptionFixture) cleanupAssignment() {
	f.h.t.Helper()
	f.h.t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		current, err := f.h.admin.GetAssignment(ctx, f.assignment)
		if err != nil {
			f.h.t.Errorf("adoption cleanup read: %v", err)
			return
		}
		if current.Status == http.StatusNotFound {
			return
		}
		if current.Status != http.StatusOK || !safetyStrongETag(current.Header.Get("ETag")) {
			f.h.t.Errorf("adoption cleanup HTTP %d or invalid ETag", current.Status)
			return
		}
		deleted, err := f.h.admin.DeleteAssignment(ctx, ConditionalName{Name: f.assignment, IfMatch: current.Header.Get("ETag")})
		if err != nil || (deleted.Status != http.StatusOK && deleted.Status != http.StatusNoContent) {
			f.h.t.Errorf("adoption cleanup delete HTTP %d: %v", deleted.Status, err)
		}
	})
}

func (f *safetyAdoptionFixture) snapshot() safetyAdoptionSnapshot {
	f.h.t.Helper()
	responses := make([]Response, 0, 3)
	for _, path := range []string{"/bootconfigurations/" + url.PathEscape(f.source.Metadata.UID), "/bootconfigurations", "/bootassignments"} {
		response, err := f.h.admin.Raw(f.h.ctx, Request{Method: http.MethodGet, Path: path})
		safetyStatus(f.h.t, response, err, http.StatusOK)
		responses = append(responses, response)
	}
	if !safetyJSONEqual(responses[0].Body, responses[0].Body) || !safetyCollectionEqual(responses[1].Body, responses[1].Body) || !safetyCollectionEqual(responses[2].Body, responses[2].Body) {
		f.h.t.Fatal("invalid adoption snapshot")
	}
	f.legacyArtifact()
	digest, _ := safetyStoreDigest(f.h.t, f.h.ctx, "main")
	return safetyAdoptionSnapshot{source: responses[0].Body, legacy: responses[1].Body, assignments: responses[2].Body, configurationDigest: digest}
}

func (f *safetyAdoptionFixture) unchanged(before safetyAdoptionSnapshot) {
	f.h.t.Helper()
	after := f.snapshot()
	if before.configurationDigest != after.configurationDigest || !safetyJSONEqual(before.source, after.source) || !safetyCollectionEqual(before.legacy, after.legacy) || !safetyCollectionEqual(before.assignments, after.assignments) {
		f.h.t.Fatal("adoption preflight changed legacy or assignment state")
	}
	series, err := f.h.admin.GetSeries(f.h.ctx, f.series)
	safetyStatus(f.h.t, series, err, http.StatusNotFound)
	assignment, err := f.h.admin.GetAssignment(f.h.ctx, f.assignment)
	safetyStatus(f.h.t, assignment, err, http.StatusNotFound)
}

func (f *safetyAdoptionFixture) run(extra ...string) ([]byte, error) {
	f.h.t.Helper()
	return f.runAt(safetyEnv(f.h.t, "SBX_DECLARATIVE_BOOT_URL"), extra...)
}

func (f *safetyAdoptionFixture) runAt(base string, extra ...string) ([]byte, error) {
	f.h.t.Helper()
	args := []string{"--server", base, "--output=json", "boot-adopt", "--source-uid", f.source.Metadata.UID, "--series", f.series, "--assignment", f.assignment}
	args = append(args, extra...)
	cmd := exec.CommandContext(f.h.ctx, safetyEnv(f.h.t, "SBX_DECLARATIVE_BOOT_CLIENT_BIN"), args...)
	cmd.Env = append(os.Environ(), "BOOT_SERVICE_TOKEN="+f.h.admin.Bearer)
	return runAdoptLeased(f.h.ctx, os.Getenv("SBX_DECLARATIVE_QUIESCENCE_LOCK"), cmd)
}
