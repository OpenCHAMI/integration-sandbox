// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type proxyFake struct {
	main         container
	facade       container
	proxy        container
	image        string
	startedImage string
	port         string
	runArgs      []string
}

func (f *proxyFake) run(_ context.Context, args ...string) ([]byte, error) {
	switch args[0] {
	case "compose":
		if args[len(args)-1] == "tokensmith" {
			return []byte(f.facade.ID), nil
		}
		return []byte(f.main.ID), nil
	case "image":
		return []byte(f.image), nil
	case "port":
		return []byte(f.port), nil
	case "inspect":
		if args[len(args)-1] == "proxy-id" {
			return json.Marshal([]container{f.proxy})
		}
		if args[len(args)-1] == f.facade.ID {
			return json.Marshal([]container{f.facade})
		}
		return json.Marshal([]container{f.main})
	case "run":
		f.runArgs = append([]string(nil), args...)
		var service string
		for _, arg := range args {
			if strings.HasPrefix(arg, "com.docker.compose.service=") {
				service = strings.TrimPrefix(arg, "com.docker.compose.service=")
			}
		}
		f.proxy = container{ID: "proxy-id", Image: f.image, Config: containerConfig{Labels: map[string]string{"com.docker.compose.project": "sandbox", "com.docker.compose.service": service, "openchami.acceptance.run": "sandbox"}}}
		if f.startedImage != "" {
			f.proxy.Image = f.startedImage
		}
		f.proxy.State.Running = true
		return []byte("proxy-id"), nil
	}
	return nil, errDocker
}

func TestProxyStartsRegisteredFixtureImage(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/__fixtures/health" {
			t.Errorf("unexpected health path %q", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	bootImage := "sha256:" + strings.Repeat("a", 64)
	fixtureImage := "sha256:" + strings.Repeat("b", 64)
	v := container{ID: "boot-id", Image: bootImage, Config: containerConfig{Labels: map[string]string{"com.docker.compose.project": "sandbox", "com.docker.compose.service": "boot", "openchami.acceptance.run": "sandbox"}}}
	v.State.Running = true
	facade := container{ID: "facade-id", Image: fixtureImage, Config: containerConfig{Labels: map[string]string{"com.docker.compose.project": "sandbox", "com.docker.compose.service": "tokensmith", "openchami.acceptance.run": "sandbox"}}}
	facade.State.Running = true
	fake := &proxyFake{main: v, facade: facade, image: fixtureImage, port: strings.TrimPrefix(server.URL, "http://")}
	t.Setenv("SBX_DECLARATIVE_FIXTURE_IMAGE", "fixture-image")
	c := controller{project: "sandbox", runDir: root, imageID: bootImage, exec: fake}
	output, err := adoptionInterrupt(context.Background(), c, json.RawMessage(`{"sourceUid":"bootconfiguration-1234","seriesName":"target-series","assignmentName":"target-assignment"}`))
	if err != nil {
		t.Fatal(err)
	}
	var response struct {
		ProxyURL string `json:"proxyURL"`
	}
	if json.Unmarshal(output, &response) != nil || response.ProxyURL != server.URL {
		t.Fatal("incorrect proxy endpoint")
	}
	joined := strings.Join(fake.runArgs, " ")
	if !strings.Contains(joined, fixtureImage+" fault-proxy --listen :27780 --upstream http://boot:27790") || !strings.Contains(joined, "127.0.0.1::27780") {
		t.Fatal("wrong fixture invocation")
	}
	assets, err := os.ReadFile(filepath.Join(root, "assets.jsonl"))
	if err != nil || strings.Count(string(assets), `"kind":"container"`) != 2 || !strings.Contains(string(assets), fixtureImage) {
		t.Fatal("proxy cleanup records missing")
	}
}

func TestProxyRejectsRetaggedFixtureBeforeCreatingContainer(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	bootImage := "sha256:" + strings.Repeat("a", 64)
	facadeImage := "sha256:" + strings.Repeat("b", 64)
	otherImage := "sha256:" + strings.Repeat("c", 64)
	labels := func(service string) containerConfig {
		return containerConfig{Labels: map[string]string{"com.docker.compose.project": "sandbox", "com.docker.compose.service": service, "openchami.acceptance.run": "sandbox"}}
	}
	main := container{ID: "boot-id", Image: bootImage, Config: labels("boot")}
	main.State.Running = true
	facade := container{ID: "facade-id", Image: facadeImage, Config: labels("tokensmith")}
	facade.State.Running = true
	fake := &proxyFake{main: main, facade: facade, image: otherImage}
	t.Setenv("SBX_DECLARATIVE_FIXTURE_IMAGE", "retagged-fixture")
	c := controller{project: "sandbox", runDir: root, imageID: bootImage, exec: fake}
	_, err = adoptionInterrupt(context.Background(), c, json.RawMessage(`{"sourceUid":"bootconfiguration-1234","seriesName":"series","assignmentName":"assignment"}`))
	if err == nil {
		t.Fatal("retagged image accepted")
	}
	if len(fake.runArgs) != 0 {
		t.Fatal("mismatched image reached docker run")
	}
	if _, err := os.Stat(filepath.Join(root, "assets.jsonl")); !os.IsNotExist(err) {
		t.Fatal("mismatched image registered a container")
	}
}

func TestProxyRejectsUnownedFacadeBeforeCreatingContainer(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	bootImage := "sha256:" + strings.Repeat("a", 64)
	fixtureImage := "sha256:" + strings.Repeat("b", 64)
	main := container{ID: "boot-id", Image: bootImage, Config: containerConfig{Labels: map[string]string{"com.docker.compose.project": "sandbox", "com.docker.compose.service": "boot", "openchami.acceptance.run": "sandbox"}}}
	main.State.Running = true
	facade := container{ID: "facade-id", Image: fixtureImage, Config: containerConfig{Labels: map[string]string{"com.docker.compose.project": "other", "com.docker.compose.service": "tokensmith", "openchami.acceptance.run": "sandbox"}}}
	facade.State.Running = true
	fake := &proxyFake{main: main, facade: facade, image: fixtureImage}
	t.Setenv("SBX_DECLARATIVE_FIXTURE_IMAGE", "fixture")
	c := controller{project: "sandbox", runDir: root, imageID: bootImage, exec: fake}
	_, err = adoptionInterrupt(context.Background(), c, json.RawMessage(`{"sourceUid":"bootconfiguration-1234","seriesName":"series","assignmentName":"assignment"}`))
	if err == nil || len(fake.runArgs) != 0 {
		t.Fatal("unowned facade allowed proxy creation")
	}
}

func TestProxyRejectsUnexpectedStartedImage(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	bootImage := "sha256:" + strings.Repeat("a", 64)
	fixtureImage := "sha256:" + strings.Repeat("b", 64)
	labels := func(service string) containerConfig {
		return containerConfig{Labels: map[string]string{"com.docker.compose.project": "sandbox", "com.docker.compose.service": service, "openchami.acceptance.run": "sandbox"}}
	}
	main := container{ID: "boot-id", Image: bootImage, Config: labels("boot")}
	main.State.Running = true
	facade := container{ID: "facade-id", Image: fixtureImage, Config: labels("tokensmith")}
	facade.State.Running = true
	fake := &proxyFake{main: main, facade: facade, image: fixtureImage, startedImage: "sha256:" + strings.Repeat("c", 64)}
	t.Setenv("SBX_DECLARATIVE_FIXTURE_IMAGE", "fixture")
	c := controller{project: "sandbox", runDir: root, imageID: bootImage, exec: fake}
	_, err = adoptionInterrupt(context.Background(), c, json.RawMessage(`{"sourceUid":"bootconfiguration-1234","seriesName":"series","assignmentName":"assignment"}`))
	if err == nil || len(fake.runArgs) == 0 {
		t.Fatal("unexpected started image accepted")
	}
	data, err := os.ReadFile(filepath.Join(root, "assets.jsonl"))
	if err != nil || strings.Count(string(data), `"kind":"container"`) != 1 {
		t.Fatal("cleanup intent missing after failed inspection")
	}
}
