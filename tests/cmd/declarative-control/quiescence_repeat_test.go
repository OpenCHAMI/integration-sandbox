// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/openchami/integration-sandbox/tests/internal/quiescence"
)

func assertStickyMarker(t *testing.T, lock, mode string) {
	t.Helper()
	lease, err := quiescence.AcquireCapture(context.Background(), lock)
	if lease != nil {
		lease.Close()
	}
	if !errors.Is(err, quiescence.ErrInFlight) {
		t.Fatal("capture did not reject an in-flight marker")
	}
	entries, err := os.ReadDir(lock + ".inflight")
	if err != nil || len(entries) == 0 {
		t.Fatal("marker missing after recovery")
	}
	if mode == "orphan-marker" {
		if _, err := os.Stat(filepath.Join(lock+".inflight", "sticky")); err != nil {
			t.Fatal("original marker was removed")
		}
	}
}

func repeatSnapshot(t *testing.T, control string, env []string, f *quiesceFake, run string) {
	t.Helper()
	f.mu.Lock()
	f.stopStarted = make(chan struct{})
	f.release = make(chan struct{})
	f.restarted = make(chan struct{})
	f.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, control, "snapshot-store", `{"instance":"main"}`)
	cmd.Env = env
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	waitQuiesceEvent(t, f.stopStarted)
	close(f.release)
	waitQuiesceEvent(t, f.restarted)
	if err := cmd.Wait(); err != nil {
		t.Fatal("second snapshot failed")
	}
	assertWatcherFinished(t, run)
}
