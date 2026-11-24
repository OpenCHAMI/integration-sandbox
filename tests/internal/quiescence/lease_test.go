//go:build darwin || linux

// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package quiescence

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func lockFixture(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "writers.lock")
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestMarkerContainsOnlyOpaqueMetadata(t *testing.T) {
	path := lockFixture(t)
	lease, err := BeginWrite(context.Background(), path, "http")
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Complete()
	entries, err := os.ReadDir(path + ".inflight")
	if err != nil || len(entries) != 1 {
		t.Fatalf("marker list: %v", err)
	}
	body, err := os.ReadFile(filepath.Join(path+".inflight", entries[0].Name()))
	if err != nil {
		t.Fatal(err)
	}
	var marker struct {
		ID   string    `json:"id"`
		PID  int       `json:"pid"`
		Kind string    `json:"kind"`
		Time time.Time `json:"time"`
	}
	if err := json.Unmarshal(body, &marker); err != nil || marker.ID != entries[0].Name() || len(marker.ID) != 32 || marker.PID != os.Getpid() || marker.Kind != "http" || marker.Time.IsZero() {
		t.Fatalf("invalid marker: %+v %v", marker, err)
	}
	info, err := entries[0].Info()
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("marker permissions: %v %v", info, err)
	}
}

func TestSharedWritersAndExclusiveCapture(t *testing.T) {
	path := lockFixture(t)
	first, err := BeginWrite(context.Background(), path, "http")
	if err != nil {
		t.Fatal(err)
	}
	second, err := BeginWrite(context.Background(), path, "cli")
	if err != nil {
		t.Fatal(err)
	}
	if err := second.Complete(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	if capture, err := AcquireCapture(ctx, path); !errors.Is(err, context.DeadlineExceeded) {
		if capture != nil {
			capture.Close()
		}
		t.Fatalf("capture must wait for first writer: %v", err)
	}
	if err := first.Complete(); err != nil {
		t.Fatal(err)
	}
	capture, err := AcquireCapture(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	ctx2, cancel2 := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel2()
	if lease, err := BeginWrite(ctx2, path, "http"); !errors.Is(err, context.DeadlineExceeded) {
		if lease != nil {
			lease.Abandon()
		}
		t.Fatalf("writer bypassed capture: %v", err)
	}
	if err := capture.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestAbandonedWriterBlocksCaptureButNotRecovery(t *testing.T) {
	path := lockFixture(t)
	lease, err := BeginWrite(context.Background(), path, "http")
	if err != nil {
		t.Fatal(err)
	}
	if err := lease.Abandon(); err != nil {
		t.Fatal(err)
	}
	if capture, err := AcquireCapture(context.Background(), path); err == nil {
		capture.Close()
		t.Fatal("abandoned write allowed capture")
	}
	recovery, err := AcquireRecovery(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if err := recovery.Close(); err != nil {
		t.Fatal(err)
	}
	if capture, err := AcquireCapture(context.Background(), path); err == nil {
		capture.Close()
		t.Fatal("recovery cleared ambiguity")
	}
}

func TestRejectSymlinkAndInvalidKind(t *testing.T) {
	path := lockFixture(t)
	link := path + "-link"
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if lease, err := BeginWrite(context.Background(), link, "http"); err == nil {
		lease.Abandon()
		t.Fatal("symlink lock accepted")
	}
	if lease, err := BeginWrite(context.Background(), path, "https://secret"); err == nil {
		lease.Abandon()
		t.Fatal("sensitive marker kind accepted")
	}
}

func TestRejectSymlinkMarkerDirectory(t *testing.T) {
	path := lockFixture(t)
	if err := os.Symlink(t.TempDir(), path+".inflight"); err != nil {
		t.Fatal(err)
	}
	if lease, err := BeginWrite(context.Background(), path, "http"); err == nil {
		lease.Abandon()
		t.Fatal("symlink marker directory accepted")
	}
	if capture, err := AcquireCapture(context.Background(), path); err == nil {
		capture.Close()
		t.Fatal("capture accepted symlink marker directory")
	}
}
