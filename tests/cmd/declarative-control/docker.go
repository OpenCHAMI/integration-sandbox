// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"strings"
)

type executor interface {
	run(context.Context, ...string) ([]byte, error)
}
type dockerExec struct{}

func (dockerExec) run(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "docker", args...)
	output := &boundedDockerOutput{}
	cmd.Stdout = output
	if err := cmd.Run(); err != nil || output.overflow {
		return nil, errDocker
	}
	return output.data.Bytes(), nil
}

type boundedDockerOutput struct {
	data     bytes.Buffer
	overflow bool
}

func (b *boundedDockerOutput) Write(p []byte) (int, error) {
	if len(p) > (4<<20)-b.data.Len() {
		b.overflow = true
		return len(p), nil
	}
	return b.data.Write(p)
}

type controller struct {
	project        string
	compose        string
	runDir         string
	receipts       string
	imageID        string
	authOffService string
	exec           executor
}
type containerConfig struct {
	Labels map[string]string `json:"Labels"`
	Cmd    []string          `json:"Cmd"`
	Env    []string          `json:"Env"`
}
type container struct {
	ID                    string          `json:"Id"`
	Image                 string          `json:"Image"`
	Config                containerConfig `json:"Config"`
	stopStateComplete     bool
	identityMountReadOnly bool
	State                 struct {
		Running   bool   `json:"Running"`
		Paused    bool   `json:"Paused"`
		StartedAt string `json:"StartedAt"`
		ExitCode  int    `json:"ExitCode"`
		OOMKilled bool   `json:"OOMKilled"`
	} `json:"State"`
	Mounts []struct {
		Type        string `json:"Type"`
		Source      string `json:"Source"`
		Destination string `json:"Destination"`
	} `json:"Mounts"`
}

var errDocker = errors.New("owned Docker operation failed")

func (c controller) inspect(ctx context.Context, service, id string) (container, error) {
	if id == "" || strings.ContainsAny(id, " \r\n\t") {
		return container{}, errDocker
	}
	out, err := c.exec.run(ctx, "inspect", "--type", "container", id)
	if err != nil {
		return container{}, errDocker
	}
	var result []container
	if json.Unmarshal(out, &result) != nil || len(result) != 1 {
		return container{}, errDocker
	}
	var fields []struct {
		State struct {
			ExitCode  *int  `json:"ExitCode"`
			OOMKilled *bool `json:"OOMKilled"`
		} `json:"State"`
		Mounts []struct {
			Source      string `json:"Source"`
			Destination string `json:"Destination"`
			Type        string `json:"Type"`
			RW          *bool  `json:"RW"`
		} `json:"Mounts"`
	}
	if json.Unmarshal(out, &fields) != nil || len(fields) != 1 {
		return container{}, errDocker
	}
	v := result[0]
	v.stopStateComplete = fields[0].State.ExitCode != nil && fields[0].State.OOMKilled != nil
	for _, mount := range fields[0].Mounts {
		if mount.Type == "bind" && mount.Destination == "/run/boot-config" && mount.Source == os.Getenv("SBX_DECLARATIVE_BOOT_CONFIG_DIR") && mount.RW != nil && !*mount.RW {
			v.identityMountReadOnly = true
		}
	}
	if v.ID == "" || v.Config.Labels["com.docker.compose.project"] != c.project || v.Config.Labels["com.docker.compose.service"] != service || v.Config.Labels["openchami.acceptance.run"] != c.project {
		return container{}, errDocker
	}
	return v, nil
}

func (c controller) locate(ctx context.Context, service string) (container, error) {
	out, err := c.exec.run(ctx, "compose", "--project-name", c.project, "--file", c.compose, "ps", "--all", "--quiet", service)
	if err != nil {
		return container{}, errDocker
	}
	id := strings.TrimSpace(string(out))
	return c.inspect(ctx, service, id)
}
