// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func assertWatcherFinished(t *testing.T, run string) {
	t.Helper()
	f, err := os.Open(filepath.Join(run, "assets.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var watcher asset
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var a asset
		if json.Unmarshal(scanner.Bytes(), &a) != nil {
			t.Fatal("invalid asset")
		}
		if a.Kind == "process" {
			watcher = a
		}
	}
	if scanner.Err() != nil || watcher.PID <= 1 {
		t.Fatal("watcher not registered")
	}
	timer := time.NewTimer(10 * time.Second)
	ticker := time.NewTicker(25 * time.Millisecond)
	defer timer.Stop()
	defer ticker.Stop()
	for {
		data, err := os.ReadFile(filepath.Join(run, watcher.Name))
		var lease recoveryLease
		if err == nil && json.Unmarshal(data, &lease) == nil && lease.Finished && errors.Is(syscall.Kill(watcher.PID, 0), syscall.ESRCH) {
			return
		}
		select {
		case <-timer.C:
			t.Fatal("watcher did not finish and exit")
		case <-ticker.C:
		}
	}
}
