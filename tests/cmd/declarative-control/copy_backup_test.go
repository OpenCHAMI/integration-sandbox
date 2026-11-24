// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type backupFake struct {
	mu    sync.Mutex
	main  container
	copy  container
	port  string
	lock  string
	onRun func() error
}

func (f *backupFake) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		w.WriteHeader(http.StatusOK)
		return
	}
	var args []string
	if json.NewDecoder(r.Body).Decode(&args) != nil || len(args) == 0 {
		http.Error(w, "bad request", 400)
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	switch args[0] {
	case "compose":
		fmt.Fprintln(w, f.main.ID)
	case "stop":
		if !captureHeld(f.lock) {
			http.Error(w, "capture lease not held", 500)
			return
		}
		if err := os.WriteFile(filepath.Join(f.main.Mounts[0].Source, "value.json"), []byte(`{"uid":"committed"}`), 0600); err != nil {
			http.Error(w, "write", 500)
			return
		}
		if err := os.WriteFile(filepath.Join(f.main.Mounts[0].Source, "series.json"), []byte(`{"current":"committed"}`), 0600); err != nil {
			http.Error(w, "write", 500)
			return
		}
		f.main.State.Running = false
	case "start":
		f.main.State.Running = true
		f.main.State.StartedAt = "2026-01-01T00:00:02Z"
	case "port":
		fmt.Fprintln(w, f.port)
	case "inspect":
		v := f.main
		if args[len(args)-1] == "copy-id" {
			v = f.copy
		}
		if err := json.NewEncoder(w).Encode([]container{v}); err != nil {
			return
		}
	case "run":
		if !captureHeld(f.lock) {
			http.Error(w, "capture lease not held during copy startup", 500)
			return
		}
		if f.onRun != nil {
			if err := f.onRun(); err != nil {
				http.Error(w, "copy startup probe failed", 500)
				return
			}
		}
		var service, data string
		for _, arg := range args {
			if strings.HasPrefix(arg, "com.docker.compose.service=") {
				service = strings.TrimPrefix(arg, "com.docker.compose.service=")
			}
			if strings.HasPrefix(arg, "type=bind,src=") {
				data = strings.TrimSuffix(strings.TrimPrefix(arg, "type=bind,src="), ",dst=/data")
			}
		}
		f.copy = container{ID: "copy-id", Image: f.main.Image, Config: containerConfig{Labels: map[string]string{"com.docker.compose.project": "sandbox", "com.docker.compose.service": service, "openchami.acceptance.run": "sandbox"}}}
		f.copy.State.Running, f.copy.State.StartedAt = true, "2026-01-01T00:00:03Z"
		f.copy.Mounts = append(f.copy.Mounts, struct {
			Type        string `json:"Type"`
			Source      string `json:"Source"`
			Destination string `json:"Destination"`
		}{"bind", data, "/data"})
		fmt.Fprintln(w, "copy-id")
	default:
		http.Error(w, "unexpected command", 400)
	}
}

func backupProcess(t *testing.T, unsafe bool) (string, []byte, error) {
	return backupProcessWithProbe(t, unsafe, nil)
}

func backupProcessWithProbe(t *testing.T, unsafe bool, probe func(string, string, context.CancelFunc) error) (string, []byte, error) {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	bin, run, composeDir := filepath.Join(root, "bin"), filepath.Join(root, "logs", "run"), filepath.Join(root, "compose")
	for _, path := range []string{bin, run, composeDir} {
		if err := os.MkdirAll(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	control := filepath.Join(bin, "control")
	buildQuiesceTestBinary(t, control, ".")
	buildQuiesceTestBinary(t, filepath.Join(bin, "docker"), "./testdata/quiesce-fakedocker")
	data := filepath.Join(run, "data")
	if err := os.Mkdir(data, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(data, "bootassignments"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(data, "bootassignments", "assignment.json"), []byte(`{"uid":"assignment"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(data, "series.json"), []byte(`{"current":"old"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(data, "value.json"), []byte(`{"uid":"old"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if unsafe {
		if err := os.Symlink(t.TempDir(), filepath.Join(data, "unsafe")); err != nil {
			t.Fatal(err)
		}
	}
	compose, receipts := filepath.Join(composeDir, "test.yaml"), filepath.Join(run, "receipts.jsonl")
	for _, p := range []string{compose, receipts} {
		if err := os.WriteFile(p, nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	lock := prepareCaptureFixture(t, run)
	image := "sha256:" + strings.Repeat("a", 64)
	v := container{ID: "source-id", Image: image, Config: containerConfig{Labels: map[string]string{"com.docker.compose.project": "sandbox", "com.docker.compose.service": "boot", "openchami.acceptance.run": "sandbox"}}}
	v.State.Running, v.State.StartedAt = true, "2026-01-01T00:00:00Z"
	v.Mounts = append(v.Mounts, struct {
		Type        string `json:"Type"`
		Source      string `json:"Source"`
		Destination string `json:"Destination"`
	}{"bind", data, "/data"})
	fake := &backupFake{main: v, lock: lock}
	server := httptest.NewServer(fake)
	defer server.Close()
	fake.mu.Lock()
	fake.port = strings.TrimPrefix(server.URL, "http://")
	fake.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if probe != nil {
		fake.mu.Lock()
		fake.onRun = func() error { return probe(lock, receipts, cancel) }
		fake.mu.Unlock()
	}
	cmd := exec.CommandContext(ctx, control, "backup-restore", `{}`)
	cmd.Env = append(os.Environ(), "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"), "QUIESCE_FAKE_DOCKER_URL="+server.URL, "SBX_DECLARATIVE_COMPOSE_PROJECT=sandbox", "PROJECT=sandbox", "SBX_DECLARATIVE_COMPOSE_FILE="+compose, "SBX_DECLARATIVE_RUN_DIR="+run, "SBX_DECLARATIVE_CONTROL_RECEIPTS="+receipts, "SBX_DECLARATIVE_BOOT_IMAGE_ID="+image, "SBX_DECLARATIVE_BOOT_URL="+server.URL, "SBX_DECLARATIVE_QUIESCENCE_LOCK="+lock)
	output, runErr := cmd.Output()
	fake.mu.Lock()
	running := fake.main.State.Running
	fake.mu.Unlock()
	if !running {
		t.Fatal("main not restarted")
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		t.Fatal("backup command timed out")
	}
	if probe == nil {
		assertCaptureFinished(t, run, lock)
	}
	return run, output, runErr
}

func TestBackupRestoreWritesReceiptForDistinctOwnedCopy(t *testing.T) {
	run, output, err := backupProcess(t, false)
	if err != nil {
		t.Fatal(err)
	}
	var response struct {
		BaseURL string `json:"baseURL"`
	}
	if json.Unmarshal(output, &response) != nil || response.BaseURL == "" {
		t.Fatal("wrong endpoint")
	}
	data, err := os.ReadFile(filepath.Join(run, "receipts.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	var r receipt
	if json.Unmarshal(data, &r) != nil || r.Action != "backup-restore" || r.BeforeContainerID != "source-id" || r.AfterContainerID != "copy-id" || r.SourceVolumeID == r.TargetVolumeID || r.BeforeStartedAt != "2026-01-01T00:00:00Z" || r.AfterStartedAt != "2026-01-01T00:00:03Z" {
		t.Fatal("invalid backup receipt")
	}
	copied, err := os.ReadFile(filepath.Join(r.TargetVolumeID, "value.json"))
	if err != nil || string(copied) != `{"uid":"committed"}` {
		t.Fatal("copy did not include completed in-flight write")
	}
	series, err := os.ReadFile(filepath.Join(r.TargetVolumeID, "series.json"))
	if err != nil || string(series) != `{"current":"committed"}` {
		t.Fatal("copy captured an incomplete multi-file update")
	}
	sourceDigest, sourceErr := digestStore(r.SourceVolumeID)
	targetDigest, targetErr := digestStore(r.TargetVolumeID)
	if sourceErr != nil || targetErr != nil || sourceDigest != targetDigest || sourceDigest == "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855" {
		t.Fatal("copied declarative-store digest differs from source")
	}
}
