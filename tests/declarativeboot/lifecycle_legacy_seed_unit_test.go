//go:build integration && declarative_boot

// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package declarativeboot

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestLifecycleHelpersLegacyNodeOwnership(t *testing.T) {
	for _, mode := range []string{"create", "restore", "duplicate", "changed"} {
		t.Run(mode, func(t *testing.T) {
			type record struct {
				Metadata struct {
					Name string `json:"name"`
					UID  string `json:"uid"`
				} `json:"metadata"`
				Spec json.RawMessage `json:"spec"`
			}
			original := json.RawMessage(`{"xname":"x1c0s0b0n0","bootMac":"02:aa:bb:cc:dd:01","nid":101,"subRole":"preserve","groups":["old"]}`)
			sentinel := record{Spec: json.RawMessage(`{"xname":"x0c0s0b0n0","nid":42}`)}
			sentinel.Metadata.UID = "sentinel-uid"
			records := []record{sentinel}
			if mode != "create" {
				item := record{Spec: original}
				item.Metadata.Name = "existing"
				item.Metadata.UID = "existing-uid"
				records = append(records, item)
				if mode == "duplicate" {
					item.Metadata.UID = "other-uid"
					records = append(records, item)
				}
			}
			posts, deletes := 0, 0
			var mu sync.Mutex
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				switch {
				case r.Method == http.MethodGet && r.URL.Path == "/nodes":
					if records == nil {
						records = []record{}
					}
					if err := json.NewEncoder(w).Encode(records); err != nil {
						t.Error(err)
					}
				case r.Method == http.MethodPost && r.URL.Path == "/nodes":
					var item record
					if err := json.NewDecoder(r.Body).Decode(&item); err != nil {
						t.Error(err)
						return
					}
					item.Metadata.UID = "owned-uid"
					records = append(records, item)
					posts++
					w.WriteHeader(http.StatusCreated)
					if err := json.NewEncoder(w).Encode(item); err != nil {
						t.Error(err)
					}
				case strings.HasPrefix(r.URL.Path, "/nodes/"):
					uid := strings.TrimPrefix(r.URL.Path, "/nodes/")
					index := -1
					for i := range records {
						if records[i].Metadata.UID == uid {
							index = i
							break
						}
					}
					if index < 0 {
						w.WriteHeader(http.StatusNotFound)
						return
					}
					switch r.Method {
					case http.MethodGet:
						if err := json.NewEncoder(w).Encode(records[index]); err != nil {
							t.Error(err)
						}
					case http.MethodPut:
						var update struct {
							Spec json.RawMessage `json:"spec"`
						}
						if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
							t.Error(err)
							return
						}
						records[index].Spec = update.Spec
						if err := json.NewEncoder(w).Encode(records[index]); err != nil {
							t.Error(err)
						}
					case http.MethodDelete:
						records = append(records[:index], records[index+1:]...)
						deletes++
						w.WriteHeader(http.StatusNoContent)
					default:
						w.WriteHeader(http.StatusMethodNotAllowed)
					}
				default:
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			defer server.Close()
			client, err := NewHTTPClient(server.URL, time.Second)
			if err != nil {
				t.Fatal(err)
			}
			fixture := lifecycleLegacySeedInput{node: lifecycleNode{xname: "x1c0s0b0n0", mac: "02:00:00:00:00:01", nid: 10000}, groups: []string{"test-group"}}
			cleanup, err := lifecycleSeedLegacyNode(context.Background(), BootClient{HTTP: client}, fixture)
			if mode == "duplicate" {
				mu.Lock()
				defer mu.Unlock()
				if err == nil || posts != 0 || deletes != 0 {
					t.Fatal("duplicate xnames were not rejected without mutation")
				}
				return
			}
			if err != nil || cleanup == nil {
				t.Fatalf("seed failed: %v", err)
			}
			mu.Lock()
			var seeded lifecycleLegacyNodeSpec
			if len(records) != 2 || json.Unmarshal(records[1].Spec, &seeded) != nil || seeded.Xname != fixture.node.xname || seeded.NID != fixture.node.nid || seeded.BootMAC != fixture.node.mac || !reflect.DeepEqual(seeded.Groups, fixture.groups) {
				mu.Unlock()
				t.Fatal("seeded node does not match SMD fixture")
			}
			if mode == "changed" {
				records[1].Spec = json.RawMessage(`{"xname":"x1c0s0b0n0","nid":999}`)
			}
			mu.Unlock()
			if mode == "changed" {
				if err := cleanup(context.Background()); err == nil {
					t.Fatal("cleanup overwrote an externally changed spec")
				}
				mu.Lock()
				defer mu.Unlock()
				if posts != 0 || deletes != 0 || string(records[1].Spec) != `{"xname":"x1c0s0b0n0","nid":999}` {
					t.Fatal("cleanup changed foreign state")
				}
				return
			}
			if err := cleanup(context.Background()); err != nil {
				t.Fatalf("cleanup failed: %v", err)
			}
			mu.Lock()
			defer mu.Unlock()
			if len(records) == 0 || records[0].Metadata.UID != "sentinel-uid" {
				t.Fatal("sentinel node was modified")
			}
			if mode == "create" && (posts != 1 || deletes != 1 || len(records) != 1) {
				t.Fatal("created node was not removed by its owned UID")
			}
			if mode == "restore" {
				var got, want map[string]json.RawMessage
				if len(records) != 2 || json.Unmarshal(records[1].Spec, &got) != nil || json.Unmarshal(original, &want) != nil || !reflect.DeepEqual(got, want) || posts != 0 || deletes != 0 {
					t.Fatal("preexisting node spec was not restored")
				}
			}
		})
	}
}
