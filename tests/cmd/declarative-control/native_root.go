//go:build go1.25

// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

func nativeChildRoot(parent *os.Root, name string) (*os.Root, error) {
	info, err := parent.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errPath
	}
	child, err := parent.OpenRoot(name)
	if err != nil {
		return nil, err
	}
	file, err := child.Open(".")
	if err != nil {
		child.Close()
		return nil, err
	}
	opened, statErr := file.Stat()
	closeErr := file.Close()
	current, currentErr := parent.Lstat(name)
	if statErr != nil || closeErr != nil || currentErr != nil || current.Mode()&os.ModeSymlink != 0 || !os.SameFile(info, opened) || !os.SameFile(info, current) {
		child.Close()
		return nil, errPath
	}
	return child, nil
}

func nativeStoreRoots(dir string) (*os.Root, *os.Root, error) {
	if !filepath.IsAbs(dir) || filepath.Clean(dir) != dir {
		return nil, nil, errPath
	}
	root, err := os.OpenRoot(string(filepath.Separator))
	if err != nil {
		return nil, nil, err
	}
	for _, part := range strings.Split(strings.TrimPrefix(dir, string(filepath.Separator)), string(filepath.Separator)) {
		if part == "" {
			continue
		}
		child, childErr := nativeChildRoot(root, part)
		closeErr := root.Close()
		if childErr != nil {
			return nil, nil, errors.Join(childErr, closeErr)
		}
		if closeErr != nil {
			child.Close()
			return nil, nil, closeErr
		}
		root = child
	}
	defer root.Close()
	revisions, err := nativeChildRoot(root, "revisions")
	if err != nil {
		return nil, nil, err
	}
	defer revisions.Close()
	bootconfig, err := nativeChildRoot(revisions, "bootconfig")
	if err != nil {
		return nil, nil, err
	}
	defer bootconfig.Close()
	records, err := nativeChildRoot(bootconfig, "records")
	if err != nil {
		return nil, nil, err
	}
	series, err := nativeChildRoot(bootconfig, "series")
	if err != nil {
		records.Close()
		return nil, nil, err
	}
	return records, series, nil
}

func nativeValidateTree(root *os.Root) error {
	return fs.WalkDir(root.FS(), ".", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		info, err := root.Lstat(path)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || (!info.IsDir() && !info.Mode().IsRegular()) {
			return errPath
		}
		return nil
	})
}

func nativeReadRecord(root *os.Root, name string) ([]byte, error) {
	info, err := root.Lstat(name)
	if err != nil || !info.Mode().IsRegular() {
		return nil, errPath
	}
	f, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return nil, errPath
	}
	current, err := root.Lstat(name)
	if err != nil || !current.Mode().IsRegular() || !os.SameFile(info, current) {
		return nil, errPath
	}
	return io.ReadAll(io.LimitReader(f, 16<<20))
}

func nativeAtomicJSON(root *os.Root, name string, value interface{}) (resultErr error) {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	suffix, err := randomSuffix()
	if err != nil {
		return err
	}
	txnName := ".fixture-" + suffix
	if err := root.Mkdir(txnName, 0700); err != nil {
		return err
	}
	txn, err := root.OpenRoot(txnName)
	if err != nil {
		return errors.Join(err, root.Remove(txnName))
	}
	defer func() {
		for _, path := range []string{"partial", "complete"} {
			if err := txn.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
				resultErr = errors.Join(resultErr, err)
			}
		}
		resultErr = errors.Join(resultErr, txn.Close(), root.Remove(txnName))
	}()
	f, err := txn.OpenFile("partial", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	_, writeErr := f.Write(data)
	syncErr := f.Sync()
	closeErr := f.Close()
	if err := errors.Join(writeErr, syncErr, closeErr); err != nil {
		return err
	}
	if err := txn.Rename("partial", "complete"); err != nil {
		return err
	}
	return root.Link(filepath.Join(txnName, "complete"), name)
}
