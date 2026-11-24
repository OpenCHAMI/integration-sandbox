// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
)

func TestBrokenFixtureSeriesLayouts(t *testing.T) {
	const uid = "rev_01h2xcejqtf2nbrexx3vqjhp41"
	const other = "rev_01h2xcejqtf2nbrexx3vqjhp42"
	const canonical = "sha256.7be403e3cc82262248913b70f596a31b93f656e0da8ce5d1bf757c63ef7a8db4.json"
	valid := `{"status":{"name":"chosen","defaultRevisionUid":"` + uid + `","latestRevisionUid":"` + uid + `","revisionCount":1},"revisionUids":["` + uid + `"]}`
	stale := `{"status":{"name":"chosen","defaultRevisionUid":"` + other + `","latestRevisionUid":"` + other + `","revisionCount":1},"revisionUids":["` + other + `"]}`
	wrong := `{"status":{"name":"Chosen","defaultRevisionUid":"` + uid + `","latestRevisionUid":"` + uid + `","revisionCount":1},"revisionUids":["` + uid + `"]}`
	duplicate := `{"status":{"name":"chosen","defaultRevisionUid":"` + uid + `","latestRevisionUid":"` + uid + `","revisionCount":2},"revisionUids":["` + uid + `","` + uid + `"]}`
	for _, tc := range []struct {
		name, canon, legacy, node string
		success                   bool
	}{
		{"canonical only", valid, "", "", true},
		{"legacy only", "", valid, "", true},
		{"legacy omitted uid list", "", `{"status":{"name":"chosen","defaultRevisionUid":"` + uid + `","latestRevisionUid":"` + uid + `","revisionCount":1}}`, "", true},
		{"same name canonical and legacy", valid, valid, "", true},
		{"canonical overrides stale legacy", valid, stale, "", true},
		{"canonical malformed", "{", valid, "", false},
		{"canonical incomplete", `{"status":{"name":"chosen"}}`, valid, "", false},
		{"canonical wrong name", wrong, valid, "", false},
		{"canonical duplicate uids", duplicate, valid, "", false},
		{"canonical missing latest", `{"status":{"name":"chosen","defaultRevisionUid":"` + uid + `","latestRevisionUid":"` + other + `","revisionCount":1},"revisionUids":["` + uid + `"]}`, valid, "", false},
		{"canonical duplicate bindings", `{"status":{"name":"chosen","defaultRevisionUid":"` + uid + `","latestRevisionUid":"` + uid + `","revisionCount":1},"revisionUids":["` + uid + `"],"revisionNames":{"one":"` + uid + `","two":"` + uid + `"}}`, valid, "", false},
		{"canonical dangling symlink", "", valid, "dangling", false},
		{"canonical live symlink", "", valid, "symlink", false},
		{"canonical directory", "", valid, "directory", false},
		{"canonical oversized", "", valid, "oversized", false},
		{"missing both", "", "", "", false},
		{"legacy case alias", "", wrong, "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := nativeTempDir(t)
			source, copied := filepath.Join(root, "source"), filepath.Join(root, "copy")
			for _, path := range []string{filepath.Join(source, "revisions", "bootconfig", "records"), filepath.Join(source, "revisions", "bootconfig", "series"), filepath.Join(source, "bootassignments")} {
				if err := os.MkdirAll(path, 0700); err != nil {
					t.Fatal(err)
				}
			}
			seriesDir := filepath.Join(source, "revisions", "bootconfig", "series")
			legacyName := base64.RawURLEncoding.EncodeToString([]byte("chosen")) + ".json"
			for name, data := range map[string]string{canonical: tc.canon, legacyName: tc.legacy} {
				if data != "" {
					if err := os.WriteFile(filepath.Join(seriesDir, name), []byte(data), 0600); err != nil {
						t.Fatal(err)
					}
				}
			}
			record := filepath.Join(source, "revisions", "bootconfig", "records", uid+".json")
			assignment := filepath.Join(source, "bootassignments", "ba-01.json")
			if err := os.WriteFile(record, []byte(`{"uid":"preserved"}`), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(assignment, []byte(`{"metadata":{"name":"winner","uid":"ba-01"},"spec":{"selector":{"xnames":["x0c0s0b0n0"]},"configRef":{"name":"chosen"}}}`), 0600); err != nil {
				t.Fatal(err)
			}
			if err := copyTree(source, copied); err != nil {
				t.Fatal(err)
			}
			seriesDir = filepath.Join(copied, "revisions", "bootconfig", "series")
			outside := filepath.Join(root, "outside.json")
			if err := os.WriteFile(outside, []byte(valid), 0600); err != nil {
				t.Fatal(err)
			}
			switch tc.node {
			case "dangling":
				if err := os.Symlink(filepath.Join(root, "missing"), filepath.Join(seriesDir, canonical)); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				if err := os.Symlink(outside, filepath.Join(seriesDir, canonical)); err != nil {
					t.Fatal(err)
				}
			case "directory":
				if err := os.Mkdir(filepath.Join(seriesDir, canonical), 0700); err != nil {
					t.Fatal(err)
				}
			case "oversized":
				file, err := os.Create(filepath.Join(seriesDir, canonical))
				if err != nil {
					t.Fatal(err)
				}
				if err := file.Truncate((16 << 20) + 1); err != nil {
					t.Fatal(err)
				}
				if err := file.Close(); err != nil {
					t.Fatal(err)
				}
			}
			err := breakReference(copied, brokenRequest{"winner", "chosen", uid, "x0c0s0b0n0"})
			if (err == nil) != tc.success {
				t.Fatalf("unexpected fixture result: %v", err)
			}
			copiedRecord := filepath.Join(copied, "revisions", "bootconfig", "records", uid+".json")
			data, readErr := os.ReadFile(copiedRecord)
			if tc.success && !os.IsNotExist(readErr) || !tc.success && (readErr != nil || string(data) != `{"uid":"preserved"}`) {
				t.Fatal("unexpected copied record mutation")
			}
			original, err := os.ReadFile(record)
			if err != nil || string(original) != `{"uid":"preserved"}` {
				t.Fatal("source record changed")
			}
			external, err := os.ReadFile(outside)
			if err != nil || string(external) != valid {
				t.Fatal("outside data changed")
			}
			if !tc.success {
				if _, err := os.Stat(filepath.Join(copied, "bootconfigurations")); !os.IsNotExist(err) {
					t.Fatal("failed lookup wrote fallback")
				}
			}
			for name, want := range map[string]string{canonical: tc.canon, legacyName: tc.legacy} {
				if want == "" {
					continue
				}
				got, err := os.ReadFile(filepath.Join(seriesDir, name))
				if err != nil || string(got) != want {
					t.Fatal("series bytes changed")
				}
			}
		})
	}
}
