//go:build integration && declarative_boot

// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package declarativeboot

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHTTPHelpersPromotionBodyAndSeries(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			RevisionUID string `json:"revisionUid"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.RevisionUID != "rev-b" || r.Header.Get("If-Match") != `"current"` {
			t.Errorf("incorrect promotion: %+v %v", body, err)
		}
		w.Header().Set("ETag", `"next"`)
		_, _ = w.Write([]byte(`{"name":"os","defaultRevisionUid":"rev-b","latestRevisionUid":"rev-b","revisionCount":2,"updatedAt":"2026-01-01T00:00:00Z"}`))
	}))
	defer server.Close()
	client, err := NewHTTPClient(server.URL, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	response, err := (BootClient{HTTP: client}).PromoteDefault(context.Background(), Promotion{Series: "os", RevisionUID: "rev-b", IfMatch: `"current"`})
	if err != nil {
		t.Fatal(err)
	}
	status, err := DecodeSeries(response)
	if err != nil || status.DefaultRevisionUID != "rev-b" || response.Header.Get("ETag") != `"next"` {
		t.Fatalf("bad series: %+v %v", status, err)
	}
}

func TestHTTPHelpersResolutionAndListDecoders(t *testing.T) {
	metadata := `{"uid":"rev-a","seriesName":"os","revisionName":"a","number":1,"digest":"opaque","createdAt":"2026-01-01T00:00:00Z"}`
	resolved, err := DecodeResolution(Response{Body: []byte(`{"seriesName":"os","selector":"latest","revision":` + metadata + `}`)})
	if err != nil || resolved.Revision.UID != "rev-a" || resolved.Selector != "latest" {
		t.Fatalf("bad resolution: %+v %v", resolved, err)
	}
	list, err := DecodeRevisions(Response{Body: []byte(`[{"metadata":` + metadata + `,"resource":{"metadata":{"name":"os"},"spec":{"kernel":"s3://fixture"}}}]`)})
	if err != nil || len(list) != 1 || list[0].Metadata.UID != "rev-a" {
		t.Fatalf("bad list: %+v %v", list, err)
	}
	for _, body := range []string{"null", `[{"metadata":{}}]`, `{`} {
		if _, err := DecodeRevisions(Response{Body: []byte(body)}); err == nil {
			t.Fatalf("bad list accepted: %s", body)
		}
	}
}

func TestSMDHelpersGroupCreateUpdateAndRead(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_, _ = w.Write([]byte(`{"label":"acceptance-red","description":"fixture","members":{"ids":[]}}`))
			return
		}
		if r.Method == http.MethodPut {
			var members GroupMembers
			if err := json.NewDecoder(r.Body).Decode(&members); err != nil || members.IDs == nil || r.URL.Path != "/hsm/v2/groups/acceptance-red/members" {
				t.Errorf("bad membership replacement: %s %+v %v", r.URL.Path, members, err)
			}
		} else {
			var group Group
			if err := json.NewDecoder(r.Body).Decode(&group); err != nil || group.Label != "acceptance-red" || group.Members.IDs == nil {
				t.Errorf("bad group: %+v %v", group, err)
			}
		}
		w.WriteHeader(201)
	}))
	defer server.Close()
	client, err := NewHTTPClient(server.URL, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	smd := SMDClient{HTTP: client}
	group := Group{Label: "acceptance-red", Description: "fixture", Members: GroupMembers{IDs: []string{}}}
	created, err := smd.CreateGroup(context.Background(), group)
	if err != nil || created.Status != 201 {
		t.Fatalf("create: %v", err)
	}
	updated, err := smd.UpdateGroup(context.Background(), group)
	if err != nil || updated.Status != 201 {
		t.Fatalf("update: %v", err)
	}
	got, err := smd.GetGroup(context.Background(), group.Label)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeGroup(got)
	if err != nil || decoded.Label != group.Label || len(decoded.Members.IDs) != 0 {
		t.Fatalf("read: %+v %v", decoded, err)
	}
}

func TestHTTPHelpersConditionalAssignmentRead(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/bootassignments/by-name/canary" || r.Header.Get("If-None-Match") != `"old"` {
			t.Error("missing read precondition")
		}
		w.WriteHeader(http.StatusNotModified)
	}))
	defer server.Close()
	client, err := NewHTTPClient(server.URL, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	response, err := (BootClient{HTTP: client}).GetAssignmentConditional(context.Background(), ConditionalName{Name: "canary", IfNoneMatch: `"old"`})
	if err != nil || response.Status != http.StatusNotModified {
		t.Fatalf("conditional read: %+v %v", response, err)
	}
}
