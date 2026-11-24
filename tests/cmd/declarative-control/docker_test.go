// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fakeExec struct {
	output []byte
	calls  int
}

func (f *fakeExec) run(_ context.Context, _ ...string) ([]byte, error) {
	f.calls++
	return f.output, nil
}

func TestInspectRejectsUnownedContainer(t *testing.T) {
	value := []container{{ID: "id", Image: "sha256:abc", Config: containerConfig{Labels: map[string]string{"com.docker.compose.project": "other", "com.docker.compose.service": "boot"}}}}
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	exec := &fakeExec{output: data}
	c := controller{project: "sandbox-test", exec: exec}
	if _, err := c.inspect(context.Background(), "boot", "id"); err == nil {
		t.Fatal("unowned container accepted")
	}
}

type sequenceExec struct {
	inspect   []byte
	port      string
	restarted bool
	paused    bool
}

func (f *sequenceExec) run(_ context.Context, args ...string) ([]byte, error) {
	if args[0] == "pause" {
		f.paused = true
	}
	if args[0] == "inspect" {
		return f.inspect, nil
	}
	if args[0] == "port" {
		return []byte(f.port), nil
	}
	if len(args) >= 2 && args[len(args)-2] == "restart" {
		f.restarted = true
	}
	if args[len(args)-2] == "--quiet" {
		return []byte("owned-id\n"), nil
	}
	return nil, nil
}

func TestAuthOffSnapshotRejectsWrongImageBeforePause(t *testing.T) {
	v := container{ID: "owned-id", Image: "sha256:" + strings.Repeat("a", 64), Config: containerConfig{Labels: map[string]string{"com.docker.compose.project": "sandbox", "com.docker.compose.service": "boot-auth-off", "openchami.acceptance.run": "sandbox"}}}
	v.State.Running = true
	data, err := json.Marshal([]container{v})
	if err != nil {
		t.Fatal(err)
	}
	f := &sequenceExec{inspect: data}
	c := controller{project: "sandbox", authOffService: "boot-auth-off", imageID: "sha256:" + strings.Repeat("b", 64), exec: f}
	if _, err := c.snapshot(context.Background(), "auth-off"); err == nil || f.paused {
		t.Fatal("wrong-image snapshot reached pause")
	}
}

func TestLifecycleRejectsSuccessfulNoOpRestart(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	data := filepath.Join(dir, "data")
	if err := os.Mkdir(data, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "receipts")
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	lock := filepath.Join(dir, "writers.lock")
	if err := os.WriteFile(lock, nil, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SBX_DECLARATIVE_QUIESCENCE_LOCK", lock)
	v := container{ID: "owned-id", Image: "sha256:" + strings.Repeat("a", 64), Config: containerConfig{Labels: map[string]string{"com.docker.compose.project": "sandbox", "com.docker.compose.service": "boot", "openchami.acceptance.run": "sandbox"}}}
	v.State.Running = true
	v.State.StartedAt = "2026-01-01T00:00:00Z"
	v.Mounts = append(v.Mounts, struct {
		Type        string `json:"Type"`
		Source      string `json:"Source"`
		Destination string `json:"Destination"`
	}{Type: "bind", Source: data, Destination: "/data"})
	encoded, err := json.Marshal([]container{v})
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("SBX_DECLARATIVE_BOOT_URL", "http://127.0.0.1:40100")
	f := &sequenceExec{inspect: encoded, port: "127.0.0.1:40100\n"}
	c := controller{project: "sandbox", compose: "compose.yaml", runDir: dir, receipts: path, imageID: v.Image, exec: f}
	if err := c.lifecycle(context.Background(), "service-restart", "boot"); err == nil {
		t.Fatal("no-op restart succeeded")
	}
	if !f.restarted {
		t.Fatal("test did not reach compose restart")
	}
	info, err := os.Stat(path)
	if err != nil || info.Size() != 0 {
		t.Fatal("no-op restart wrote a receipt")
	}
}

type portExec struct {
	ports []string
	index int
}

func (f *portExec) run(_ context.Context, _ ...string) ([]byte, error) {
	value := f.ports[f.index]
	f.index++
	return []byte(value), nil
}

func TestChangedPublishedPortRejected(t *testing.T) {
	f := &portExec{ports: []string{"127.0.0.1:40101\n"}}
	c := controller{exec: f}
	if err := c.samePort(context.Background(), "127.0.0.1:40100", "after"); err == nil {
		t.Fatal("changed port accepted")
	}
}

func TestRecreateRequiresFixedComposePort(t *testing.T) {
	f := &portExec{ports: []string{`{"services":{"boot":{"ports":[{"host_ip":"127.0.0.1","target":27790,"published":"0"}]}}}`}}
	c := controller{project: "sandbox", compose: "compose.yaml", exec: f}
	if err := c.configuredPort(context.Background(), "40100"); err == nil {
		t.Fatal("dynamic compose port accepted")
	}
}
