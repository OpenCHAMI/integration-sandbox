// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/openchami/integration-sandbox/tests/internal/quiescence"
)

func prepareCaptureFixture(t *testing.T, run string) string {
	t.Helper()
	lock := filepath.Join(run, "writers.lock")
	if err := os.WriteFile(lock, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(lock+".inflight", 0700); err != nil {
		t.Fatal(err)
	}
	return lock
}

func assertCaptureFinished(t *testing.T, run, lock string) {
	t.Helper()
	assertWatcherFinished(t, run)
	entries, err := os.ReadDir(lock + ".inflight")
	if err != nil || len(entries) != 0 {
		t.Fatal("capture left an in-flight marker")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	lease, err := quiescence.AcquireCapture(ctx, lock)
	if err != nil {
		t.Fatalf("capture lease still held: %v", err)
	}
	if err := lease.Close(); err != nil {
		t.Fatal(err)
	}
}

func captureHeld(lock string) bool {
	f, err := os.OpenFile(lock, os.O_RDWR, 0)
	if err != nil {
		return false
	}
	defer f.Close()
	err = syscall.Flock(int(f.Fd()), syscall.LOCK_SH|syscall.LOCK_NB)
	if err == nil {
		return false
	}
	return errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN)
}
