// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"path/filepath"
)

type copiedStore struct {
	before  container
	service string
	name    string
	dir     string
	data    string
}

func randomSuffix() (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

func (c controller) copyStore(ctx context.Context, after func(copiedStore) error) error {
	before, err := c.locate(ctx, "boot")
	if err != nil || !before.State.Running || before.State.Paused || c.imageID == "" || before.Image != c.imageID {
		return errDocker
	}
	suffix, err := randomSuffix()
	if err != nil {
		return err
	}
	service := "boot-copy-" + suffix
	name := c.project + "-" + service
	dir := filepath.Join(c.runDir, service)
	if err := c.registerAsset(asset{Kind: "bind-directory", Name: service, Project: c.project, Service: service, Path: dir}); err != nil {
		return err
	}
	if err := os.Mkdir(dir, 0700); err != nil {
		return err
	}
	data := filepath.Join(dir, "data")
	cp := copiedStore{before: before, service: service, name: name, dir: dir, data: data}
	return c.withStoppedAfter(ctx, before, func(source string) error {
		return copyTree(source, data)
	}, func() error { return after(cp) })
}
