// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package main

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"
)

type nativeResource struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Metadata   struct {
		Name        string            `json:"name"`
		UID         string            `json:"uid"`
		Labels      map[string]string `json:"labels,omitempty"`
		Annotations map[string]string `json:"annotations,omitempty"`
		CreatedAt   time.Time         `json:"createdAt"`
		UpdatedAt   time.Time         `json:"updatedAt"`
	} `json:"metadata"`
	Spec struct {
		Kernel string `json:"kernel"`
		Initrd string `json:"initrd,omitempty"`
		Params string `json:"params,omitempty"`
	} `json:"spec"`
	Status json.RawMessage `json:"status,omitempty"`
}

type nativeRecord struct {
	Metadata struct {
		UID          string     `json:"uid"`
		SeriesName   string     `json:"seriesName"`
		RevisionName string     `json:"revisionName,omitempty"`
		Number       uint64     `json:"number"`
		Digest       string     `json:"digest"`
		CreatedAt    time.Time  `json:"createdAt"`
		RetiredAt    *time.Time `json:"retiredAt,omitempty"`
	} `json:"metadata"`
	Resource nativeResource `json:"resource"`
}

func nativeDigest(resource nativeResource) (string, error) {
	projection := struct {
		APIVersion string `json:"apiVersion"`
		Kind       string `json:"kind"`
		Metadata   struct {
			Name        string            `json:"name"`
			Labels      map[string]string `json:"labels,omitempty"`
			Annotations map[string]string `json:"annotations,omitempty"`
		} `json:"metadata"`
		Spec interface{} `json:"spec"`
	}{APIVersion: resource.APIVersion, Kind: resource.Kind, Spec: resource.Spec}
	projection.Metadata.Name = resource.Metadata.Name
	projection.Metadata.Labels = resource.Metadata.Labels
	projection.Metadata.Annotations = resource.Metadata.Annotations
	data, err := json.Marshal(projection)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func writeExclusiveJSON(path string, value interface{}) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	_, writeErr := f.Write(data)
	closeErr := f.Close()
	if writeErr != nil || closeErr != nil {
		return errors.Join(writeErr, closeErr, os.Remove(path))
	}
	return nil
}

func makeRetiredFixture(dir string) (string, string, error) {
	records, seriesRoot, err := nativeStoreRoots(dir)
	if err != nil {
		return "", "", err
	}
	defer records.Close()
	defer seriesRoot.Close()
	if err := nativeValidateTree(records); err != nil {
		return "", "", err
	}
	if err := nativeValidateTree(seriesRoot); err != nil {
		return "", "", err
	}
	dirFile, err := records.Open(".")
	if err != nil {
		return "", "", err
	}
	entries, readErr := dirFile.ReadDir(-1)
	closeErr := dirFile.Close()
	if err := errors.Join(readErr, closeErr); err != nil {
		return "", "", err
	}
	var example nativeRecord
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		data, readErr := nativeReadRecord(records, entry.Name())
		if readErr != nil {
			return "", "", readErr
		}
		if json.Unmarshal(data, &example) == nil && example.Resource.Kind == "BootConfig" && example.Resource.Spec.Kernel != "" {
			break
		}
		example = nativeRecord{}
	}
	if example.Resource.Spec.Kernel == "" {
		return "", "", errStore
	}
	suffix, err := randomSuffix()
	if err != nil {
		return "", "", err
	}
	name := "retired-fixture-" + suffix
	now := time.Now().UTC()
	example.Resource.Metadata.Name = name
	digest, err := nativeDigest(example.Resource)
	if err != nil {
		return "", "", err
	}
	uids := make([]string, 3)
	for i := range uids {
		uids[i], err = nativeRevisionUID()
		if err != nil {
			return "", "", err
		}
		item := example
		item.Metadata.UID, item.Metadata.SeriesName, item.Metadata.RevisionName = uids[i], name, ""
		item.Metadata.Number, item.Metadata.Digest, item.Metadata.CreatedAt, item.Metadata.RetiredAt = uint64(i+1), digest, now, nil
		if i == 1 {
			item.Metadata.RetiredAt = &now
		}
		if err := nativeAtomicJSON(records, uids[i]+".json", item); err != nil {
			return "", "", err
		}
	}
	series := struct {
		Status struct {
			Name    string    `json:"name"`
			Default string    `json:"defaultRevisionUid"`
			Latest  string    `json:"latestRevisionUid"`
			Count   int       `json:"revisionCount"`
			Updated time.Time `json:"updatedAt"`
		} `json:"status"`
		UIDs           []string        `json:"revisionUids"`
		ResourceStatus json.RawMessage `json:"resourceStatus,omitempty"`
	}{UIDs: uids, ResourceStatus: example.Resource.Status}
	series.Status.Name, series.Status.Default, series.Status.Latest, series.Status.Count, series.Status.Updated = name, uids[0], uids[2], 3, now
	path := base64.RawURLEncoding.EncodeToString([]byte(name)) + ".json"
	if err := nativeAtomicJSON(seriesRoot, path, series); err != nil {
		return "", "", err
	}
	return name, uids[1], nil
}
