//go:build integration && declarative_boot

// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package declarativeboot

import (
	"encoding/json"
	"net/http"
	"net/url"
	"testing"
)

func TestDeclarativeLifecycle(t *testing.T) {
	t.Run("named-revision-replay-conflict", lifecycleRevisionReplay)
	t.Run("config-content-validation", lifecycleConfigValidation)
	t.Run("routes-additive-legacy", lifecycleRoutesLegacy)
	t.Run("default-latest-pinned-rollout", lifecycleRollout)
	t.Run("all-reference-forms", lifecycleReferences)
	t.Run("unknown-and-no-assignment", lifecycleUnknownLegacy)
	t.Run("selector-strongest-family", lifecycleStrongest)
	t.Run("selector-priority-name-order", lifecyclePriorityName)
	t.Run("profile-and-legacy-selection", lifecycleProfileCase)
	t.Run("cache-hit-and-write-freshness", lifecycleCacheWritesCase)
	t.Run("cache-input-fingerprint", lifecycleFingerprintCase)
	t.Run("concurrent-render-coherence", lifecycleCoherenceCase)
	t.Run("smd-membership-freshness", lifecycleMembershipCase)
	t.Run("smd-refresh-fail-closed", lifecycleRefreshFailureCase)
}

func lifecycleRevisionReplay(t *testing.T) {
	c := lifecycleNew(t)
	response, err := c.boot.CreateConfig(c.ctx, c.config("A"))
	a := c.revision(c.status(response, err, http.StatusCreated))
	if a.Metadata.RevisionName != "release-a" || a.Resource.Spec != c.config("A").Resource.Spec {
		t.Fatal("created A differs from fixture")
	}
	c.pointers(a.Metadata.UID, a.Metadata.UID, 1)
	response, err = c.boot.EnsureRevision(c.ctx, c.series, c.config("B"))
	b := c.revision(c.status(response, err, http.StatusCreated))
	if b.Metadata.RevisionName != "release-b" || b.Resource.Spec != c.config("B").Resource.Spec || b.Metadata.UID == a.Metadata.UID || b.Metadata.Digest == a.Metadata.Digest {
		t.Fatal("created B differs from fixture or A")
	}
	c.pointers(a.Metadata.UID, b.Metadata.UID, 2)
	response, err = c.boot.EnsureRevision(c.ctx, c.series, c.config("B"))
	replay := c.revision(c.status(response, err, http.StatusOK))
	if replay.Metadata.UID != b.Metadata.UID || replay.Metadata.Digest != b.Metadata.Digest || replay.Resource.Spec != c.config("B").Resource.Spec {
		t.Fatal("identical ensure did not replay B")
	}
	c.pointers(a.Metadata.UID, b.Metadata.UID, 2)
	conflict := c.config("B")
	conflict.Resource.Spec = c.config("A").Resource.Spec
	response, err = c.boot.EnsureRevision(c.ctx, c.series, conflict)
	c.status(response, err, http.StatusConflict)
	c.pointers(a.Metadata.UID, b.Metadata.UID, 2)
	response, err = c.boot.GetRevision(c.ctx, RevisionID{Series: c.series, UID: b.Metadata.UID})
	stored := c.revision(c.status(response, err, http.StatusOK))
	if stored.Metadata.UID != b.Metadata.UID || stored.Metadata.Digest != b.Metadata.Digest || stored.Resource.Spec != c.config("B").Resource.Spec {
		t.Fatal("conflicting ensure changed B")
	}
}

func lifecycleConfigValidation(t *testing.T) {
	c := lifecycleNew(t)
	response, err := c.boot.CreateConfig(c.ctx, c.config("A"))
	a := c.revision(c.status(response, err, http.StatusCreated))
	minimal := c.config("B")
	minimal.RevisionName = "minimal"
	minimal.Resource.Spec = BootConfigSpec{Kernel: "/boot/minimal-kernel"}
	response, err = c.boot.EnsureRevision(c.ctx, c.series, minimal)
	minimalRecord := c.revision(c.status(response, err, http.StatusCreated))
	if minimalRecord.Resource.Spec != minimal.Resource.Spec {
		t.Fatal("optional content did not round-trip")
	}
	c.pointers(a.Metadata.UID, minimalRecord.Metadata.UID, 2)
	for _, tc := range []struct {
		name string
		spec json.RawMessage
	}{
		{"missing-kernel", json.RawMessage(`{"initrd":"/boot/initrd"}`)},
		{"invalid-kernel", json.RawMessage(`{"kernel":"://invalid"}`)},
		{"selector", json.RawMessage(`{"kernel":"/boot/kernel","selector":{"groups":["x"]}}`)},
		{"profile", json.RawMessage(`{"kernel":"/boot/kernel","profile":"x"}`)},
		{"priority", json.RawMessage(`{"kernel":"/boot/kernel","priority":9}`)},
		{"overlay", json.RawMessage(`{"kernel":"/boot/kernel","overlay":"x"}`)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body, marshalErr := json.Marshal(struct {
				Metadata Metadata        `json:"metadata"`
				Spec     json.RawMessage `json:"spec"`
			}{Metadata: Metadata{Name: c.series}, Spec: tc.spec})
			if marshalErr != nil {
				t.Fatal(marshalErr)
			}
			response, requestErr := c.boot.Raw(c.ctx, Request{Method: http.MethodPut, Path: "/bootconfigs/" + url.PathEscape(c.series) + "/revisions/invalid-" + tc.name, Headers: http.Header{"Content-Type": {"application/json"}}, Body: body})
			if requestErr != nil {
				t.Fatal(requestErr)
			}
			if response.Status < 400 || response.Status >= 500 {
				t.Fatalf("invalid content accepted or server error: HTTP %d", response.Status)
			}
			c.pointers(a.Metadata.UID, minimalRecord.Metadata.UID, 2)
		})
	}
	mutation, marshalErr := EncodeJSON(c.config("B").Resource)
	if marshalErr != nil {
		t.Fatal(marshalErr)
	}
	response, err = c.boot.Raw(c.ctx, Request{Method: http.MethodPut, Path: "/bootconfigs/" + url.PathEscape(a.Metadata.UID), Headers: http.Header{"Content-Type": {"application/json"}}, Body: mutation})
	c.status(response, err, http.StatusMethodNotAllowed)
	c.pointers(a.Metadata.UID, minimalRecord.Metadata.UID, 2)
	response, err = c.boot.GetRevision(c.ctx, RevisionID{Series: c.series, UID: a.Metadata.UID})
	stored := c.revision(c.status(response, err, http.StatusOK))
	if stored.Metadata.Digest != a.Metadata.Digest || stored.Resource.Spec != c.config("A").Resource.Spec {
		t.Fatal("validation attempts changed immutable A")
	}
}
