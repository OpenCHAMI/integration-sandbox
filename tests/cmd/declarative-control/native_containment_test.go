// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func nativeTempDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestRetiredFixtureRejectsSymlinkedStoreBeforeWriting(t *testing.T) {
	for _, target := range []string{"revisions", "bootconfig", "records", "series", "record"} {
		t.Run(target, func(t *testing.T) {
			base := nativeTempDir(t)
			outside := t.TempDir()
			sentinel := filepath.Join(outside, "sentinel.json")
			const original = `{"metadata":{"uid":"source","seriesName":"old","number":1,"digest":"old","createdAt":"2026-01-01T00:00:00Z"},"resource":{"apiVersion":"boot.openchami.io/v1","kind":"BootConfig","metadata":{"name":"old","uid":"u","createdAt":"2026-01-01T00:00:00Z","updatedAt":"2026-01-01T00:00:00Z"},"spec":{"kernel":"https://example.test/kernel"}}}`
			if err := os.WriteFile(sentinel, []byte(original), 0600); err != nil {
				t.Fatal(err)
			}
			root := filepath.Join(base, "revisions", "bootconfig")
			if err := os.MkdirAll(filepath.Join(root, "records"), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(filepath.Join(root, "series"), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, "records", "source.json"), []byte(original), 0600); err != nil {
				t.Fatal(err)
			}
			var path string
			switch target {
			case "revisions":
				path = filepath.Join(base, "revisions")
			case "bootconfig":
				path = root
			case "records", "series":
				path = filepath.Join(root, target)
			case "record":
				path = filepath.Join(root, "records", "z-link.json")
			}
			if target != "record" {
				if err := os.RemoveAll(path); err != nil {
					t.Fatal(err)
				}
			}
			destination := outside
			if target == "record" {
				destination = sentinel
			}
			if err := os.Symlink(destination, path); err != nil {
				t.Fatal(err)
			}
			if _, _, err := makeRetiredFixture(base); err == nil {
				t.Fatal("symlink accepted")
			}
			contents, err := os.ReadDir(outside)
			if err != nil || len(contents) != 1 || contents[0].Name() != "sentinel.json" {
				t.Fatal("outside directory modified")
			}
			data, err := os.ReadFile(sentinel)
			if err != nil || string(data) != original {
				t.Fatal("outside sentinel modified")
			}
			if target == "series" || target == "record" {
				entries, err := os.ReadDir(filepath.Join(root, "records"))
				if err != nil || len(entries) != 1 && target == "series" || len(entries) != 2 && target == "record" {
					t.Fatal("store modified before rejecting symlink")
				}
			}
		})
	}
}

func TestNativeAtomicJSONNeverReplacesExistingFile(t *testing.T) {
	dir := t.TempDir()
	const original = `{"old":"valid"}`
	if err := os.WriteFile(filepath.Join(dir, "record.json"), []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if err := nativeAtomicJSON(root, "record.json", struct {
		New string `json:"new"`
	}{"value"}); err == nil {
		t.Fatal("existing record replaced")
	}
	data, err := os.ReadFile(filepath.Join(dir, "record.json"))
	if err != nil || string(data) != original {
		t.Fatal("existing record changed")
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		t.Fatal("temporary stage left behind")
	}
}

func TestInterruptedNativePublicationKeepsExistingSeriesValid(t *testing.T) {
	dir := t.TempDir()
	const existing = `{"status":{"name":"old","revisionCount":1},"revisionUids":["old"]}`
	if err := os.WriteFile(filepath.Join(dir, "series.json"), []byte(existing), 0600); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if err := nativeAtomicJSON(root, "orphan.json", struct {
		UID string `json:"uid"`
	}{"new"}); err != nil {
		t.Fatal(err)
	}
	if err := nativeAtomicJSON(root, "series.json", struct {
		Name string `json:"name"`
	}{"new"}); err == nil {
		t.Fatal("existing authoritative series replaced")
	}
	series, err := os.ReadFile(filepath.Join(dir, "series.json"))
	if err != nil || string(series) != existing || !json.Valid(series) {
		t.Fatal("existing authoritative series changed")
	}
	orphan, err := os.ReadFile(filepath.Join(dir, "orphan.json"))
	if err != nil || !json.Valid(orphan) {
		t.Fatal("published orphan is incomplete")
	}
}
