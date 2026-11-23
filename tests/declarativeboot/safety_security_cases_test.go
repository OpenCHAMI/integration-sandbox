//go:build integration && declarative_boot

// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package declarativeboot

import (
	"net/http"
	"net/url"
	"testing"
	"time"
)

func safetyRoleMatrix(t *testing.T) {
	f := safetyFixtureNew(t)
	reader := f.role([]string{"read:boot"})
	proposer := f.role([]string{"read:boot", "propose:boot"})
	proposeOnly := f.role([]string{"propose:boot"})
	promoteOnly := f.role([]string{"promote:boot"})
	b := f.ensureB(proposeOnly)
	confirmed, err := reader.GetRevision(f.ctx, RevisionID{Series: f.assignment.Spec.ConfigRef.Name, UID: b.Metadata.UID})
	safetyStatus(t, confirmed, err, http.StatusOK)
	before := f.snapshot()
	for _, route := range f.securityRoutes(b.Metadata.UID) {
		for _, path := range append([]string{route.request.Path}, safetyAliasPaths(route.request.Path)...) {
			route.request.Path = path
			if route.scope == "read" {
				for _, client := range []BootClient{reader, proposer, f.client} {
					response, err := client.Raw(f.ctx, route.request)
					safetyRequireResponse(t, route, response, err, http.StatusOK)
				}
				response, err := promoteOnly.Raw(f.ctx, route.request)
				safetyRequireResponse(t, route, response, err, http.StatusForbidden)
			} else {
				denied := []BootClient{reader}
				if route.scope == "promote" {
					denied = append(denied, proposer)
				} else {
					denied = append(denied, promoteOnly)
				}
				for _, client := range denied {
					response, err := client.Raw(f.ctx, route.request)
					safetyRequireResponse(t, route, response, err, http.StatusForbidden)
					f.unchanged(before)
				}
			}
		}
	}
	for _, request := range []Request{
		{Method: http.MethodDelete, Path: "/bootconfigs/" + url.PathEscape(f.assignment.Spec.ConfigRef.Name)},
		{Method: http.MethodPut, Path: "/bootconfigs/" + url.PathEscape(b.Metadata.UID), Body: f.configFor(f.assignment.Spec.ConfigRef.Name, safetyArtifactB)},
		{Method: http.MethodPatch, Path: "/bootconfigs/" + url.PathEscape(b.Metadata.UID), Body: []byte(`{"kernel":"/other"}`)},
	} {
		for _, path := range append([]string{request.Path}, safetyAliasPaths(request.Path)...) {
			request.Path = path
			for _, client := range []BootClient{reader, proposer, promoteOnly, f.client} {
				response, err := client.Raw(f.ctx, request)
				if err != nil || (response.Status != http.StatusMethodNotAllowed && response.Status != http.StatusForbidden) {
					t.Fatalf("unsupported mutation returned HTTP %d", response.Status)
				}
				f.unchanged(before)
			}
		}
	}
	changed := f.assignment
	priority := 99
	changed.Spec.Priority = &priority
	response, err := promoteOnly.ApplyAssignment(f.ctx, ConditionalAssignment{Resource: changed, IfMatch: before.assignETag})
	safetyStatus(t, response, err, http.StatusOK)
	current, _, _ := safetyReadAssignment(t, f.ctx, f.client, changed.Metadata.Name)
	if current.Spec.Priority == nil || *current.Spec.Priority != priority {
		t.Fatal("promoter assignment update did not commit")
	}
	f.artifact(safetyArtifactA)
	seriesResponse, err := reader.GetSeries(f.ctx, changed.Spec.ConfigRef.Name)
	safetyStatus(t, seriesResponse, err, http.StatusOK)
	response, err = promoteOnly.PromoteDefault(f.ctx, Promotion{Series: changed.Spec.ConfigRef.Name, RevisionUID: b.Metadata.UID, IfMatch: safetyETag(t, seriesResponse)})
	safetyStatus(t, response, err, http.StatusOK)
	f.artifact(safetyArtifactB)
}

func safetyInvalidClaims(t *testing.T) {
	f := safetyFixtureNew(t)
	before := f.snapshot()
	keys, _ := f.keys()
	changed := f.assignment
	priority := 99
	changed.Spec.Priority = &priority
	for _, kind := range []string{"wrong-issuer", "wrong-audience", "expired", "wrong-algorithm", "bad-signature"} {
		client := BootClient{HTTP: f.client.HTTP, Bearer: f.claimFixture(kind, keys)}
		response, err := client.GetSeries(f.ctx, f.assignment.Spec.ConfigRef.Name)
		safetyStatus(t, response, err, http.StatusUnauthorized)
		response, err = client.ApplyAssignment(f.ctx, ConditionalAssignment{Resource: changed, IfMatch: before.assignETag})
		safetyStatus(t, response, err, http.StatusUnauthorized)
		f.unchanged(before)
	}
}

func safetyAuthOff(t *testing.T) {
	f := safetyFixtureNew(t)
	before := f.snapshot()
	base := safetyEnv(t, "SBX_DECLARATIVE_AUTH_OFF_URL")
	off, err := NewHTTPClient(base, 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if off.base.Host == f.client.HTTP.base.Host && off.base.Scheme == f.client.HTTP.base.Scheme {
		t.Fatal("auth-off instance must be separate")
	}
	path := "/bootscript?" + url.Values{"host": {safetyNode}}.Encode()
	legacy := BootConfigSpec{Kernel: "https://images.example.invalid/boot-images/sandbox-kernel", Initrd: "https://images.example.invalid/boot-images/sandbox-initrd", Params: "console=ttyS0 root=live:s3://boot-images/sandbox-rootfs.squashfs ip=dhcp"}
	bootBefore, err := off.Do(f.ctx, Request{Method: http.MethodGet, Path: path})
	safetyStatus(t, bootBefore, err, http.StatusOK)
	if !safetyArtifactTuple(string(bootBefore.Body), legacy) {
		t.Fatal("auth-off legacy boot fixture is unavailable")
	}
	legacyBefore, err := off.Do(f.ctx, Request{Method: http.MethodGet, Path: "/bootconfigurations"})
	safetyStatus(t, legacyBefore, err, http.StatusOK)
	mainDigest, mainReceipt := safetyStoreDigest(t, f.ctx, "main")
	offDigest, offReceipt := safetyStoreDigest(t, f.ctx, "auth-off")
	if mainReceipt.SourceVolumeID == offReceipt.SourceVolumeID || mainReceipt.BeforeContainerID == offReceipt.BeforeContainerID {
		t.Fatal("auth-off store is not independently owned")
	}
	for _, request := range []Request{
		{Method: http.MethodGet, Path: "/bootconfigs/" + url.PathEscape(f.assignment.Spec.ConfigRef.Name)},
		{Method: http.MethodPost, Path: "/bootconfigs?revisionName=release-a", Headers: http.Header{"Content-Type": {"application/json"}}, Body: f.configFor(safetyName(t), safetyArtifactA)},
		{Method: http.MethodPost, Path: "/bootassignments", Headers: http.Header{"Content-Type": {"application/json"}}, Body: f.encode(f.assignment)},
	} {
		response, reqErr := off.Do(f.ctx, request)
		safetyStatus(t, response, reqErr, http.StatusServiceUnavailable)
		currentDigest, currentReceipt := safetyStoreDigest(t, f.ctx, "auth-off")
		if currentDigest != offDigest || currentReceipt.SourceVolumeID != offReceipt.SourceVolumeID || currentReceipt.BeforeContainerID != offReceipt.BeforeContainerID {
			t.Fatal("auth-off denial changed or replaced its declarative store")
		}
		legacyAfter, reqErr := off.Do(f.ctx, Request{Method: http.MethodGet, Path: "/bootconfigurations"})
		safetyStatus(t, legacyAfter, reqErr, http.StatusOK)
		if !safetyJSONEqual(legacyBefore.Body, legacyAfter.Body) {
			t.Fatal("auth-off refusal changed legacy records")
		}
		bootAfter, reqErr := off.Do(f.ctx, Request{Method: http.MethodGet, Path: path})
		safetyStatus(t, bootAfter, reqErr, http.StatusOK)
		if !safetyArtifactTuple(string(bootAfter.Body), legacy) {
			t.Fatal("auth-off refusal changed public legacy delivery")
		}
	}
	currentMain, _ := safetyStoreDigest(t, f.ctx, "main")
	if currentMain != mainDigest {
		t.Fatal("auth-off probes changed the main declarative store")
	}
	f.unchanged(before)
}
