//go:build integration && declarative_boot

// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package declarativeboot

import (
	"context"
	"net/http"
	"reflect"
	"testing"
)

func TestDeclarativeSafety(t *testing.T) {
	t.Run("assignment-shape-validation", safetyAssignmentShape)
	t.Run("malformed-input-rejection", safetyMalformedInput)
	t.Run("promotion-preconditions-race", safetyPromotionRace)
	t.Run("invalid-references", safetyInvalidReferences)
	t.Run("named-create-race", safetyNamedCreateRace)
	t.Run("named-update-delete-preconditions", safetyNamedPreconditions)
	t.Run("named-update-race", safetyNamedUpdateRace)
	t.Run("uid-update-delete-race", safetyUIDRace)
	t.Run("duplicate-and-rename", safetyDuplicateRename)
	t.Run("status-and-alias-boundary", safetyStatusAlias)
	t.Run("auth-role-matrix", safetyRoleMatrix)
	t.Run("auth-invalid-claims", safetyInvalidClaims)
	t.Run("auth-unavailable-legacy-public", safetyAuthOff)
	t.Run("latest-proposer-guard", safetyLatestProposer)
	t.Run("latest-follower-create-race", safetyLatestFollowerRace)
	t.Run("retirement-all-aliases", safetyRetirementAliases)
	t.Run("broken-winning-reference", safetyBrokenWinningReference)
	t.Run("legacy-adoption-dry-run", safetyLegacyAdoptionDryRun)
	t.Run("legacy-adoption-replay", safetyLegacyAdoptionReplay)
	t.Run("legacy-adoption-unsafe-preflight", safetyLegacyAdoptionUnsafePreflight)
	t.Run("durable-restart-recreation", safetyDurableRestartRecreation)
	t.Run("matching-version-backup-restore", safetyMatchingVersionBackupRestore)
}

func safetyNamedCreateRace(t *testing.T) {
	f := safetyFixtureSeeded(t)
	ctx, client, assignment := f.ctx, f.client, f.assignment
	jobs := make([]func(context.Context) (Response, error), 4)
	for i := range jobs {
		jobs[i] = func(ctx context.Context) (Response, error) {
			return client.ApplyAssignment(ctx, ConditionalAssignment{Resource: assignment, IfNoneMatch: "*"})
		}
	}
	results := safetyRace(ctx, jobs)
	winner := safetyCheckRace(t, results, http.StatusCreated)
	created, err := DecodeAssignment(results[winner].response)
	if err != nil {
		t.Fatalf("decode winning create: %v", err)
	}
	createdETag := safetyETag(t, results[winner].response)
	stored, etag, _ := safetyReadAssignment(t, ctx, client, assignment.Metadata.Name)
	if created.Metadata.UID != stored.Metadata.UID || createdETag != etag || !reflect.DeepEqual(assignment.Spec, stored.Spec) {
		t.Fatal("winning assignment was not retained under its name")
	}
	response, err := client.Raw(ctx, Request{Method: http.MethodGet, Path: "/bootassignments"})
	safetyStatus(t, response, err, http.StatusOK)
	assignments, err := DecodeJSON[[]BootAssignment](response)
	if err != nil || assignments == nil {
		t.Fatalf("decode assignment collection: %v", err)
	}
	count := 0
	for _, value := range assignments {
		if value.Metadata.Name == assignment.Metadata.Name {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("found %d assignments with winning name; want one", count)
	}
	f.artifact(safetyArtifactA)
}

func safetyNamedPreconditions(t *testing.T) {
	f := safetyFixtureNew(t)
	ctx, client, assignment := f.ctx, f.client, f.assignment
	state := f.snapshot()
	baseline, etag, before := safetyReadAssignment(t, ctx, client, assignment.Metadata.Name)
	response, err := client.ApplyAssignment(ctx, ConditionalAssignment{Resource: assignment, IfMatch: etag})
	safetyStatus(t, response, err, http.StatusOK)
	replayed, err := DecodeAssignment(response)
	if err != nil || replayed.Metadata.UID != baseline.Metadata.UID || safetyETag(t, response) != etag {
		t.Fatal("same-spec replay changed UID or ETag")
	}
	f.unchanged(state)
	changed := assignment
	priority := 99
	changed.Spec.Priority = &priority
	for _, condition := range []string{"", safetyStale(etag), "*"} {
		response, err = client.ApplyAssignment(ctx, ConditionalAssignment{Resource: changed, IfMatch: condition})
		safetyStatus(t, response, err, http.StatusPreconditionFailed)
		_, currentETag, current := safetyReadAssignment(t, ctx, client, assignment.Metadata.Name)
		safetySameAssignment(t, before, current, etag, currentETag)
		f.unchanged(state)
		response, err = client.DeleteAssignment(ctx, ConditionalName{Name: assignment.Metadata.Name, IfMatch: condition})
		safetyStatus(t, response, err, http.StatusPreconditionFailed)
		_, currentETag, current = safetyReadAssignment(t, ctx, client, assignment.Metadata.Name)
		safetySameAssignment(t, before, current, etag, currentETag)
		f.unchanged(state)
	}
	response, err = client.ApplyAssignment(ctx, ConditionalAssignment{Resource: changed, IfMatch: etag})
	safetyStatus(t, response, err, http.StatusOK)
	updated, updatedETag, _ := safetyReadAssignment(t, ctx, client, assignment.Metadata.Name)
	if updated.Metadata.UID != baseline.Metadata.UID || updated.Spec.Priority == nil || *updated.Spec.Priority != priority || updatedETag == etag || safetyETag(t, response) != updatedETag {
		t.Fatal("guarded update did not commit the desired spec under the original UID")
	}
	f.artifact(safetyArtifactA)
	response, err = client.DeleteAssignment(ctx, ConditionalName{Name: assignment.Metadata.Name, IfMatch: updatedETag})
	if err != nil || (response.Status != http.StatusOK && response.Status != http.StatusNoContent) {
		t.Fatalf("guarded delete: HTTP %d, error %v", response.Status, err)
	}
	response, err = client.GetAssignment(ctx, assignment.Metadata.Name)
	safetyStatus(t, response, err, http.StatusNotFound)
}

func safetyNamedUpdateRace(t *testing.T) {
	f := safetyFixtureNew(t)
	ctx, client, assignment := f.ctx, f.client, f.assignment
	initial, etag, _ := safetyReadAssignment(t, ctx, client, assignment.Metadata.Name)
	first, second := assignment, assignment
	firstPriority, secondPriority := 99, 98
	first.Spec.Priority = &firstPriority
	second.Spec.Priority = &secondPriority
	options := []BootAssignment{first, second}
	jobs := make([]func(context.Context) (Response, error), len(options))
	for i, option := range options {
		jobs[i] = func(ctx context.Context) (Response, error) {
			return client.ApplyAssignment(ctx, ConditionalAssignment{Resource: option, IfMatch: etag})
		}
	}
	results := safetyRace(ctx, jobs)
	winner := safetyCheckRace(t, results, http.StatusOK)
	stored, newETag, _ := safetyReadAssignment(t, ctx, client, assignment.Metadata.Name)
	if stored.Metadata.UID != initial.Metadata.UID || stored.Spec.Priority == nil || *stored.Spec.Priority != *options[winner].Spec.Priority || newETag == etag {
		t.Fatal("stored assignment does not match the single winning update")
	}
	if safetyETag(t, results[winner].response) != newETag {
		t.Fatal("winning response does not identify the committed version")
	}
	f.artifact(safetyArtifactA)
}
