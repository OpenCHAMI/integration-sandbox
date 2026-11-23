//go:build (darwin || linux) && go1.25

// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package quiescence

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

var ErrInFlight = errors.New("unresolved writer marker")
var ErrClosed = errors.New("lease already closed")
var ErrUnsafePath = errors.New("unsafe quiescence path")

func openLock(ctx context.Context, path string, mode int) (*os.File, *os.Root, string, error) {
	if path == "" {
		return nil, nil, "", ErrUnsafePath
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, nil, "", fmt.Errorf("lock path: %w", err)
	}
	base := filepath.Base(absolute)
	root, err := os.OpenRoot(filepath.Dir(absolute))
	if err != nil {
		return nil, nil, "", fmt.Errorf("open lock directory: %w", err)
	}
	file, err := root.OpenFile(base, os.O_RDWR|syscall.O_NOFOLLOW, 0)
	if err != nil {
		root.Close()
		return nil, nil, "", fmt.Errorf("open existing lock: %w", err)
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || !owned(info) {
		file.Close()
		root.Close()
		return nil, nil, "", ErrUnsafePath
	}
	if err := flock(ctx, file, mode); err != nil {
		file.Close()
		root.Close()
		return nil, nil, "", err
	}
	return file, root, base, nil
}

func owned(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Uid == uint32(os.Geteuid())
}

func flock(ctx context.Context, file *os.File, mode int) error {
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
		}
		err := syscall.Flock(int(file.Fd()), mode|syscall.LOCK_NB)
		if err == nil {
			return nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EAGAIN) && !errors.Is(err, syscall.EINTR) {
			return fmt.Errorf("acquire lock: %w", err)
		}
		timer.Reset(10 * time.Millisecond)
	}
}

func release(file *os.File) error {
	return errors.Join(syscall.Flock(int(file.Fd()), syscall.LOCK_UN), file.Close())
}

func markerRoot(root *os.Root, base string) (*os.Root, error) {
	name := base + ".inflight"
	if err := root.Mkdir(name, 0700); err != nil && !errors.Is(err, os.ErrExist) {
		return nil, fmt.Errorf("create marker directory: %w", err)
	}
	info, err := root.Lstat(name)
	if err != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 || !owned(info) {
		return nil, ErrUnsafePath
	}
	if info.Mode().Perm() != 0700 {
		if err := root.Chmod(name, 0700); err != nil {
			return nil, fmt.Errorf("set marker directory permissions: %w", err)
		}
		info, err = root.Lstat(name)
		if err != nil || !info.IsDir() || info.Mode().Perm() != 0700 || !owned(info) {
			return nil, ErrUnsafePath
		}
	}
	markers, err := root.OpenRoot(name)
	if err != nil {
		return nil, fmt.Errorf("open marker directory: %w", err)
	}
	return markers, nil
}
