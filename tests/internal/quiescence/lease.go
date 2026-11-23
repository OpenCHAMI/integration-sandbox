//go:build darwin || linux

// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package quiescence

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"syscall"
	"time"
)

type WriteLease struct {
	mu      sync.Mutex
	file    *os.File
	markers *os.Root
	marker  string
	closed  bool
}
type CaptureLease struct {
	mu     sync.Mutex
	file   *os.File
	closed bool
}
type markerData struct {
	ID   string    `json:"id"`
	PID  int       `json:"pid"`
	Kind string    `json:"kind"`
	Time time.Time `json:"time"`
}

func BeginWrite(ctx context.Context, path, kind string) (*WriteLease, error) {
	if kind == "" || len(kind) > 32 || strings.Trim(kind, "abcdefghijklmnopqrstuvwxyz0123456789_-") != "" {
		return nil, fmt.Errorf("invalid marker kind: %w", ErrUnsafePath)
	}
	file, root, base, err := openLock(ctx, path, syscall.LOCK_SH)
	if err != nil {
		return nil, err
	}
	markers, err := markerRoot(root, base)
	closeErr := root.Close()
	if err != nil || closeErr != nil {
		if markers != nil {
			markers.Close()
		}
		release(file)
		return nil, errors.Join(err, closeErr)
	}
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		markers.Close()
		release(file)
		return nil, fmt.Errorf("marker randomness: %w", err)
	}
	id := hex.EncodeToString(random[:])
	marker, err := markers.OpenFile(id, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		markers.Close()
		release(file)
		return nil, fmt.Errorf("create marker: %w", err)
	}
	err = marker.Chmod(0600)
	if err == nil {
		var info os.FileInfo
		info, err = marker.Stat()
		if err == nil && (!info.Mode().IsRegular() || info.Mode().Perm() != 0600 || !owned(info)) {
			err = ErrUnsafePath
		}
	}
	if err == nil {
		err = json.NewEncoder(marker).Encode(markerData{ID: id, PID: os.Getpid(), Kind: kind, Time: time.Now().UTC()})
	}
	err = errors.Join(err, marker.Close())
	if err != nil {
		removeErr := markers.Remove(id)
		markers.Close()
		release(file)
		return nil, errors.Join(err, removeErr)
	}
	return &WriteLease{file: file, markers: markers, marker: id}, nil
}

func (l *WriteLease) Complete() error { return l.finish(true) }
func (l *WriteLease) Abandon() error  { return l.finish(false) }
func (l *WriteLease) finish(complete bool) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return ErrClosed
	}
	l.closed = true
	var err error
	if complete {
		err = l.markers.Remove(l.marker)
	}
	return errors.Join(err, l.markers.Close(), release(l.file))
}

func AcquireCapture(ctx context.Context, path string) (*CaptureLease, error) {
	return acquireExclusive(ctx, path, true)
}
func AcquireRecovery(ctx context.Context, path string) (*CaptureLease, error) {
	return acquireExclusive(ctx, path, false)
}
func acquireExclusive(ctx context.Context, path string, check bool) (*CaptureLease, error) {
	file, root, base, err := openLock(ctx, path, syscall.LOCK_EX)
	if err != nil {
		return nil, err
	}
	if check {
		markers, openErr := markerRoot(root, base)
		if openErr != nil {
			root.Close()
			release(file)
			return nil, openErr
		}
		dir, readErr := markers.Open(".")
		if readErr == nil {
			_, readErr = dir.Readdirnames(1)
			readErr = errors.Join(normalizeEmpty(readErr), dir.Close())
		}
		readErr = errors.Join(readErr, markers.Close())
		if readErr != nil {
			root.Close()
			release(file)
			return nil, readErr
		}
	}
	if err := root.Close(); err != nil {
		release(file)
		return nil, err
	}
	return &CaptureLease{file: file}, nil
}

func normalizeEmpty(err error) error {
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err == nil {
		return ErrInFlight
	}
	return err
}
func (l *CaptureLease) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return ErrClosed
	}
	l.closed = true
	return release(l.file)
}
