// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package main

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
)

var errStore = errors.New("invalid declarative store")

func digestStore(dir string) (string, error) {
	type entry struct {
		path string
		data []byte
	}
	var entries []entry
	for _, namespace := range []string{"bootassignments", "revisions/bootconfig"} {
		root := filepath.Join(dir, filepath.FromSlash(namespace))
		if namespace == "revisions/bootconfig" {
			parent, err := os.Lstat(filepath.Join(dir, "revisions"))
			if err != nil && !errors.Is(err, os.ErrNotExist) {
				return "", errStore
			}
			if err == nil && (!parent.IsDir() || parent.Mode()&os.ModeSymlink != 0) {
				return "", errStore
			}
		}
		info, err := os.Lstat(root)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return "", errStore
		}
		err = filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil || d.Type()&os.ModeSymlink != 0 {
				return errStore
			}
			if d.IsDir() {
				return nil
			}
			info, err := d.Info()
			if err != nil || !info.Mode().IsRegular() || info.Size() > 16<<20 {
				return errStore
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return errStore
			}
			if !json.Valid(data) {
				return errStore
			}
			rel, err := filepath.Rel(dir, path)
			if err != nil {
				return errStore
			}
			entries = append(entries, entry{filepath.ToSlash(rel), data})
			return nil
		})
		if err != nil {
			return "", err
		}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].path < entries[j].path })
	h := sha256.New()
	for _, e := range entries {
		for _, field := range [][]byte{[]byte(e.path), e.data} {
			var size [8]byte
			binary.BigEndian.PutUint64(size[:], uint64(len(field)))
			if _, err := h.Write(size[:]); err != nil {
				return "", err
			}
			if _, err := h.Write(field); err != nil {
				return "", err
			}
		}
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), nil
}
