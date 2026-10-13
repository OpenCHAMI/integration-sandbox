//go:build integration

// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package integration

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Contract cases execute the real CLI image but do not require live services.
func uc8ContractRunner(t *testing.T) *ochami_runner {
	t.Helper()
	env, err := LoadImages()
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	t.Cleanup(cancel)
	r := &ochami_runner{ctx: ctx, t: t, env: env, config_path: filepath.Join(t.TempDir(), "config.yaml")}
	require.NoError(t, r.sync_config())
	return r
}

func TestUC8_RunnerResult(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		r := uc8ContractRunner(t)
		result := r.execute(CLIInput{IgnoreConfig: true}, "version")
		require.NoError(t, result.Err, result.Stderr)
		require.Zero(t, result.ExitCode)
		require.NoError(t, validateUC8Version(result.Stdout))
	})
	t.Run("cli_failure", func(t *testing.T) {
		r := uc8ContractRunner(t)
		result := r.execute(CLIInput{IgnoreConfig: true}, "uc8-unknown-command")
		var exitErr *exec.ExitError
		require.ErrorAs(t, result.Err, &exitErr)
		require.Greater(t, result.ExitCode, 0)
		require.Contains(t, result.Stderr, "unknown command")
	})
	t.Run("canceled_context", func(t *testing.T) {
		r := uc8ContractRunner(t)
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		r.ctx = ctx
		result := r.execute(CLIInput{IgnoreConfig: true}, "version")
		require.ErrorIs(t, result.Err, context.Canceled)
		require.Equal(t, -1, result.ExitCode)
		require.Error(t, exec.Command("docker", "inspect", result.ContainerName).Run(), "owned invocation must be absent")
	})
	t.Run("invalid_mount", func(t *testing.T) {
		r := uc8ContractRunner(t)
		result := r.execute(CLIInput{Files: map[string]string{r.config_path: "relative-path"}}, "version")
		require.ErrorContains(t, result.Err, "invalid fixture mount")
		require.Equal(t, -1, result.ExitCode)
	})
}
