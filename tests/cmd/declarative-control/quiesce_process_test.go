// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

type quiesceFake struct {
	mu          sync.Mutex
	running     bool
	started     string
	data        string
	image       string
	exitCode    int
	starts      int
	stopStarted chan struct{}
	release     chan struct{}
	restarted   chan struct{}
}

func (f *quiesceFake) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		w.WriteHeader(http.StatusOK)
		return
	}
	var args []string
	if json.NewDecoder(r.Body).Decode(&args) != nil || len(args) == 0 {
		http.Error(w, "bad request", 400)
		return
	}
	switch args[0] {
	case "compose":
		fmt.Fprintln(w, "owned-id")
	case "stop":
		if len(args) != 4 || args[1] != "--time" || args[2] != "40" || args[3] != "owned-id" {
			http.Error(w, "invalid stop", 400)
			return
		}
		close(f.stopStarted)
		<-f.release
		f.mu.Lock()
		f.running = false
		f.mu.Unlock()
	case "start":
		f.mu.Lock()
		f.running = true
		f.starts++
		f.started = fmt.Sprintf("2026-01-01T00:00:%02dZ", f.starts)
		f.mu.Unlock()
		close(f.restarted)
	case "inspect":
		f.mu.Lock()
		v := container{ID: "owned-id", Image: f.image, Config: containerConfig{Labels: map[string]string{"com.docker.compose.project": "sandbox", "com.docker.compose.service": "boot", "openchami.acceptance.run": "sandbox"}}}
		v.State.Running, v.State.StartedAt = f.running, f.started
		v.State.ExitCode = f.exitCode
		v.Mounts = append(v.Mounts, struct {
			Type        string `json:"Type"`
			Source      string `json:"Source"`
			Destination string `json:"Destination"`
		}{"bind", f.data, "/data"})
		f.mu.Unlock()
		json.NewEncoder(w).Encode([]container{v})
	default:
		http.Error(w, "unexpected command", 400)
	}
}

func buildQuiesceTestBinary(t *testing.T, path string, pkg string) {
	t.Helper()
	cmd := exec.Command("go", "build", "-o", path, pkg)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build helper: %v %s", err, output)
	}
}

func waitQuiesceEvent(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(15 * time.Second):
		t.Fatal("recovery event timed out")
	}
}

func TestQuiesceProcessRecovery(t *testing.T) {
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(base, "bin")
	if err := os.Mkdir(bin, 0700); err != nil {
		t.Fatal(err)
	}
	control := filepath.Join(bin, "control")
	buildQuiesceTestBinary(t, control, ".")
	buildQuiesceTestBinary(t, filepath.Join(bin, "docker"), "./testdata/quiesce-fakedocker")
	for _, mode := range []string{"hard-kill", "sigterm", "copy-error", "forced", "drain", "orphan-marker", "retired-error", "twice"} {
		t.Run(mode, func(t *testing.T) {
			root := filepath.Join(base, mode)
			run := filepath.Join(root, "logs", "run")
			data := filepath.Join(run, "data")
			if err := os.MkdirAll(data, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(filepath.Join(root, "compose"), 0700); err != nil {
				t.Fatal(err)
			}
			compose := filepath.Join(root, "compose", "test.yaml")
			receipts := filepath.Join(run, "receipts.jsonl")
			lock := filepath.Join(run, "writers.lock")
			for _, p := range []string{compose, receipts, lock} {
				if err := os.WriteFile(p, nil, 0600); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "copy-error" {
				if err := os.Symlink(filepath.Join(root, "outside"), filepath.Join(data, "unsafe-link")); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "drain" {
				for _, dir := range []string{"series", "records"} {
					if err := os.MkdirAll(filepath.Join(data, "revisions", "bootconfig", dir), 0700); err != nil {
						t.Fatal(err)
					}
				}
				for path, body := range map[string]string{"series/s.json": `{"latest":"a"}`, "records/a.json": `{"uid":"a"}`, "records/b.json": `{"uid":"b"}`} {
					if err := os.WriteFile(filepath.Join(data, "revisions", "bootconfig", path), []byte(body), 0600); err != nil {
						t.Fatal(err)
					}
				}
			}
			f := &quiesceFake{running: true, started: "2026-01-01T00:00:00Z", data: data, image: "sha256:" + strings.Repeat("a", 64), stopStarted: make(chan struct{}), release: make(chan struct{}), restarted: make(chan struct{})}
			if mode == "forced" {
				f.exitCode = 137
			}
			server := httptest.NewServer(f)
			defer server.Close()
			var releaseOnce sync.Once
			defer releaseOnce.Do(func() { close(f.release) })
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			if mode == "hard-kill" {
				cancel()
				ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
			}
			defer cancel()
			action, request := "snapshot-store", `{"instance":"main"}`
			if mode == "copy-error" {
				action, request = "backup-restore", `{}`
			}
			if mode == "retired-error" {
				action, request = "retired-reference-fixture", `{}`
			}
			cmd := exec.CommandContext(ctx, control, action, request)
			var output bytes.Buffer
			cmd.Stdout = &output
			cmd.Env = append(os.Environ(), "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"), "QUIESCE_FAKE_DOCKER_URL="+server.URL, "SBX_DECLARATIVE_COMPOSE_PROJECT=sandbox", "PROJECT=sandbox", "SBX_DECLARATIVE_COMPOSE_FILE="+compose, "SBX_DECLARATIVE_RUN_DIR="+run, "SBX_DECLARATIVE_CONTROL_RECEIPTS="+receipts, "SBX_DECLARATIVE_BOOT_IMAGE_ID="+f.image, "SBX_DECLARATIVE_BOOT_URL="+server.URL, "SBX_DECLARATIVE_QUIESCENCE_LOCK="+lock)
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			waitQuiesceEvent(t, f.stopStarted)
			waited := make(chan error, 1)
			go func() { waited <- cmd.Wait() }()
			switch mode {
			case "orphan-marker":
				if err := os.WriteFile(filepath.Join(lock+".inflight", "sticky"), []byte(`{}`), 0600); err != nil {
					t.Fatal(err)
				}
				cancel()
				select {
				case <-waited:
				case <-time.After(10 * time.Second):
					t.Fatal("owner did not exit")
				}
			case "hard-kill":
				select {
				case err := <-waited:
					if err == nil {
						t.Fatal("hard kill succeeded")
					}
				case <-time.After(10 * time.Second):
					t.Fatal("hard kill did not terminate owner")
				}
			case "sigterm":
				if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
					t.Fatal(err)
				}
			case "drain":
				select {
				case <-waited:
					t.Fatal("snapshot completed before stop drained")
				default:
				}
				if err := os.WriteFile(filepath.Join(data, "revisions", "bootconfig", "series", "s.json"), []byte(`{"latest":"b"}`), 0600); err != nil {
					t.Fatal(err)
				}
			}
			releaseOnce.Do(func() { close(f.release) })
			waitQuiesceEvent(t, f.restarted)
			if mode != "hard-kill" && mode != "orphan-marker" {
				select {
				case err := <-waited:
					if (mode == "drain" || mode == "twice") != (err == nil) {
						t.Fatal("unexpected snapshot outcome")
					}
				case <-time.After(15 * time.Second):
					t.Fatal("owner did not exit")
				}
			}
			if mode == "drain" {
				var result struct {
					Digest string `json:"configurationDigest"`
				}
				want, err := digestStore(data)
				if err != nil || json.Unmarshal(output.Bytes(), &result) != nil || result.Digest != want {
					t.Fatal("snapshot missed committed bytes")
				}
				body, err := os.ReadFile(receipts)
				var observed receipt
				if err != nil || json.Unmarshal(body, &observed) != nil || observed.BeforeStartedAt != "2026-01-01T00:00:00Z" || observed.AfterStartedAt != "2026-01-01T00:00:01Z" {
					t.Fatal("snapshot receipt does not report real restart")
				}
			}
			assertWatcherFinished(t, run)
			if mode == "retired-error" || mode == "orphan-marker" {
				assertStickyMarker(t, lock, mode)
			}
			if mode == "twice" {
				repeatSnapshot(t, control, cmd.Env, f, run)
			}
		})
	}
}
