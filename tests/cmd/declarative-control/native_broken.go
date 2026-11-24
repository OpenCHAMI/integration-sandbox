// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

type brokenRequest struct {
	AssignmentName string `json:"assignmentName"`
	SeriesName     string `json:"seriesName"`
	RevisionUID    string `json:"revisionUid"`
	NodeIdentifier string `json:"nodeIdentifier"`
}

func breakReference(data string, req brokenRequest) error {
	if req.AssignmentName == "" || req.SeriesName == "" || req.NodeIdentifier == "" || !strings.HasPrefix(req.RevisionUID, "rev_") || !assetName.MatchString(req.RevisionUID) {
		return errStore
	}
	series, err := readNativeSeries(data, req.SeriesName)
	if err != nil {
		return err
	}
	if series.Status.Default != req.RevisionUID || !slices.Contains(series.UIDs, req.RevisionUID) {
		return errStore
	}
	assignments, err := os.ReadDir(filepath.Join(data, "bootassignments"))
	if err != nil {
		return err
	}
	found := false
	for _, entry := range assignments {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		body, err := os.ReadFile(filepath.Join(data, "bootassignments", entry.Name()))
		if err != nil {
			return err
		}
		var a struct {
			Metadata struct {
				Name string `json:"name"`
				UID  string `json:"uid"`
			} `json:"metadata"`
			Spec struct {
				Selector struct {
					Xnames []string `json:"xnames"`
				} `json:"selector"`
				ConfigRef struct {
					Name string `json:"name"`
				} `json:"configRef"`
			} `json:"spec"`
		}
		if json.Unmarshal(body, &a) != nil {
			return errStore
		}
		if a.Metadata.Name == req.AssignmentName && a.Metadata.UID != "" && a.Spec.ConfigRef.Name == req.SeriesName && slices.Contains(a.Spec.Selector.Xnames, req.NodeIdentifier) {
			found = true
		}
	}
	if !found {
		return errStore
	}
	suffix, err := randomSuffix()
	if err != nil {
		return err
	}
	uid := "bootconfiguration-" + suffix
	legacy := struct {
		APIVersion string `json:"apiVersion"`
		Kind       string `json:"kind"`
		Metadata   struct {
			Name      string    `json:"name"`
			UID       string    `json:"uid"`
			CreatedAt time.Time `json:"createdAt"`
			UpdatedAt time.Time `json:"updatedAt"`
		} `json:"metadata"`
		Spec struct {
			Hosts    []string `json:"hosts"`
			Kernel   string   `json:"kernel"`
			Initrd   string   `json:"initrd"`
			Params   string   `json:"params"`
			Priority int      `json:"priority"`
		} `json:"spec"`
		Status struct{} `json:"status"`
	}{APIVersion: "boot.openchami.io/v1", Kind: "BootConfiguration"}
	legacy.Metadata.Name, legacy.Metadata.UID, legacy.Metadata.CreatedAt, legacy.Metadata.UpdatedAt = "legacy-fallback-"+suffix, uid, time.Now().UTC(), time.Now().UTC()
	legacy.Spec.Hosts, legacy.Spec.Kernel, legacy.Spec.Initrd = []string{req.NodeIdentifier}, "https://images.example.invalid/boot-images/sandbox-kernel", "https://images.example.invalid/boot-images/sandbox-initrd"
	legacy.Spec.Params, legacy.Spec.Priority = "console=ttyS0 root=live:s3://boot-images/sandbox-rootfs.squashfs ip=dhcp", 1
	dir := filepath.Join(data, "bootconfigurations")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	if err := writeExclusiveJSON(filepath.Join(dir, uid+".json"), legacy); err != nil {
		return err
	}
	return os.Remove(filepath.Join(data, "revisions", "bootconfig", "records", req.RevisionUID+".json"))
}

func brokenReference(ctx context.Context, c controller, raw json.RawMessage) (json.RawMessage, error) {
	var req brokenRequest
	if err := strictRequest(raw, &req); err != nil {
		return nil, err
	}
	var base string
	err := c.copyStore(ctx, func(cp copiedStore) error {
		if err := breakReference(cp.data, req); err != nil {
			return err
		}
		if _, err := os.Stat(filepath.Join(cp.data, "revisions", "bootconfig", "records", req.RevisionUID+".json")); !os.IsNotExist(err) {
			return errStore
		}
		var err error
		base, _, err = c.startCopy(ctx, cp)
		return err
	})
	if err != nil {
		return nil, err
	}
	return json.Marshal(struct {
		BaseURL            string `json:"baseURL"`
		MissingRevisionUID string `json:"missingRevisionUid"`
	}{base, req.RevisionUID})
}

func init() { registerAction("broken-reference-fixture", brokenReference) }
