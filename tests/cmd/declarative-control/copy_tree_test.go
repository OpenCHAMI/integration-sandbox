// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCopyTreePreservesContentsWithoutSharing(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	dest := filepath.Join(root, "destination")
	if err := os.MkdirAll(filepath.Join(source, "nested"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "nested", "item.json"), []byte(`{"uid":"original"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := copyTree(source, dest); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dest, "nested", "item.json"), []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(source, "nested", "item.json"))
	if err != nil || string(got) != `{"uid":"original"}` {
		t.Fatalf("source changed: %q %v", got, err)
	}
}

func TestCopyTreeRejectsSymlink(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	if err := os.Mkdir(source, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), filepath.Join(source, "link")); err != nil {
		t.Fatal(err)
	}
	if err := copyTree(source, filepath.Join(root, "dest")); err == nil {
		t.Fatal("symlink accepted")
	}
}
