// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type copyIdentityFake struct {
	backend             identityFake
	image, config, port string
	issuedAtRun         []int
	runArgs             [][]string
	service             string
	data                string
}

func (f *copyIdentityFake) run(ctx context.Context, args ...string) ([]byte, error) {
	switch args[0] {
	case "compose", "exec":
		return f.backend.run(ctx, args...)
	case "inspect":
		id := args[len(args)-1]
		if id == "ts-owned" {
			return f.backend.run(ctx, args...)
		}
		service := "boot"
		data := ""
		if id == "copy-id" {
			service, data = f.service, f.data
		}
		readonly := false
		v := struct {
			ID     string `json:"Id"`
			Image  string `json:"Image"`
			Config struct {
				Labels map[string]string `json:"Labels"`
				Cmd    []string          `json:"Cmd"`
				Env    []string          `json:"Env"`
			} `json:"Config"`
			Mounts []copyMount `json:"Mounts"`
			State  struct {
				Running bool `json:"Running"`
			} `json:"State"`
		}{ID: id, Image: f.image}
		v.Config.Labels = map[string]string{"com.docker.compose.project": "sandbox", "com.docker.compose.service": service, "openchami.acceptance.run": "sandbox"}
		v.Config.Cmd = []string{"serve", "--tokensmith-url=http://tokensmith:27780", "--config=/run/boot-config/boot.json"}
		v.Config.Env = []string{"TOKENSMITH_JWKS_URL=http://tokensmith:27780/.well-known/jwks.json"}
		v.State.Running = true
		v.Mounts = []copyMount{{Type: "bind", Source: f.config, Destination: "/run/boot-config", RW: &readonly}}
		if data != "" {
			v.Mounts = append(v.Mounts, copyMount{Type: "bind", Source: data, Destination: "/data"})
		}
		return json.Marshal([]interface{}{v})
	case "run":
		f.issuedAtRun = append(f.issuedAtRun, f.backend.issued)
		f.runArgs = append(f.runArgs, append([]string(nil), args...))
		for _, arg := range args {
			if strings.HasPrefix(arg, "com.docker.compose.service=") {
				f.service = strings.TrimPrefix(arg, "com.docker.compose.service=")
			}
			if strings.HasPrefix(arg, "type=bind,src=") && strings.HasSuffix(arg, ",dst=/data") {
				f.data = strings.TrimSuffix(strings.TrimPrefix(arg, "type=bind,src="), ",dst=/data")
			}
		}
		return []byte("copy-id"), nil
	case "port":
		return []byte(f.port), nil
	}
	return nil, errors.New("unexpected Docker operation")
}

func TestAuthenticatedCopiesReceiveDistinctFreshCredentials(t *testing.T) {
	run := nativeTempDir(t)
	if err := os.Chmod(run, 0700); err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(run, "config", "boot")
	if err := os.MkdirAll(config, 0755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(config, "boot.json")
	if err := os.WriteFile(file, []byte(`{"tokensmith_bootstrap_token":"initial"}`), 0444); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(file, 0444); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SBX_DECLARATIVE_BOOT_CONFIG_DIR", config)
	t.Setenv("SBX_DECLARATIVE_HSM_BOOTSTRAP_REDACTIONS", filepath.Join(run, "credentials.redactions"))
	t.Setenv("SBX_DECLARATIVE_TOKENSMITH_STORE", "/tokensmith/data/bootstrap-tokens")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
	defer server.Close()
	image := "sha256:" + strings.Repeat("a", 64)
	fake := &copyIdentityFake{backend: identityFake{project: "sandbox", tokens: []string{"fresh-one", "fresh-two"}}, image: image, config: config, port: strings.TrimPrefix(server.URL, "http://")}
	c := controller{project: "sandbox", runDir: run, imageID: image, exec: fake}
	var tokens []string
	for _, suffix := range []string{"one", "two"} {
		dir := filepath.Join(run, "copy-"+suffix)
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
		data := filepath.Join(dir, "data")
		if err := os.Mkdir(data, 0700); err != nil {
			t.Fatal(err)
		}
		cp := copiedStore{before: container{ID: "source"}, service: "boot-copy-" + suffix, name: "sandbox-boot-copy-" + suffix, dir: dir, data: data}
		if _, _, err := c.startCopy(context.Background(), cp); err != nil {
			t.Fatalf("start copy: %v", err)
		}
		body, err := os.ReadFile(file)
		var value struct {
			Token string `json:"tokensmith_bootstrap_token"`
		}
		if err != nil || json.Unmarshal(body, &value) != nil {
			t.Fatal("invalid config")
		}
		tokens = append(tokens, value.Token)
	}
	if len(fake.issuedAtRun) != 2 || fake.issuedAtRun[0] != 1 || fake.issuedAtRun[1] != 2 || tokens[0] != "fresh-one" || tokens[1] != "fresh-two" {
		t.Fatal("copy startup reused identity")
	}
	info, err := os.Stat(file)
	if err != nil || info.Mode().Perm() != 0444 {
		t.Fatal("invalid config permissions")
	}
	assets, err := os.ReadFile(filepath.Join(run, "assets.jsonl"))
	if err != nil || strings.Contains(string(assets), "fresh-one") || strings.Contains(string(assets), "fresh-two") {
		t.Fatal("credential leaked to asset metadata")
	}
	want := "type=bind,src=" + config + ",dst=/run/boot-config,readonly"
	for _, args := range fake.runArgs {
		found := false
		for _, arg := range args {
			if arg == want {
				found = true
			}
			if strings.Contains(arg, "fresh-one") || strings.Contains(arg, "fresh-two") {
				t.Fatal("credential in arguments")
			}
		}
		if !found {
			t.Fatal("missing read-only config directory mount")
		}
	}
}

func TestCopyIdentityFailureStopsBeforeContainerCreation(t *testing.T) {
	for _, mode := range []string{"unowned-config", "issuance-failure"} {
		t.Run(mode, func(t *testing.T) {
			c, backend, config := identityFixture(t)
			config = filepath.Join(config, "boot")
			if err := os.Mkdir(config, 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(config, 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(config, "boot.json"), []byte(`{"tokensmith_bootstrap_token":"initial"}`), 0444); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(filepath.Join(config, "boot.json"), 0444); err != nil {
				t.Fatal(err)
			}
			t.Setenv("SBX_DECLARATIVE_BOOT_CONFIG_DIR", config)
			image := "sha256:" + strings.Repeat("a", 64)
			c.imageID = image
			fake := &copyIdentityFake{backend: *backend, image: image, config: config}
			if mode == "unowned-config" {
				fake.config = t.TempDir()
			} else {
				fake.backend.failure = true
			}
			c.exec = fake
			dir := filepath.Join(c.runDir, "copy")
			if err := os.Mkdir(dir, 0700); err != nil {
				t.Fatal(err)
			}
			data := filepath.Join(dir, "data")
			if err := os.Mkdir(data, 0700); err != nil {
				t.Fatal(err)
			}
			cp := copiedStore{before: container{ID: "source"}, service: "boot-copy-test", name: "sandbox-boot-copy-test", dir: dir, data: data}
			if _, _, err := c.startCopy(context.Background(), cp); err == nil {
				t.Fatal("invalid identity configuration succeeded")
			}
			if len(fake.runArgs) != 0 || fake.backend.issued != 0 {
				t.Fatal("invalid identity reached container creation")
			}
		})
	}
}
