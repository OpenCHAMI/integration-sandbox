// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRetiredFixtureUsesGracefulStopAndRestartsMain(t *testing.T) {
	root := nativeTempDir(t)
	bin := filepath.Join(root, "bin")
	run := filepath.Join(root, "logs", "run")
	data := filepath.Join(run, "data")
	composeDir := filepath.Join(root, "compose")
	for _, path := range []string{bin, data, composeDir, filepath.Join(data, "revisions", "bootconfig", "records"), filepath.Join(data, "revisions", "bootconfig", "series")} {
		if err := os.MkdirAll(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	control := filepath.Join(bin, "control")
	buildQuiesceTestBinary(t, control, ".")
	buildQuiesceTestBinary(t, filepath.Join(bin, "docker"), "./testdata/quiesce-fakedocker")
	compose, receipts := filepath.Join(composeDir, "test.yaml"), filepath.Join(run, "receipts.jsonl")
	for _, path := range []string{compose, receipts} {
		if err := os.WriteFile(path, nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	lock := prepareCaptureFixture(t, run)
	const original = `{"metadata":{"uid":"source","seriesName":"old","number":1,"digest":"old","createdAt":"2026-01-01T00:00:00Z"},"resource":{"apiVersion":"boot.openchami.io/v1","kind":"BootConfig","metadata":{"name":"old","uid":"u","createdAt":"2026-01-01T00:00:00Z","updatedAt":"2026-01-01T00:00:00Z"},"spec":{"kernel":"https://example.test/kernel"}}}`
	source := filepath.Join(data, "revisions", "bootconfig", "records", "source.json")
	if err := os.WriteFile(source, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	fake := &quiesceFake{running: true, started: "2026-01-01T00:00:00Z", data: data, image: "sha256:" + strings.Repeat("a", 64), stopStarted: make(chan struct{}), release: make(chan struct{}), restarted: make(chan struct{})}
	close(fake.release)
	server := httptest.NewServer(fake)
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, control, "retired-reference-fixture", `{}`)
	cmd.Env = append(os.Environ(), "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"), "QUIESCE_FAKE_DOCKER_URL="+server.URL, "SBX_DECLARATIVE_COMPOSE_PROJECT=sandbox", "PROJECT=sandbox", "SBX_DECLARATIVE_COMPOSE_FILE="+compose, "SBX_DECLARATIVE_RUN_DIR="+run, "SBX_DECLARATIVE_CONTROL_RECEIPTS="+receipts, "SBX_DECLARATIVE_BOOT_IMAGE_ID="+fake.image, "SBX_DECLARATIVE_BOOT_URL="+server.URL, "SBX_DECLARATIVE_QUIESCENCE_LOCK="+lock)
	output, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		Series string `json:"seriesName"`
		UID    string `json:"revisionUid"`
	}
	if json.Unmarshal(output, &result) != nil || result.Series == "" || result.UID == "" {
		t.Fatal("invalid fixture result")
	}
	waitQuiesceEvent(t, fake.restarted)
	assertCaptureFinished(t, run, lock)
	got, err := os.ReadFile(source)
	if err != nil || string(got) != original {
		t.Fatal("existing resource changed")
	}
	record, err := os.ReadFile(filepath.Join(data, "revisions", "bootconfig", "records", result.UID+".json"))
	var item nativeRecord
	if err != nil || json.Unmarshal(record, &item) != nil || item.Metadata.RetiredAt == nil || item.Metadata.SeriesName != result.Series {
		t.Fatal("retired record not published")
	}
}
