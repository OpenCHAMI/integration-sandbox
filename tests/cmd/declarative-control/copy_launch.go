// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

type copyInspect struct {
	ID     string      `json:"Id"`
	Image  string      `json:"Image"`
	Mounts []copyMount `json:"Mounts"`
	Config struct {
		Env        []string `json:"Env"`
		Entrypoint []string `json:"Entrypoint"`
		Cmd        []string `json:"Cmd"`
		WorkingDir string   `json:"WorkingDir"`
		User       string   `json:"User"`
	} `json:"Config"`
}

func (c controller) startCopy(ctx context.Context, cp copiedStore) (string, container, error) {
	raw, err := c.exec.run(ctx, "inspect", "--type", "container", cp.before.ID)
	if err != nil {
		return "", container{}, errDocker
	}
	var inspect []copyInspect
	if json.Unmarshal(raw, &inspect) != nil || len(inspect) != 1 || inspect[0].ID != cp.before.ID || inspect[0].Image != c.imageID {
		return "", container{}, errDocker
	}
	configDir, err := c.copyBootConfig(inspect[0])
	if err != nil {
		return "", container{}, err
	}
	if configDir != "" {
		if err := c.prepareBootIdentity(ctx); err != nil {
			return "", container{}, err
		}
	}
	cfg := inspect[0].Config
	envPath := filepath.Join(cp.dir, "environment")
	for _, entry := range cfg.Env {
		if strings.ContainsAny(entry, "\r\n\x00") || !strings.Contains(entry, "=") {
			return "", container{}, errDocker
		}
	}
	if err := os.WriteFile(envPath, []byte(strings.Join(cfg.Env, "\n")+"\n"), 0600); err != nil {
		return "", container{}, err
	}
	if err := c.registerAsset(asset{Kind: "container", Name: cp.name, Project: c.project, Service: cp.service}); err != nil {
		return "", container{}, err
	}
	args := []string{"run", "--detach", "--name", cp.name, "--label", "com.docker.compose.project=" + c.project, "--label", "com.docker.compose.service=" + cp.service, "--label", "openchami.acceptance.run=" + c.project, "--network", c.project + "_default", "--publish", "127.0.0.1::27790", "--mount", "type=bind,src=" + cp.data + ",dst=/data", "--env-file", envPath}
	if configDir != "" {
		args = append(args, "--mount", "type=bind,src="+configDir+",dst=/run/boot-config,readonly")
	}
	if cfg.WorkingDir != "" {
		args = append(args, "--workdir", cfg.WorkingDir)
	}
	if cfg.User != "" {
		args = append(args, "--user", cfg.User)
	}
	if len(cfg.Entrypoint) > 0 {
		args = append(args, "--entrypoint", cfg.Entrypoint[0])
	}
	args = append(args, c.imageID)
	if len(cfg.Entrypoint) > 1 {
		args = append(args, cfg.Entrypoint[1:]...)
	}
	args = append(args, cfg.Cmd...)
	out, err := c.exec.run(ctx, args...)
	if err != nil {
		return "", container{}, errDocker
	}
	id := strings.TrimSpace(string(out))
	started, err := c.inspect(ctx, cp.service, id)
	if err != nil || !started.State.Running || started.Image != c.imageID {
		return "", container{}, errDocker
	}
	dir, err := c.dataDir(started)
	if err != nil || dir != cp.data {
		return "", container{}, errDocker
	}
	if configDir != "" {
		raw, err := c.exec.run(ctx, "inspect", "--type", "container", started.ID)
		var actual []copyInspect
		if err != nil || json.Unmarshal(raw, &actual) != nil || len(actual) != 1 || actual[0].ID != started.ID || actual[0].Image != c.imageID {
			return "", container{}, errDocker
		}
		actualDir, err := c.copyBootConfig(actual[0])
		if err != nil || actualDir != configDir {
			return "", container{}, errDocker
		}
	}
	if err := c.registerAsset(asset{Kind: "container", Name: cp.name, Project: c.project, Service: cp.service, ImageID: started.Image}); err != nil {
		return "", container{}, err
	}
	base, err := c.endpoint(ctx, id, "27790/tcp", "/health")
	if err != nil {
		return "", container{}, err
	}
	return base, started, nil
}

func backupRestore(ctx context.Context, c controller, raw json.RawMessage) (json.RawMessage, error) {
	var request struct{}
	if err := strictRequest(raw, &request); err != nil {
		return nil, err
	}
	var base string
	err := c.copyStore(ctx, func(cp copiedStore) error {
		var started container
		var err error
		base, started, err = c.startCopy(ctx, cp)
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		return c.appendReceipt("backup-restore", "boot", "main", cp.before, started)
	})
	if err != nil {
		return nil, err
	}
	return json.Marshal(struct {
		BaseURL string `json:"baseURL"`
	}{base})
}

func init() { registerAction("backup-restore", backupRestore) }
