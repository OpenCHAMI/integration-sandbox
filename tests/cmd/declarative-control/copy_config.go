// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package main

import (
	"os"
	"path/filepath"
	"strings"
)

type copyMount struct {
	Type        string `json:"Type"`
	Source      string `json:"Source"`
	Destination string `json:"Destination"`
	RW          *bool  `json:"RW"`
}

func (c controller) copyBootConfig(inspect copyInspect) (string, error) {
	const mountPath = "/run/boot-config"
	const configPath = mountPath + "/boot.json"
	var configured, authenticated bool
	args := append(append([]string{}, inspect.Config.Entrypoint...), inspect.Config.Cmd...)
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--tokensmith-bootstrap-token" || strings.HasPrefix(arg, "--tokensmith-bootstrap-token=") {
			return "", errDocker
		}
		if strings.HasPrefix(arg, "--tokensmith-") || strings.HasPrefix(arg, "--hsm-url") || strings.HasPrefix(arg, "--smd-url") || arg == "--auth-enabled=true" || arg == "--auth-enabled" && i+1 < len(args) && args[i+1] == "true" {
			authenticated = true
		}
		if arg == "--config" {
			if configured || i+1 >= len(args) || args[i+1] != configPath {
				return "", errDocker
			}
			configured = true
			i++
		} else if strings.HasPrefix(arg, "--config=") {
			if configured || strings.TrimPrefix(arg, "--config=") != configPath {
				return "", errDocker
			}
			configured = true
		}
	}
	for _, entry := range inspect.Config.Env {
		key, _, _ := strings.Cut(entry, "=")
		if key == "TOKENSMITH_BOOTSTRAP_TOKEN" {
			return "", errDocker
		}
		if strings.HasPrefix(key, "TOKENSMITH_") || strings.HasPrefix(key, "HSM_") || strings.HasPrefix(key, "SMD_") {
			authenticated = true
		}
	}
	var mount *copyMount
	for i := range inspect.Mounts {
		candidate := &inspect.Mounts[i]
		if candidate.Destination == mountPath || strings.HasPrefix(candidate.Destination, mountPath+"/") {
			if mount != nil || candidate.Destination != mountPath {
				return "", errDocker
			}
			mount = candidate
		}
	}
	if !authenticated && !configured && mount == nil {
		return "", nil
	}
	if !configured || mount == nil || mount.Type != "bind" || mount.RW == nil || *mount.RW {
		return "", errDocker
	}
	dir := os.Getenv("SBX_DECLARATIVE_BOOT_CONFIG_DIR")
	if dir != filepath.Join(c.runDir, "config", "boot") || mount.Source != dir || strings.ContainsAny(dir, ",\r\n") || ownedPath(c.runDir, dir, false) != nil {
		return "", errPath
	}
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return "", errPath
	}
	return dir, nil
}
