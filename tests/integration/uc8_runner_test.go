//go:build integration

// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package integration

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"time"
)

// CLIResult retains process failure information for negative assertions.
// ExitCode is -1 when the process could not start or was canceled.
type CLIResult struct {
	Stdout, Stderr string
	ExitCode       int
	Err            error
	ContainerName  string
}

// CLIInput permits per-command configuration and read-only file fixtures.
// Files maps host files to their absolute container paths.
type CLIInput struct {
	Stdin        string
	ConfigPath   string
	IgnoreConfig bool
	GlobalArgs   []string
	Env          map[string]string
	Files        map[string]string
}

var uc8Invocation atomic.Uint64

// Older CLI releases use envelope payloads by default; newer releases require
// --envelope. Select the same API intentionally using the advertised capability,
// without guessing from image tags or weakening resource assertions.
func (r *ochami_runner) envelopeArgs(args []string) ([]string, error) {
	if len(args) == 0 || args[0] != "boot" {
		return args, nil
	}
	if !r.bootModeChecked {
		result := r.execute(CLIInput{IgnoreConfig: true}, "boot", "--help")
		if result.Err != nil {
			return nil, fmt.Errorf("inspect boot API mode: %w: %s", result.Err, result.Stderr)
		}
		r.bootEnvelope = strings.Contains(result.Stdout, "--envelope")
		r.bootModeChecked = true
	}
	if r.bootEnvelope {
		return append([]string{"boot", "--envelope"}, args[1:]...), nil
	}
	return args, nil
}

func (r *ochami_runner) execute(input CLIInput, args ...string) CLIResult {
	result := CLIResult{ExitCode: -1, ContainerName: fmt.Sprintf("uc8-cli-%d-%d-%d", os.Getpid(), time.Now().UnixNano(), uc8Invocation.Add(1))}
	composeArgs := []string{"compose", "-f", "../../compose/infra.yaml", "-f", "../../compose/bmc-sim.yaml", "-f", "../../compose/core.yaml"}
	commandArgs := append(append([]string{}, composeArgs...), "run", "--rm", "-T", "--no-deps", "--name", result.ContainerName, "--entrypoint", "ochami")
	config := input.ConfigPath
	if config == "" {
		config = r.config_path
	}
	if !input.IgnoreConfig {
		absolute, err := filepath.Abs(config)
		if err != nil {
			result.Err = fmt.Errorf("resolve CLI config: %w", err)
			return result
		}
		commandArgs = append(commandArgs, "--volume", absolute+":/config.yaml:ro")
	}
	paths := make([]string, 0, len(input.Files))
	for host := range input.Files {
		paths = append(paths, host)
	}
	sort.Strings(paths)
	for _, host := range paths {
		absolute, err := filepath.Abs(host)
		if err != nil || !strings.HasPrefix(input.Files[host], "/") {
			result.Err = fmt.Errorf("invalid fixture mount %q -> %q", host, input.Files[host])
			return result
		}
		commandArgs = append(commandArgs, "--volume", absolute+":"+input.Files[host]+":ro")
	}
	env := map[string]string{"TEST_ACCESS_TOKEN": r.access_token}
	for key, value := range input.Env {
		env[key] = value
	}
	keys := make([]string, 0, len(env))
	for key := range env {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		commandArgs = append(commandArgs, "--env", key+"="+env[key])
	}
	commandArgs = append(commandArgs, "ochami-runner")
	if input.IgnoreConfig {
		commandArgs = append(commandArgs, "--ignore-config")
	} else {
		commandArgs = append(commandArgs, "--config", "/config.yaml")
	}
	commandArgs = append(commandArgs, input.GlobalArgs...)
	commandArgs = append(commandArgs, args...)
	cmd := exec.CommandContext(r.ctx, "docker", commandArgs...)
	cmd.Env = os.Environ()
	for key, value := range r.env {
		cmd.Env = append(cmd.Env, key+"="+value)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdin, cmd.Stdout, cmd.Stderr = strings.NewReader(input.Stdin), &stdout, &stderr
	result.Err = cmd.Run()
	result.Stdout, result.Stderr = stdout.String(), stderr.String()
	if cmd.ProcessState != nil {
		result.ExitCode = cmd.ProcessState.ExitCode()
	}
	if r.ctx.Err() != nil {
		result.Err = r.ctx.Err()
		result.ExitCode = -1
		result.Err = errors.Join(result.Err, r.removeCanceledInvocation(result.ContainerName, composeArgs))
	}
	return result
}

func (r *ochami_runner) removeCanceledInvocation(name string, composeArgs []string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	out := filepath.Join("../../logs", time.Now().UTC().Format("20060102T150405Z"), name)
	if err := os.MkdirAll(out, 0755); err != nil {
		return err
	}
	var failures []error
	for _, item := range []struct {
		name string
		args []string
	}{
		{"compose-ps.log", append(append([]string{}, composeArgs...), "ps")},
		{"docker-ps.log", []string{"ps", "-a"}},
		{"services.log", append(append([]string{}, composeArgs...), "logs", "--tail=200", "smd", "boot-service", "power-control")},
		{"invocation.log", []string{"logs", "--tail=200", name}},
	} {
		output, err := exec.CommandContext(ctx, "docker", item.args...).CombinedOutput()
		// The invocation may already have exited and been auto-removed.
		if err != nil {
			output = append(output, []byte(fmt.Sprintf("\ndiagnostic command: %v\n", err))...)
		}
		if err := os.WriteFile(filepath.Join(out, item.name), output, 0644); err != nil {
			failures = append(failures, err)
		}
	}
	// Inspect first: an already removed --rm invocation requires no action.
	if exec.CommandContext(ctx, "docker", "inspect", name).Run() == nil {
		if output, err := exec.CommandContext(ctx, "docker", "rm", "-f", name).CombinedOutput(); err != nil {
			failures = append(failures, fmt.Errorf("remove owned invocation: %w: %s", err, output))
		}
	}
	return errors.Join(failures...)
}
