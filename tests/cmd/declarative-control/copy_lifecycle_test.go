// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type copyTestExec struct {
	before       container
	paused       bool
	calls        [][]string
	failRun      bool
	inspectExtra []byte
}

func (f *copyTestExec) run(_ context.Context, args ...string) ([]byte, error) {
	f.calls = append(f.calls, append([]string(nil), args...))
	switch args[0] {
	case "compose":
		return []byte(f.before.ID), nil
	case "pause":
		f.paused = true
		return nil, nil
	case "unpause":
		f.paused = false
		return nil, nil
	case "inspect":
		if f.inspectExtra != nil {
			return f.inspectExtra, nil
		}
		v := f.before
		v.State.Paused = f.paused
		return json.Marshal([]container{v})
	case "run":
		if f.failRun {
			return nil, errors.New("injected failure")
		}
	}
	return nil, nil
}

func TestCopyRunFailureRetainsIntentWithoutSecretArguments(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	data := filepath.Join(dir, "data")
	if err := os.Mkdir(data, 0700); err != nil {
		t.Fatal(err)
	}
	image := "sha256:" + strings.Repeat("a", 64)
	inspect := []byte(`[{"Id":"source","Image":"` + image + `","Config":{"Env":["SECRET=fixture-secret"],"Entrypoint":["/boot"],"Cmd":["serve"]}}]`)
	fake := &copyTestExec{inspectExtra: inspect, failRun: true}
	c := controller{project: "sandbox", runDir: dir, imageID: image, exec: fake}
	cp := copiedStore{before: container{ID: "source"}, service: "boot-copy-abcd", name: "sandbox-boot-copy-abcd", dir: dir, data: data}
	if _, _, err := c.startCopy(context.Background(), cp); err == nil {
		t.Fatal("failed docker run succeeded")
	}
	assets, err := os.ReadFile(filepath.Join(dir, "assets.jsonl"))
	if err != nil || !strings.Contains(string(assets), `"name":"sandbox-boot-copy-abcd"`) {
		t.Fatal("container cleanup intent missing")
	}
	for _, args := range fake.calls {
		for _, arg := range args {
			if strings.Contains(arg, "fixture-secret") {
				t.Fatal("secret passed in Docker arguments")
			}
		}
	}
	info, err := os.Stat(filepath.Join(dir, "environment"))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("env file is not private")
	}
}

func TestCopyFailureRestartsOwnedContainer(t *testing.T) {
	dir, _, err := backupProcess(t, true)
	if err == nil {
		t.Fatal("symlink copy succeeded")
	}
	assets, err := os.ReadFile(filepath.Join(dir, "assets.jsonl"))
	if err != nil || !strings.Contains(string(assets), `"kind":"bind-directory"`) {
		t.Fatal("cleanup intent missing")
	}
}

func TestProxyRejectsInvalidPayloadBeforeDocker(t *testing.T) {
	fake := &copyTestExec{}
	c := controller{exec: fake}
	if _, err := adoptionInterrupt(context.Background(), c, json.RawMessage(`{"sourceUid":"../../bad","seriesName":"valid","assignmentName":"valid"}`)); err == nil {
		t.Fatal("unsafe UID accepted")
	}
	if len(fake.calls) != 0 {
		t.Fatal("invalid request reached Docker")
	}
}
