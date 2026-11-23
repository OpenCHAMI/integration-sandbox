// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNativeLegacyAndSameNameCanonicalCoexist(t *testing.T) {
	dir := nativeTempDir(t)
	root := filepath.Join(dir, "revisions", "bootconfig")
	for _, name := range []string{"series", "records"} {
		if err := os.MkdirAll(filepath.Join(root, name), 0700); err != nil {
			t.Fatal(err)
		}
	}
	const uid = "rev_01h2xcejqtf2nbrexx3vqjhp41"
	const body = `{"status":{"name":"chosen","defaultRevisionUid":"` + uid + `","latestRevisionUid":"` + uid + `","revisionCount":1},"revisionUids":["` + uid + `"]}`
	legacy := filepath.Join(root, "series", base64.RawURLEncoding.EncodeToString([]byte("chosen"))+".json")
	if err := os.WriteFile(legacy, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	initial, err := readNativeSeries(dir, "chosen")
	if err != nil || initial.Status.Default != uid {
		t.Fatal("legacy read failed")
	}
	canonical := filepath.Join(root, "series", "sha256.7be403e3cc82262248913b70f596a31b93f656e0da8ce5d1bf757c63ef7a8db4.json")
	if err := os.WriteFile(canonical, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	actual, err := readNativeSeries(dir, "chosen")
	if err != nil || actual.Status.Name != "chosen" || actual.Status.Default != uid {
		t.Fatal("same-name canonical read failed")
	}
	legacyBytes, err := os.ReadFile(legacy)
	if err != nil || string(legacyBytes) != body {
		t.Fatal("legacy series changed")
	}
}

func TestNativeLegacyCaseAliasDoesNotReturnAnotherSeries(t *testing.T) {
	dir := nativeTempDir(t)
	root := filepath.Join(dir, "revisions", "bootconfig")
	for _, name := range []string{"series", "records"} {
		if err := os.MkdirAll(filepath.Join(root, name), 0700); err != nil {
			t.Fatal(err)
		}
	}
	original, requested := "aaG", "aaa"
	first := base64.RawURLEncoding.EncodeToString([]byte(original)) + ".json"
	second := base64.RawURLEncoding.EncodeToString([]byte(requested)) + ".json"
	if first == second || !strings.EqualFold(first, second) {
		t.Fatal("fixture does not model a filename case collision")
	}
	const body = `{"status":{"name":"aaG","defaultRevisionUid":"rev_original"},"revisionUids":["rev_original"]}`
	path := filepath.Join(root, "series", first)
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	aliased := filepath.Join(root, "series", second)
	if err := os.Rename(path, aliased); err != nil {
		t.Fatal(err)
	}
	if _, err := readNativeSeries(dir, requested); err == nil {
		t.Fatal("case-alias returned another series")
	}
	data, err := os.ReadFile(aliased)
	if err != nil || string(data) != body {
		t.Fatal("case-alias fixture changed")
	}
}
