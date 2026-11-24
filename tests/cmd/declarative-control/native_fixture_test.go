// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package main

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestRetiredFixtureMatchesPinnedRecordShape(t *testing.T) {
	dir := nativeTempDir(t)
	root := filepath.Join(dir, "revisions", "bootconfig")
	for _, sub := range []string{"records", "series"} {
		if err := os.MkdirAll(filepath.Join(root, sub), 0700); err != nil {
			t.Fatal(err)
		}
	}
	original := []byte(`{"metadata":{"uid":"rev_01h2xcejqtf2nbrexx3vqjhp41","seriesName":"source","number":1,"digest":"sha256:old","createdAt":"2026-01-01T00:00:00Z"},"resource":{"apiVersion":"boot.openchami.io/v1","kind":"BootConfig","metadata":{"name":"source","uid":"bootconfig-12","createdAt":"2026-01-01T00:00:00Z","updatedAt":"2026-01-01T00:00:00Z"},"spec":{"kernel":"https://example.test/kernel","initrd":"https://example.test/initrd","params":"quiet"},"status":{}}}`)
	path := filepath.Join(root, "records", "rev_01h2xcejqtf2nbrexx3vqjhp41.json")
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	name, uid, err := makeRetiredFixture(dir)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != string(original) {
		t.Fatal("source record changed")
	}
	seriesData, err := os.ReadFile(filepath.Join(root, "series", base64.RawURLEncoding.EncodeToString([]byte(name))+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var series struct {
		Status struct {
			Name    string `json:"name"`
			Default string `json:"defaultRevisionUid"`
			Latest  string `json:"latestRevisionUid"`
			Count   int    `json:"revisionCount"`
		} `json:"status"`
		UIDs []string `json:"revisionUids"`
	}
	if json.Unmarshal(seriesData, &series) != nil || series.Status.Name != name || series.Status.Count != 3 || len(series.UIDs) != 3 || series.UIDs[1] != uid || series.Status.Default != series.UIDs[0] || series.Status.Latest != series.UIDs[2] {
		t.Fatal("wrong authoritative series state")
	}
	body, err := os.ReadFile(filepath.Join(root, "records", uid+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var record nativeRecord
	if json.Unmarshal(body, &record) != nil || record.Metadata.UID != uid || record.Metadata.SeriesName != name || record.Metadata.RetiredAt == nil || record.Resource.Metadata.Name != name {
		t.Fatal("invalid retired record")
	}
	canonical := `{"apiVersion":"boot.openchami.io/v1","kind":"BootConfig","metadata":{"name":"` + name + `"},"spec":{"kernel":"https://example.test/kernel","initrd":"https://example.test/initrd","params":"quiet"}}`
	sum := sha256.Sum256([]byte(canonical))
	if record.Metadata.Digest != "sha256:"+hex.EncodeToString(sum[:]) {
		t.Fatal("incorrect canonical digest")
	}
}

func TestBrokenFixturePreservesSeriesAndAssignment(t *testing.T) {
	dir := nativeTempDir(t)
	root := filepath.Join(dir, "revisions", "bootconfig")
	for _, sub := range []string{"records", "series"} {
		if err := os.MkdirAll(filepath.Join(root, sub), 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(dir, "bootassignments"), 0700); err != nil {
		t.Fatal(err)
	}
	uid := "rev_01h2xcejqtf2nbrexx3vqjhp41"
	series := []byte(`{"status":{"name":"chosen","defaultRevisionUid":"` + uid + `"},"revisionUids":["` + uid + `"]}`)
	seriesPath := filepath.Join(root, "series", base64.RawURLEncoding.EncodeToString([]byte("chosen"))+".json")
	assignment := []byte(`{"metadata":{"name":"winner","uid":"ba-01"},"spec":{"selector":{"xnames":["x0c0s0b0n0"]},"configRef":{"name":"chosen"}}}`)
	assignmentPath := filepath.Join(dir, "bootassignments", "ba-01.json")
	for path, data := range map[string][]byte{seriesPath: series, assignmentPath: assignment, filepath.Join(root, "records", uid+".json"): []byte(`{}`)} {
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := breakReference(dir, brokenRequest{"winner", "chosen", uid, "x0c0s0b0n0"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "records", uid+".json")); !os.IsNotExist(err) {
		t.Fatal("winning revision remains")
	}
	for path, want := range map[string][]byte{seriesPath: series, assignmentPath: assignment} {
		got, err := os.ReadFile(path)
		if err != nil || string(got) != string(want) {
			t.Fatal("fixture changed authoritative state")
		}
	}
	files, err := os.ReadDir(filepath.Join(dir, "bootconfigurations"))
	if err != nil || len(files) != 1 {
		t.Fatal("legacy fixture missing")
	}
	legacy, err := os.ReadFile(filepath.Join(dir, "bootconfigurations", files[0].Name()))
	var value struct {
		Kind string `json:"kind"`
		Spec struct {
			Hosts    []string `json:"hosts"`
			Kernel   string   `json:"kernel"`
			Initrd   string   `json:"initrd"`
			Priority int      `json:"priority"`
		} `json:"spec"`
	}
	if err != nil || json.Unmarshal(legacy, &value) != nil || value.Kind != "BootConfiguration" || len(value.Spec.Hosts) != 1 || value.Spec.Hosts[0] != "x0c0s0b0n0" || value.Spec.Priority >= 100 || value.Spec.Kernel != "https://images.example.invalid/boot-images/sandbox-kernel" || value.Spec.Initrd != "https://images.example.invalid/boot-images/sandbox-initrd" {
		t.Fatal("invalid legacy fallback")
	}
}

func TestRetiredFixtureCleansNewRecordsWhenSeriesWriteFails(t *testing.T) {
	dir := nativeTempDir(t)
	records := filepath.Join(dir, "revisions", "bootconfig", "records")
	if err := os.MkdirAll(records, 0700); err != nil {
		t.Fatal(err)
	}
	const original = `{"metadata":{"uid":"existing","seriesName":"source","number":1,"digest":"old","createdAt":"2026-01-01T00:00:00Z"},"resource":{"apiVersion":"boot.openchami.io/v1","kind":"BootConfig","metadata":{"name":"source","uid":"u","createdAt":"2026-01-01T00:00:00Z","updatedAt":"2026-01-01T00:00:00Z"},"spec":{"kernel":"https://example.test/kernel"}}}`
	if err := os.WriteFile(filepath.Join(records, "existing.json"), []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := makeRetiredFixture(dir); err == nil {
		t.Fatal("missing series directory unexpectedly succeeded")
	}
	entries, err := os.ReadDir(records)
	if err != nil || len(entries) != 1 || entries[0].Name() != "existing.json" {
		t.Fatal("failed fixture left orphan records or changed source")
	}
}
