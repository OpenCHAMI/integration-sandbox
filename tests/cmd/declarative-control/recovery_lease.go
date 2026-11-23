//go:build go1.25

// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"time"
)

type recoveryLease struct {
	Project     string    `json:"project"`
	Service     string    `json:"service"`
	ContainerID string    `json:"containerID"`
	ImageID     string    `json:"imageID"`
	DataDir     string    `json:"dataDir"`
	StartedAt   string    `json:"startedAt"`
	OwnerPID    int       `json:"ownerPID"`
	Expires     time.Time `json:"expires"`
	Finished    bool      `json:"finished"`
}

var errRecovery = errors.New("owned recovery failed")

func (c controller) newLease(v container) (string, error) {
	dir, err := c.dataDir(v)
	if err != nil {
		return "", err
	}
	service := v.Config.Labels["com.docker.compose.service"]
	if service != "boot" && service != "boot-auth-off" {
		return "", errRecovery
	}
	if !v.State.Running || v.State.Paused || v.Image != c.imageID || !validImageID(v.Image) {
		return "", errRecovery
	}
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", errRecovery
	}
	name := "recovery-" + hex.EncodeToString(random[:]) + ".json"
	if _, err := time.Parse(time.RFC3339Nano, v.State.StartedAt); err != nil {
		return "", errRecovery
	}
	value := recoveryLease{Project: c.project, Service: service, ContainerID: v.ID, ImageID: v.Image, DataDir: dir, StartedAt: v.State.StartedAt, OwnerPID: os.Getpid(), Expires: time.Now().Add(3 * time.Minute).UTC()}
	data, err := json.Marshal(value)
	if err != nil {
		return "", errRecovery
	}
	if ownedPath(c.runDir, c.runDir, false) != nil {
		return "", errPath
	}
	root, err := os.OpenRoot(c.runDir)
	if err != nil {
		return "", errRecovery
	}
	defer root.Close()
	f, err := root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return "", errRecovery
	}
	if _, err := f.Write(data); err != nil {
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
	return filepath.Join(c.runDir, name), nil
}

func (c controller) readLease(path string) (recoveryLease, error) {
	var value recoveryLease
	if filepath.Dir(path) != c.runDir || ownedPath(c.runDir, path, false) != nil {
		return value, errPath
	}
	root, err := os.OpenRoot(c.runDir)
	if err != nil {
		return value, errRecovery
	}
	defer root.Close()
	file, err := root.Open(filepath.Base(path))
	if err != nil {
		return value, errRecovery
	}
	defer file.Close()
	info, err := file.Stat()
	entry, entryErr := os.Lstat(path)
	if err != nil || entryErr != nil || !info.Mode().IsRegular() || entry.Mode()&os.ModeSymlink != 0 || !os.SameFile(info, entry) {
		return value, errRecovery
	}
	data, err := io.ReadAll(io.LimitReader(file, 8193))
	if err != nil || len(data) > 8192 || json.Unmarshal(data, &value) != nil {
		return value, errRecovery
	}
	if value.Finished || value.Project != c.project || (value.Service != "boot" && value.Service != "boot-auth-off") || value.ImageID != c.imageID || value.OwnerPID <= 1 || !validImageID(value.ImageID) || value.ContainerID == "" || ownedPath(c.runDir, value.DataDir, false) != nil || value.DataDir == c.runDir {
		return value, errRecovery
	}
	if _, err := time.Parse(time.RFC3339Nano, value.StartedAt); err != nil {
		return value, errRecovery
	}
	return value, nil
}

func markLeaseFinished(rootPath, path string, value recoveryLease) error {
	if filepath.Dir(path) != rootPath || ownedPath(rootPath, path, false) != nil {
		return errRecovery
	}
	value.Finished = true
	data, err := json.Marshal(value)
	if err != nil {
		return errRecovery
	}
	root, err := os.OpenRoot(rootPath)
	if err != nil {
		return errRecovery
	}
	defer root.Close()
	name := filepath.Base(path)
	temporary := name + ".finished"
	f, err := root.OpenFile(temporary, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return errRecovery
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return errRecovery
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return errRecovery
	}
	if err := f.Close(); err != nil {
		return errRecovery
	}
	return root.Rename(temporary, name)
}
