// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReceiptRefusesNoOpRestart(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	data := filepath.Join(dir, "data")
	if err := os.Mkdir(data, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "receipts.jsonl")
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	c := controller{project: "sandbox", runDir: dir, receipts: path}
	v := container{ID: "container", Image: "sha256:" + strings.Repeat("a", 64)}
	v.State.Running = true
	v.State.StartedAt = "2026-01-01T00:00:00Z"
	v.Mounts = append(v.Mounts, struct {
		Type        string `json:"Type"`
		Source      string `json:"Source"`
		Destination string `json:"Destination"`
	}{Type: "bind", Source: data, Destination: "/data"})
	if err := c.appendReceipt("service-restart", "boot", "main", v, v); err == nil {
		t.Fatal("no-op restart accepted")
	}
	info, err := os.Stat(path)
	if err != nil || info.Size() != 0 {
		t.Fatal("failed action emitted receipt")
	}
}
