// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

var errPath = errors.New("unsafe owned path")

func ownedPath(root, path string, missing bool) error {
	if !filepath.IsAbs(root) || !filepath.IsAbs(path) || filepath.Clean(root) != root || filepath.Clean(path) != path {
		return errPath
	}
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return errPath
	}
	current := string(filepath.Separator)
	for _, part := range strings.Split(strings.TrimPrefix(path, current), current) {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) && missing {
			return nil
		}
		if err != nil || info.Mode()&os.ModeSymlink != 0 {
			return errPath
		}
	}
	return nil
}

func (c controller) dataDir(v container) (string, error) {
	var source string
	for _, mount := range v.Mounts {
		if mount.Destination != "/data" {
			continue
		}
		if source != "" || mount.Type != "bind" || ownedPath(c.runDir, mount.Source, false) != nil || mount.Source == c.runDir {
			return "", errPath
		}
		source = mount.Source
	}
	if source == "" {
		return "", errPath
	}
	return source, nil
}
