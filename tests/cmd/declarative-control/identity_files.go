//go:build go1.25

// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

type identityFiles struct {
	config        *os.Root
	redactions    *os.File
	audit         *os.File
	values        map[string]json.RawMessage
	previousToken string
}

func ownedFile(info os.FileInfo, mode os.FileMode) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Uid == uint32(os.Geteuid()) && info.Mode().Perm() == mode
}

func (c controller) openIdentityFiles(ctx context.Context) (*identityFiles, error) {
	dir := os.Getenv("SBX_DECLARATIVE_BOOT_CONFIG_DIR")
	ledger := os.Getenv("SBX_DECLARATIVE_HSM_BOOTSTRAP_REDACTIONS")
	if dir == c.runDir || ownedPath(c.runDir, dir, false) != nil || ledger != filepath.Join(c.runDir, "credentials.redactions") || ownedPath(c.runDir, ledger, true) != nil {
		return nil, errIdentity
	}
	runInfo, err := os.Lstat(c.runDir)
	if err != nil || !runInfo.IsDir() || !ownedFile(runInfo, 0700) {
		return nil, errIdentity
	}
	dirInfo, err := os.Lstat(dir)
	if err != nil || !dirInfo.IsDir() || !ownedFile(dirInfo, 0755) {
		return nil, errIdentity
	}
	run, err := os.OpenRoot(c.runDir)
	if err != nil {
		return nil, errIdentity
	}
	defer run.Close()
	rel, err := filepath.Rel(c.runDir, dir)
	if err != nil {
		return nil, errIdentity
	}
	config, err := run.OpenRoot(rel)
	if err != nil {
		return nil, errIdentity
	}
	files := &identityFiles{config: config}
	files.audit, err = openIdentityLog(run, "credential-issuance.jsonl")
	if err != nil {
		files.Close()
		return nil, errIdentity
	}
	if err := lockIdentity(ctx, files.audit); err != nil {
		files.Close()
		return nil, errIdentity
	}
	if err := files.loadConfig(dirInfo); err != nil {
		files.Close()
		return nil, errIdentity
	}
	files.redactions, err = openIdentityLog(run, "credentials.redactions")
	if err != nil {
		files.Close()
		return nil, errIdentity
	}
	return files, nil
}

func lockIdentity(ctx context.Context, file *os.File) error {
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return nil
		}
		if err != syscall.EWOULDBLOCK && err != syscall.EAGAIN && err != syscall.EINTR {
			return errIdentity
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (f *identityFiles) loadConfig(expected os.FileInfo) error {
	dir, err := f.config.Open(".")
	if err != nil {
		return errIdentity
	}
	actual, statErr := dir.Stat()
	closeErr := dir.Close()
	if statErr != nil || closeErr != nil || !os.SameFile(actual, expected) {
		return errIdentity
	}
	file, err := f.config.OpenFile("boot.json", os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if errors.Is(err, os.ErrNotExist) {
		f.values = make(map[string]json.RawMessage)
		return nil
	}
	if err != nil {
		return errIdentity
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || !ownedFile(info, 0444) || !singleLink(info) {
		return errIdentity
	}
	data, err := io.ReadAll(io.LimitReader(file, 65537))
	if err != nil || len(data) > 65536 || !uniqueJSON(data) || json.Unmarshal(data, &f.values) != nil || f.values == nil {
		return errIdentity
	}
	if raw, ok := f.values["tokensmith_bootstrap_token"]; ok {
		if json.Unmarshal(raw, &f.previousToken) != nil {
			return errIdentity
		}
	}
	return nil
}

func openIdentityLog(root *os.Root, name string) (*os.File, error) {
	f, err := root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_RDWR|os.O_APPEND|syscall.O_NOFOLLOW, 0600)
	if errors.Is(err, os.ErrExist) {
		f, err = root.OpenFile(name, os.O_RDWR|os.O_APPEND|syscall.O_NOFOLLOW, 0)
	} else if err == nil {
		if err := f.Chmod(0600); err != nil {
			f.Close()
			return nil, errIdentity
		}
	}
	if err != nil {
		return nil, errIdentity
	}
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || !ownedFile(info, 0600) || !singleLink(info) || info.Size() > 16<<20 {
		f.Close()
		return nil, errIdentity
	}
	return f, nil
}

func singleLink(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Nlink == 1
}

func (f *identityFiles) Close() {
	if f.redactions != nil {
		f.redactions.Close()
	}
	if f.audit != nil {
		f.audit.Close()
	}
	if f.config != nil {
		f.config.Close()
	}
}

func (f *identityFiles) publish(token string) error {
	value, err := json.Marshal(token)
	if err != nil {
		return errIdentity
	}
	f.values["tokensmith_bootstrap_token"] = value
	data, err := json.Marshal(f.values)
	if err != nil {
		return errIdentity
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return errIdentity
	}
	name := ".boot-" + hex.EncodeToString(nonce[:]) + ".tmp"
	out, err := f.config.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return errIdentity
	}
	defer f.config.Remove(name)
	if _, err := out.Write(data); err != nil {
		out.Close()
		return errIdentity
	}
	if err := out.Chmod(0444); err != nil {
		out.Close()
		return errIdentity
	}
	if err := out.Sync(); err != nil {
		out.Close()
		return errIdentity
	}
	if err := out.Close(); err != nil {
		return errIdentity
	}
	if err := f.config.Rename(name, "boot.json"); err != nil {
		return errIdentity
	}
	return nil
}
