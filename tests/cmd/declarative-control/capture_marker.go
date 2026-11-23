// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

type controlActionKey struct{}

func captureMayMutate(ctx context.Context) bool {
	value, _ := ctx.Value(controlActionKey{}).(string)
	switch value {
	case "backup-restore", "broken-reference-fixture":
		return false
	default:
		return true
	}
}

func (c controller) createOfflineMarker() (string, error) {
	path, err := c.capturePath()
	if err != nil {
		return "", err
	}
	dir := path + ".inflight"
	if ownedPath(c.runDir, dir, false) != nil {
		return "", errPath
	}
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0700 {
		return "", errPath
	}
	root, err := c.markerRoot(dir)
	if err != nil {
		return "", errPath
	}
	defer root.Close()
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", errRecovery
	}
	id := hex.EncodeToString(random[:])
	f, err := root.OpenFile(id, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return "", errRecovery
	}
	marker := struct {
		ID   string    `json:"id"`
		PID  int       `json:"pid"`
		Kind string    `json:"kind"`
		Time time.Time `json:"time"`
	}{id, os.Getpid(), "control", time.Now().UTC()}
	if err := json.NewEncoder(f).Encode(marker); err != nil {
		f.Close()
		return "", errRecovery
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return "", errRecovery
	}
	if err := f.Close(); err != nil {
		return "", errRecovery
	}
	return filepath.Join(dir, id), nil
}

func (c controller) clearOfflineMarker(path string) error {
	lock, err := c.capturePath()
	if err != nil || filepath.Dir(path) != lock+".inflight" || ownedPath(c.runDir, path, false) != nil {
		return errPath
	}
	root, err := c.markerRoot(filepath.Dir(path))
	if err != nil {
		return errRecovery
	}
	defer root.Close()
	return root.Remove(filepath.Base(path))
}

func (c controller) markerRoot(dir string) (*os.Root, error) {
	if ownedPath(c.runDir, dir, false) != nil {
		return nil, errPath
	}
	relative, err := filepath.Rel(c.runDir, dir)
	if err != nil {
		return nil, errPath
	}
	run, err := os.OpenRoot(c.runDir)
	if err != nil {
		return nil, errPath
	}
	defer run.Close()
	root, err := run.OpenRoot(relative)
	if err != nil {
		return nil, errPath
	}
	file, err := root.Open(".")
	if err != nil {
		root.Close()
		return nil, errPath
	}
	info, err := file.Stat()
	closeErr := file.Close()
	entry, entryErr := os.Lstat(dir)
	if err != nil || closeErr != nil || entryErr != nil || !info.IsDir() || entry.Mode()&os.ModeSymlink != 0 || !os.SameFile(info, entry) {
		root.Close()
		return nil, errPath
	}
	return root, nil
}
