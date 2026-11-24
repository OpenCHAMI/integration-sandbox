// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package main

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
)

type nativeSeries struct {
	Status struct {
		Name    string `json:"name"`
		Default string `json:"defaultRevisionUid"`
		Latest  string `json:"latestRevisionUid"`
		Count   uint64 `json:"revisionCount"`
	} `json:"status"`
	UIDs  []string          `json:"revisionUids"`
	Names map[string]string `json:"revisionNames"`
}

func readNativeSeries(data, name string) (nativeSeries, error) {
	records, root, err := nativeStoreRoots(data)
	if err != nil {
		return nativeSeries{}, err
	}
	defer records.Close()
	defer root.Close()
	sum := sha256.Sum256([]byte(name))
	filename := "sha256." + hex.EncodeToString(sum[:]) + ".json"
	info, err := root.Lstat(filename)
	canonical := true
	if errors.Is(err, os.ErrNotExist) {
		canonical = false
		filename = base64.RawURLEncoding.EncodeToString([]byte(name)) + ".json"
		info, err = root.Lstat(filename)
	}
	if err != nil {
		return nativeSeries{}, err
	}
	if !info.Mode().IsRegular() || info.Size() > 16<<20 {
		return nativeSeries{}, errStore
	}
	file, err := root.Open(filename)
	if err != nil {
		return nativeSeries{}, err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(info, opened) {
		return nativeSeries{}, errStore
	}
	current, err := root.Lstat(filename)
	if err != nil || !current.Mode().IsRegular() || !os.SameFile(opened, current) {
		return nativeSeries{}, errStore
	}
	body, err := io.ReadAll(io.LimitReader(file, (16<<20)+1))
	if err != nil || len(body) > 16<<20 {
		return nativeSeries{}, errStore
	}
	var series nativeSeries
	if json.Unmarshal(body, &series) != nil || series.Status.Name != name {
		return nativeSeries{}, errStore
	}
	if !canonical && series.UIDs == nil {
		seen := make(map[string]bool)
		for _, uid := range []string{series.Status.Default, series.Status.Latest} {
			if uid != "" && !seen[uid] {
				seen[uid] = true
				series.UIDs = append(series.UIDs, uid)
			}
		}
		for _, uid := range series.Names {
			if uid != "" && !seen[uid] {
				seen[uid] = true
				series.UIDs = append(series.UIDs, uid)
			}
		}
	}
	if canonical {
		if series.Status.Count == 0 || len(series.UIDs) == 0 || uint64(len(series.UIDs)) > series.Status.Count {
			return nativeSeries{}, errStore
		}
		uids := make(map[string]bool, len(series.UIDs))
		for _, uid := range series.UIDs {
			if uid == "" || uids[uid] {
				return nativeSeries{}, errStore
			}
			uids[uid] = true
		}
		if !uids[series.Status.Default] || !uids[series.Status.Latest] {
			return nativeSeries{}, errStore
		}
		bound := make(map[string]bool, len(series.Names))
		for revisionName, uid := range series.Names {
			if revisionName == "" || !uids[uid] || bound[uid] {
				return nativeSeries{}, errStore
			}
			bound[uid] = true
		}
	}
	return series, nil
}
