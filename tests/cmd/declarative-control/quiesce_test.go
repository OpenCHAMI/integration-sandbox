// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCleanStopRejectsKilledAndOOM(t *testing.T) {
	for _, tc := range []struct {
		name               string
		exit               int
		oom, running, want bool
	}{
		{"clean", 0, false, false, true},
		{"forced", 137, false, false, false},
		{"oom", 0, true, false, false},
		{"running", 0, false, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := container{}
			v.stopStateComplete = true
			v.State.ExitCode, v.State.OOMKilled, v.State.Running = tc.exit, tc.oom, tc.running
			if cleanStop(v) != tc.want {
				t.Fatal("incorrect stop classification")
			}
		})
	}
}

func TestCleanStopRejectsMissingStateFields(t *testing.T) {
	if cleanStop(container{}) {
		t.Fatal("missing Docker exit status accepted")
	}
}

func TestRecoveryLeaseFinishesAndCannotBeReused(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	data := filepath.Join(dir, "data")
	if err := os.Mkdir(data, 0700); err != nil {
		t.Fatal(err)
	}
	image := "sha256:" + strings.Repeat("a", 64)
	v := container{ID: "owned", Image: image, Config: containerConfig{Labels: map[string]string{"com.docker.compose.service": "boot"}}}
	v.State.Running, v.State.StartedAt = true, "2026-01-01T00:00:00Z"
	v.Mounts = append(v.Mounts, struct {
		Type        string `json:"Type"`
		Source      string `json:"Source"`
		Destination string `json:"Destination"`
	}{"bind", data, "/data"})
	c := controller{project: "sandbox", runDir: dir, imageID: image}
	path, err := c.newLease(v)
	if err != nil {
		t.Fatal(err)
	}
	lease, err := c.readLease(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := markLeaseFinished(dir, path, lease); err != nil {
		t.Fatal(err)
	}
	if _, err := c.readLease(path); err == nil {
		t.Fatal("finished lease reused")
	}
}

func TestRecoveryLockExcludesConcurrentWatcher(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	c := controller{runDir: dir}
	first, err := c.lockRecovery(context.Background(), "boot")
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	if second, err := c.lockRecovery(ctx, "boot"); err == nil {
		second.Close()
		t.Fatal("concurrent watcher acquired lock")
	}
}
