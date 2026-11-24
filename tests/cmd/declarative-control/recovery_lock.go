// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

func (c controller) lockRecovery(ctx context.Context, service string) (*os.File, error) {
	if service != "boot" && service != "boot-auth-off" {
		return nil, errRecovery
	}
	name := "recovery-" + service + ".lock"
	path := filepath.Join(c.runDir, name)
	if ownedPath(c.runDir, path, true) != nil {
		return nil, errPath
	}
	root, err := os.OpenRoot(c.runDir)
	if err != nil {
		return nil, errRecovery
	}
	defer root.Close()
	f, err := root.OpenFile(name, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, errRecovery
	}
	info, err := f.Stat()
	entry, entryErr := os.Lstat(path)
	if err != nil || entryErr != nil || !info.Mode().IsRegular() || entry.Mode()&os.ModeSymlink != 0 || !os.SameFile(info, entry) {
		f.Close()
		return nil, errRecovery
	}
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		if ctx.Err() != nil {
			f.Close()
			return nil, ctx.Err()
		}
		err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return f, nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EAGAIN) {
			f.Close()
			return nil, errRecovery
		}
		select {
		case <-ctx.Done():
			f.Close()
			return nil, ctx.Err()
		case <-ticker.C:
		}
	}
}
