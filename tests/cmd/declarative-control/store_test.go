// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDigestExactBytesAndScoped(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bootassignments")
	if err := os.MkdirAll(path, 0700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(path, "item.json")
	if err := os.WriteFile(file, []byte(`{"b":2,"a":1}`), 0600); err != nil {
		t.Fatal(err)
	}
	first, err := digestStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte("{\n \"a\": 1, \"b\": 2}"), 0600); err != nil {
		t.Fatal(err)
	}
	second, err := digestStore(dir)
	if err != nil || first == second {
		t.Fatal("different JSON bytes must produce different digests")
	}
	if err := os.WriteFile(file, []byte(`{"a":3,"b":2}`), 0600); err != nil {
		t.Fatal(err)
	}
	third, err := digestStore(dir)
	if err != nil || third == first {
		t.Fatal("assignment changes must change the digest")
	}
}

func TestEmptyNamespacesHashAsEmptyInventory(t *testing.T) {
	dir := t.TempDir()
	before, err := digestStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "revisions", "bootconfig"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "bootassignments"), 0700); err != nil {
		t.Fatal(err)
	}
	after, err := digestStore(dir)
	if err != nil || before != after || after != "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855" {
		t.Fatal("empty namespace must hash as empty inventory")
	}
}

func TestDigestIncludesOrphanRevision(t *testing.T) {
	dir := t.TempDir()
	before, err := digestStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	records := filepath.Join(dir, "revisions", "bootconfig", "records")
	if err := os.MkdirAll(records, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(records, "orphan.json"), []byte(`{"orphan":true}`), 0600); err != nil {
		t.Fatal(err)
	}
	after, err := digestStore(dir)
	if err != nil || before == after {
		t.Fatal("orphan record did not change digest")
	}
}

func TestDigestRejectsCorruptionAndSymlink(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bootassignments")
	if err := os.MkdirAll(path, 0700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(path, "bad.json")
	if err := os.WriteFile(file, []byte(`{`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := digestStore(dir); err == nil {
		t.Fatal("corrupt JSON accepted")
	}
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), filepath.Join(path, "link")); err != nil {
		t.Fatal(err)
	}
	if _, err := digestStore(dir); err == nil {
		t.Fatal("symlink accepted")
	}
}
