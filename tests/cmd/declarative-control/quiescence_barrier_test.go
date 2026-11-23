// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/openchami/integration-sandbox/tests/internal/quiescence"
)

func barrierFixture(t *testing.T) (controller, string, *fakeExec) {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "writers.lock")
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SBX_DECLARATIVE_QUIESCENCE_LOCK", path)
	executor := &fakeExec{}
	return controller{runDir: dir, exec: executor}, path, executor
}

func TestAmbiguousWriterBlocksBeforeDocker(t *testing.T) {
	c, path, executor := barrierFixture(t)
	writer, err := quiescence.BeginWrite(context.Background(), path, "http")
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.Abandon(); err != nil {
		t.Fatal(err)
	}
	if err := c.withStopped(context.Background(), container{}, func(string) error { return nil }); err == nil {
		t.Fatal("ambiguous write accepted")
	}
	if executor.calls != 0 {
		t.Fatal("Docker reached before ambiguity rejection")
	}
}

func TestActiveWriterBlocksBeforeDocker(t *testing.T) {
	c, path, executor := barrierFixture(t)
	writer, err := quiescence.BeginWrite(context.Background(), path, "http")
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Complete()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if err := c.withStopped(ctx, container{}, func(string) error { return nil }); err == nil {
		t.Fatal("active writer did not block capture")
	}
	if executor.calls != 0 {
		t.Fatal("Docker reached before writer completion")
	}
}

func TestSuccessfulControlMarkerAllowsNextCapture(t *testing.T) {
	c, path, _ := barrierFixture(t)
	if err := c.withCapture(context.Background(), func() error {
		marker, err := c.createOfflineMarker()
		if err != nil {
			return err
		}
		return c.clearOfflineMarker(marker)
	}); err != nil {
		t.Fatal(err)
	}
	lease, err := quiescence.AcquireCapture(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if err := lease.Close(); err != nil {
		t.Fatal(err)
	}
}
