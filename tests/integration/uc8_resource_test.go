//go:build integration

// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package integration

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func uc8FixtureFile(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	require.NoError(t, os.WriteFile(path, []byte(content), 0600))
	return path
}

func uc8PayloadFile(t *testing.T, name string, value any, format string) string {
	t.Helper()
	var data []byte
	var err error
	if format == "yaml" {
		data, err = yaml.Marshal(value)
	} else {
		data, err = json.Marshal(value)
	}
	require.NoError(t, err)
	return uc8FixtureFile(t, name, string(data))
}

func uc8InputCommand(t *testing.T, r *ochami_runner, input CLIInput, args ...string) string {
	t.Helper()
	args, err := r.envelopeArgs(args)
	require.NoError(t, err)
	result := r.execute(input, args...)
	require.NoError(t, result.Err, result.Stderr)
	require.Zero(t, result.ExitCode)
	return result.Stdout
}

func uc8Resource(t *testing.T, output, format string) map[string]any {
	t.Helper()
	var result map[string]any
	if format == "yaml" {
		require.NoError(t, yaml.Unmarshal([]byte(output), &result))
	} else {
		require.NoError(t, json.Unmarshal([]byte(output), &result))
	}
	// Normalize numeric types so JSON and YAML represent the same resource.
	data, err := json.Marshal(result)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(data, &result))
	// YAML omitempty suppresses the empty status object emitted by JSON.
	if _, exists := result["status"]; !exists {
		result["status"] = map[string]any{}
	}
	return result
}

func uc8ResourceUID(t *testing.T, r *ochami_runner, kind, name string) string {
	t.Helper()
	var resources []struct {
		Metadata struct{ Name, UID string } `json:"metadata"`
	}
	require.NoError(t, json.Unmarshal([]byte(uc8Command(t, r, "", "boot", kind, "list")), &resources))
	var owned []string
	for _, resource := range resources {
		if resource.Metadata.Name == name {
			require.NotEmpty(t, resource.Metadata.UID)
			owned = append(owned, resource.Metadata.UID)
		}
	}
	require.Len(t, owned, 1, "expected one owned %s named %s", kind, name)
	return owned[0]
}
