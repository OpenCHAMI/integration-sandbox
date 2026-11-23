// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type identityLifecycleFake struct {
	backend   *identityFake
	boot      container
	configDir string
	port      string
	starts    int
	consumed  map[string]bool
}

func (f *identityLifecycleFake) run(ctx context.Context, args ...string) ([]byte, error) {
	switch args[0] {
	case "compose":
		if args[len(args)-2] == "--quiet" {
			if args[len(args)-1] == "tokensmith-backend" {
				return []byte("ts-owned\n"), nil
			}
			return []byte("boot-owned\n"), nil
		}
		if args[len(args)-2] == "restart" {
			return nil, f.start()
		}
	case "inspect":
		if args[len(args)-1] == "ts-owned" {
			return f.backend.run(ctx, args...)
		}
		return f.bootInspect()
	case "exec":
		return f.backend.run(ctx, args...)
	case "port":
		return []byte(f.port + "\n"), nil
	case "start":
		return nil, f.start()
	}
	return nil, errDocker
}

func (f *identityLifecycleFake) bootInspect() ([]byte, error) {
	data, err := json.Marshal(f.boot)
	if err != nil {
		return nil, err
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil {
		return nil, err
	}
	mounts := []struct {
		Type        string `json:"Type"`
		Source      string `json:"Source"`
		Destination string `json:"Destination"`
		RW          bool   `json:"RW"`
	}{
		{"bind", f.boot.Mounts[0].Source, "/data", true},
		{"bind", f.configDir, "/run/boot-config", false},
	}
	object["Mounts"], err = json.Marshal(mounts)
	if err != nil {
		return nil, err
	}
	return json.Marshal([]map[string]json.RawMessage{object})
}

func (f *identityLifecycleFake) start() error {
	data, err := os.ReadFile(filepath.Join(f.configDir, "boot.json"))
	if err != nil {
		return errDocker
	}
	var config struct {
		Token string `json:"tokensmith_bootstrap_token"`
	}
	if json.Unmarshal(data, &config) != nil || config.Token == "" || f.consumed[config.Token] {
		return errDocker
	}
	f.consumed[config.Token] = true
	f.starts++
	f.boot.State.Running = true
	f.boot.State.StartedAt = fmt.Sprintf("2026-01-01T00:00:%02dZ", f.starts)
	return nil
}

func identityLifecycleFixture(t *testing.T, port string) (controller, *identityLifecycleFake) {
	t.Helper()
	c, backend, configDir := identityFixture(t)
	data := filepath.Join(c.runDir, "data")
	if err := os.Mkdir(data, 0700); err != nil {
		t.Fatal(err)
	}
	c.receipts = filepath.Join(c.runDir, "receipts.jsonl")
	lock := filepath.Join(c.runDir, "writers.lock")
	for _, path := range []string{c.receipts, lock} {
		if err := os.WriteFile(path, nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("SBX_DECLARATIVE_QUIESCENCE_LOCK", lock)
	t.Setenv("SBX_DECLARATIVE_BOOT_URL", "http://"+port)
	c.imageID = "sha256:" + strings.Repeat("a", 64)
	v := container{ID: "boot-owned", Image: c.imageID, Config: containerConfig{Labels: map[string]string{"com.docker.compose.project": "sandbox", "com.docker.compose.service": "boot", "openchami.acceptance.run": "sandbox"}, Cmd: []string{"serve", "--enable-auth=true", "--hsm-url=http://smd", "--tokensmith-url=http://tokensmith", "--config=/run/boot-config/boot.json"}}}
	v.State.Running, v.State.StartedAt = true, "2026-01-01T00:00:00Z"
	v.Mounts = append(v.Mounts, struct {
		Type        string `json:"Type"`
		Source      string `json:"Source"`
		Destination string `json:"Destination"`
	}{"bind", data, "/data"})
	f := &identityLifecycleFake{backend: backend, boot: v, configDir: configDir, port: port, consumed: map[string]bool{"initial-secret": true}}
	c.exec = f
	return c, f
}
