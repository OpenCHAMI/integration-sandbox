// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type asset struct {
	Kind    string `json:"kind"`
	Name    string `json:"name"`
	Project string `json:"project"`
	Service string `json:"service"`
	Path    string `json:"path,omitempty"`
	PID     int    `json:"pid,omitempty"`
	ImageID string `json:"imageID,omitempty"`
}

var assetName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,127}$`)
var errAsset = errors.New("invalid owned asset")

func validImageID(value string) bool {
	digest, ok := strings.CutPrefix(value, "sha256:")
	decoded, err := hex.DecodeString(digest)
	return ok && err == nil && len(decoded) == 32
}

func (c controller) registerAsset(a asset) error {
	if a.Project != c.project || !assetName.MatchString(a.Project) || !assetName.MatchString(a.Name) || !assetName.MatchString(a.Service) {
		return errAsset
	}
	switch a.Kind {
	case "bind-directory":
		if a.Path == "" || a.PID != 0 || a.ImageID != "" || a.Path == c.runDir || ownedPath(c.runDir, a.Path, true) != nil {
			return errAsset
		}
	case "container":
		if a.Path != "" || a.PID != 0 {
			return errAsset
		}
		if a.ImageID != "" && !validImageID(a.ImageID) {
			return errAsset
		}
	case "process":
		if a.Path != "" || a.PID <= 0 || a.ImageID != "" {
			return errAsset
		}
	default:
		return errAsset
	}
	path := filepath.Join(c.runDir, "assets.jsonl")
	if err := ownedPath(c.runDir, path, true); err != nil {
		return err
	}
	data, err := json.Marshal(a)
	if err != nil {
		return errAsset
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0600)
	if err != nil {
		return errAsset
	}
	defer f.Close()
	if _, err := f.Write(append(data, '\n')); err != nil {
		return errAsset
	}
	return nil
}
