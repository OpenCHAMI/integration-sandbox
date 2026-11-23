//go:build integration && declarative_boot

// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package declarativeboot

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestSMDHelpersGroupConflictReconciliation(t *testing.T) {
	for _, tc := range []struct {
		name    string
		stored  string
		success bool
	}{
		{"matching", `{"label":"acceptance-red","description":"fixture","members":{"ids":["x0c0s0b0n0"]}}`, true},
		{"stale", `{"label":"acceptance-red","description":"fixture","members":{"ids":["x0c0s1b0n0"]}}`, false},
		{"different label case", `{"label":"Acceptance-Red","description":"fixture","members":{"ids":["x0c0s0b0n0"]}}`, false},
		{"different member case", `{"label":"acceptance-red","description":"fixture","members":{"ids":["X0c0s0b0n0"]}}`, false},
		{"missing members", `{"label":"acceptance-red"}`, false},
		{"missing description", `{"label":"acceptance-red","members":{"ids":["x0c0s0b0n0"]}}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.Method {
				case "POST":
					var group Group
					if err := json.NewDecoder(r.Body).Decode(&group); err != nil || group.Members.IDs[0] != "x0c0s0b0n0" {
						t.Errorf("bad payload: %v", err)
					}
					w.WriteHeader(409)
				case "GET":
					if r.URL.Path != "/hsm/v2/groups/acceptance-red" {
						t.Errorf("bad path %s", r.URL.Path)
					}
					_, _ = w.Write([]byte(tc.stored))
				default:
					t.Errorf("unexpected method %s", r.Method)
				}
			}))
			defer server.Close()
			client, err := NewHTTPClient(server.URL, time.Second)
			if err != nil {
				t.Fatal(err)
			}
			err = (SMDClient{HTTP: client}).EnsureGroup(context.Background(), Group{Label: "acceptance-red", Description: "fixture", Members: GroupMembers{IDs: []string{"x0c0s0b0n0"}}})
			if (err == nil) != tc.success {
				t.Fatalf("success=%v error=%v", tc.success, err)
			}
			if (tc.name == "different label case" || tc.name == "different member case") && !errors.Is(err, ErrGroupConflict) {
				t.Fatalf("expected group conflict, got %v", err)
			}
		})
	}
}

func TestSMDHelpersGroupMissingDescription(t *testing.T) {
	_, err := DecodeGroup(Response{Body: []byte(`{"label":"acceptance-red","members":{"ids":[]}}`)})
	if err == nil {
		t.Fatal("group without required description accepted")
	}
}
