//go:build integration

// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type uc8ModernResource struct {
	Metadata struct{ Name, UID string } `json:"metadata"`
	Spec     map[string]any             `json:"spec"`
}

func uc8ModernList(ctx context.Context, uri, token string) ([]uc8ModernResource, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, uri, nil)
	if err != nil {
		return nil, err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("list resources: HTTP %d", resp.StatusCode)
	}
	var resources []uc8ModernResource
	err = json.NewDecoder(resp.Body).Decode(&resources)
	return resources, err
}

func uc8DeleteOwnedModern(ctx context.Context, uri, name, token string) error {
	resources, err := uc8ModernList(ctx, uri, token)
	if err != nil {
		return err
	}
	for _, resource := range resources {
		if resource.Metadata.Name != name {
			continue
		}
		if resource.Metadata.UID == "" {
			return fmt.Errorf("owned resource has no UID")
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodDelete, uri+"/"+url.PathEscape(resource.Metadata.UID), nil)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := httpClient.Do(req)
		if err != nil {
			return err
		}
		resp.Body.Close()
		if resp.StatusCode/100 != 2 && resp.StatusCode != http.StatusNotFound {
			return fmt.Errorf("delete owned resource: HTTP %d", resp.StatusCode)
		}
	}
	return nil
}

func TestUC8_MetadataLifecycle(t *testing.T) {
	for _, test := range []struct {
		kind, collection, field string
		spec                    map[string]any
	}{
		{"defaults", "/clusterdefaultss", "description", map[string]any{"base_url": "http://metadata-service:27792", "cluster_name": "uc8", "description": "original", "short_name": "nid", "nid_length": 4}},
		{"group", "/groups", "template", map[string]any{"template": "#cloud-config\npackage_update: false\n", "metaData": map[string]string{"role": "uc8"}}},
		{"instance", "/instanceinfos", "hostname", map[string]any{"instance_id": "x0c0s0b0n1", "hostname": "uc8.example", "local_hostname": "uc8", "public_keys": []string{}}},
		{"peer", "/wireguardpeers", "description", map[string]any{"public_key": "xTIBA5rboUvnH4htodjb6e6e97QjLERt1NAB4mZqp8Dg=", "allowed_ip": "10.42.1.1/32", "description": "original"}},
	} {
		t.Run(test.kind, func(t *testing.T) {
			r := uc8ContractRunner(t)
			ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
			defer cancel()
			r.ctx = ctx
			require.NoError(t, r.gen_access_token())
			name := fmt.Sprintf("uc8-metadata-%s-%d", test.kind, time.Now().UnixNano())
			uri := Endpoints["metadata"] + test.collection
			before, err := uc8ModernList(ctx, uri, r.access_token)
			require.NoError(t, err)
			for _, item := range before {
				require.NotEqual(t, name, item.Metadata.Name, "refuse existing fixture")
			}
			t.Cleanup(func() {
				cleanupCtx, stop := context.WithTimeout(context.Background(), 15*time.Second)
				defer stop()
				require.NoError(t, uc8DeleteOwnedModern(cleanupCtx, uri, name, r.access_token))
			})
			spec := test.spec
			spec["name"] = name
			command := func(input CLIInput, args ...string) string {
				return uc8InputCommand(t, r, input, append([]string{"metadata", test.kind}, args...)...)
			}
			data, err := json.Marshal(spec)
			require.NoError(t, err)
			command(CLIInput{Stdin: string(data)}, "add", "-d", "@-", "-f", "json")
			listed := command(CLIInput{}, "list", "-F", "json")
			var resources []uc8ModernResource
			require.NoError(t, json.Unmarshal([]byte(listed), &resources))
			var uid string
			for _, item := range resources {
				if item.Metadata.Name == name {
					require.Empty(t, uid)
					uid = item.Metadata.UID
				}
			}
			require.NotEmpty(t, uid)
			read := func() uc8ModernResource {
				var resource uc8ModernResource
				require.NoError(t, json.Unmarshal([]byte(command(CLIInput{}, "get", uid, "-F", "json")), &resource))
				require.Equal(t, uid, resource.Metadata.UID)
				return resource
			}
			require.Equal(t, spec[test.field], read().Spec[test.field])
			spec[test.field] = "uc8-replaced"
			path := uc8PayloadFile(t, "metadata.yaml", spec, "yaml")
			command(CLIInput{Files: map[string]string{path: "/fixtures/metadata.yaml"}}, "set", uid, "-d", "@/fixtures/metadata.yaml", "-f", "yaml")
			require.Equal(t, "uc8-replaced", read().Spec[test.field])
			patch, err := json.Marshal(map[string]string{test.field: "uc8-patched"})
			require.NoError(t, err)
			command(CLIInput{}, "patch", uid, "--patch-method", "rfc7386", "-d", string(patch))
			require.Equal(t, "uc8-patched", read().Spec[test.field])
			independent, err := uc8ModernList(ctx, uri, r.access_token)
			require.NoError(t, err)
			found := false
			for _, item := range independent {
				if item.Metadata.UID == uid {
					found = true
					require.Equal(t, "uc8-patched", item.Spec[test.field])
				}
			}
			require.True(t, found, "independent HTTP must observe CLI mutation")
			command(CLIInput{}, "delete", "--no-confirm", uid)
			result := r.execute(CLIInput{}, "metadata", test.kind, "get", uid, "-F", "json")
			requireUC8CLIFailure(t, r, result)
			after, err := uc8ModernList(ctx, uri, r.access_token)
			require.NoError(t, err)
			for _, item := range after {
				require.NotEqual(t, uid, item.Metadata.UID)
			}
		})
	}
}
