//go:build integration && declarative_boot

// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package declarativeboot

import (
	"context"
	"net/http"
	"net/url"
	"testing"
	"time"
)

func safetyDuplicateRename(t *testing.T) {
	f := safetyFixtureNew(t)
	before := f.snapshot()
	stored, _, _ := safetyReadAssignment(t, f.ctx, f.client, f.assignment.Metadata.Name)
	response := f.raw(http.MethodPost, "/bootassignments", "", f.encode(f.assignment))
	safetyStatus(t, response, nil, http.StatusConflict)
	f.unchanged(before)
	newName := safetyName(t)
	renamed := f.assignment
	renamed.Metadata.Name = newName
	uidPath := "/bootassignments/" + url.PathEscape(stored.Metadata.UID)
	namePath := "/bootassignments/by-name/" + url.PathEscape(stored.Metadata.Name)
	for _, path := range []string{uidPath, namePath} {
		response = f.raw(http.MethodPut, path, before.assignETag, f.encode(renamed))
		safetyStatus(t, response, nil, http.StatusConflict)
		f.unchanged(before)
		response, err := f.client.GetAssignment(f.ctx, newName)
		safetyStatus(t, response, err, http.StatusNotFound)
	}
	changedUID := f.assignment
	changedUID.Metadata.UID = "different-uid"
	response = f.raw(http.MethodPut, uidPath, before.assignETag, f.encode(changedUID))
	if response.Status != http.StatusBadRequest && response.Status != http.StatusConflict && response.Status != http.StatusUnprocessableEntity {
		t.Fatalf("UID mutation returned HTTP %d", response.Status)
	}
	f.unchanged(before)
	response, err := f.client.Raw(f.ctx, Request{Method: http.MethodGet, Path: "/bootassignments"})
	safetyStatus(t, response, err, http.StatusOK)
	list, err := DecodeJSON[[]BootAssignment](response)
	if err != nil || list == nil {
		t.Fatal("invalid assignment collection")
	}
	count := 0
	for _, item := range list {
		if item.Metadata.Name == stored.Metadata.Name {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("duplicate POST produced %d records for the original name", count)
	}
	created := f.assignment
	created.Metadata.Name = safetyName(t)
	created.Spec.Selector = &Selector{Xnames: []string{"x999c0s0b0n0"}}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		current, getErr := f.client.GetAssignment(ctx, created.Metadata.Name)
		if getErr != nil {
			t.Errorf("cleanup collection-created assignment: %v", getErr)
			return
		}
		if current.Status == http.StatusNotFound {
			return
		}
		if current.Status != http.StatusOK || !safetyStrongETag(current.Header.Get("ETag")) {
			t.Errorf("cleanup collection-created assignment: HTTP %d or invalid ETag", current.Status)
			return
		}
		deleted, deleteErr := f.client.DeleteAssignment(ctx, ConditionalName{Name: created.Metadata.Name, IfMatch: current.Header.Get("ETag")})
		if deleteErr != nil || (deleted.Status != http.StatusOK && deleted.Status != http.StatusNoContent) {
			t.Errorf("cleanup collection-created assignment: HTTP %d, error %v", deleted.Status, deleteErr)
		}
	})
	response = f.raw(http.MethodPost, "/bootassignments", "", f.encode(created))
	safetyStatus(t, response, nil, http.StatusCreated)
	createdResult, err := DecodeAssignment(response)
	if err != nil || createdResult.Metadata.Name != created.Metadata.Name || createdResult.Metadata.UID == stored.Metadata.UID {
		t.Fatal("collection POST did not create an independent assignment")
	}
	after := f.snapshot()
	if !safetyJSONEqual(before.assignment, after.assignment) || before.assignETag != after.assignETag || !safetyJSONEqual(before.series, after.series) || before.seriesETag != after.seriesETag {
		t.Fatal("independent POST changed the original assignment or series")
	}
	f.artifact(safetyArtifactA)
}
