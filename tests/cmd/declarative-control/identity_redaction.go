// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package main

import (
	"os"
	"path/filepath"
	"syscall"
)

const identityIncomplete = "identity-redaction-incomplete"

type redactionFence struct {
	root *os.Root
	info os.FileInfo
}

func (c controller) beginRedactionFence() (*redactionFence, error) {
	path := filepath.Join(c.runDir, identityIncomplete)
	if ownedPath(c.runDir, path, true) != nil {
		return nil, errIdentity
	}
	root, err := os.OpenRoot(c.runDir)
	if err != nil {
		return nil, errIdentity
	}
	f, err := root.OpenFile(identityIncomplete, os.O_CREATE|os.O_EXCL|os.O_WRONLY|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		root.Close()
		return nil, errIdentity
	}
	if err := f.Chmod(0600); err != nil {
		f.Close()
		root.Close()
		return nil, errIdentity
	}
	if err := f.Sync(); err != nil {
		f.Close()
		root.Close()
		return nil, errIdentity
	}
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || !ownedFile(info, 0600) || !singleLink(info) {
		f.Close()
		root.Close()
		return nil, errIdentity
	}
	if err := f.Close(); err != nil {
		root.Close()
		return nil, errIdentity
	}
	return &redactionFence{root: root, info: info}, nil
}

func (f *redactionFence) protected() error {
	info, err := f.root.Lstat(identityIncomplete)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !os.SameFile(info, f.info) {
		return errIdentity
	}
	if err := f.root.Remove(identityIncomplete); err != nil {
		return errIdentity
	}
	return nil
}

func (f *redactionFence) Close() error { return f.root.Close() }
