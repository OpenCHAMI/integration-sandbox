//go:build integration
// +build integration

// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
//
// SPDX-License-Identifier: MIT

package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"text/template"
	"time"

	"github.com/pkg/errors"
)

// TestUC8_Ochami_CLI tests the Ochami CLI against various endpoints:
//
//  1. Runs through all the flags on the commandline tool, doing GETs after
//     POSTs to read dynamically configured data.
//
// Stub-resistance: Uses some dynamic data to help prevent stub responses and
// requests

const config_template = `
default-cluster: test
log:
    format: rfc3339
    level: debug
timeout: 30s
clusters:
    - name: test
      cluster:
        smd:
          uri: {{ .smd }}/hsm/v2
        bss:
          uri: {{ .boot }}
        cloud-init:
          uri: {{ .metadata }}
        pcs:
          uri: {{ .power }}
        boot-service:
          api-version: v1
          uri: {{ .boot }}
`

type ochami_runner struct {
	ctx context.Context
	t   *testing.T

	config_path  string
	access_token string
	env          map[string]string
}

func (r *ochami_runner) clean() error {
	err := os.Remove(r.config_path)
	return err
}

func (r *ochami_runner) sync_config() error {
	tmpl, err := template.New("config").Parse(config_template)
	if err != nil {
		return errors.Wrap(err, "config template error")
	}

	if r.config_path == "" {
		return fmt.Errorf("config_path is not set")
	}

	f, err := os.Create(r.config_path)
	if err != nil {
		return errors.Wrap(err, "unable to create config file")
	}
	defer f.Close()

	err = tmpl.Execute(f, Endpoints)
	if err != nil {
		return errors.Wrap(err, "config template execution error")
	}

	return nil
}

func (r *ochami_runner) run(stdin string, args []string) (stdout string, stderr string, err error) {
	stdin_reader := strings.NewReader(stdin)

	stdout_writer := bytes.NewBuffer(nil)
	stderr_writer := bytes.NewBuffer(nil)

	abs_config_path, err := filepath.Abs(r.config_path)
	if err != nil {
		return "", "", errors.Wrap(err, "unable to resolve config file path")
	}

	cmd := exec.CommandContext(r.ctx,
		"docker", "compose",
		"-f", "../../compose/infra.yaml",
		"-f", "../../compose/bmc-sim.yaml",
		"-f", "../../compose/core.yaml",
		"run", "--rm",
		"--volume", abs_config_path+":/config.yaml",
		"--entrypoint", "ochami",
		"--env", "TEST_ACCESS_TOKEN="+r.access_token,
		"ochami-runner",
		"--config", "/config.yaml",
	)

	for k, v := range r.env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}

	for _, line := range os.Environ() {
		cmd.Env = append(cmd.Env, line)
	}

	cmd.Args = append(cmd.Args, args...)

	cmd.Stdin = stdin_reader
	cmd.Stdout = stdout_writer
	cmd.Stderr = stderr_writer

	err = cmd.Run()
	stdout = stdout_writer.String()
	stderr = stderr_writer.String()
	return
}

func (r *ochami_runner) gen_access_token() error {
	cmd := exec.CommandContext(r.ctx,
		"docker", "compose",
		"-f", "../../compose/infra.yaml",
		"-f", "../../compose/bmc-sim.yaml",
		"-f", "../../compose/core.yaml",
		"exec", "-it", "tokensmith",
		"tokensmith", "user-token", "create",
		"--subject", "testing",
		"--scopes", "admin,audit",
		"--enable-local-user-mint",
		"--key-file", "/tokensmith/keys/private.pem",
		"--audience", "smd",
		"--issuer", Endpoints["tokensmith"],
	)
	stdout, err := cmd.Output()
	if err != nil {
		return err
	}

	r.access_token = strings.TrimSpace(string(stdout))

	return nil
}

func TestUC8_Ochami_CLI(t *testing.T) {
	var ochami_tests = []struct {
		name  string
		stdin string
		args  []string

		expected_stdout   string
		log_stdout        bool
		skip_stdout_check bool
		output_transform  func(string) (string, error)
	}{
		{
			name:              "version",
			args:              []string{"version"},
			log_stdout:        true,
			skip_stdout_check: true,
		},
		{
			name:            "smd group add",
			args:            []string{"smd", "group", "add", "testing"},
			expected_stdout: "",
		},
		{
			name:            "smd group add member x0c0s0b0",
			args:            []string{"smd", "group", "member", "add", "testing", "x0c0s0b0"},
			expected_stdout: "",
		},
		{
			name:            "smd group get testing",
			args:            []string{"smd", "group", "get", "--name", "testing"},
			expected_stdout: `[{"description":"","label":"testing","members":{"ids":["x0c0s0b0"]}}]`,
		},
		{
			name:            "smd group membership testing",
			args:            []string{"smd", "group", "get", "--name", "testing"},
			expected_stdout: `[{"description":"","label":"testing","members":{"ids":["x0c0s0b0"]}}]`,
		},
		{
			name:            "ochami boot config add",
			args:            []string{"boot", "config", "add", "-d", "@-", "-f", "json"},
			expected_stdout: "",
			stdin: `{"spec":
			{
				"kernel": "http://fake-address/vmlinuz",
				"initrd": "http://fake-address/initramfs.img",
				"params": "nomodeset ro root=live:http://fake-address/fake-image",
				"macs": ["02:00:00:00:00:00"]
			}}`,
		},
		{
			name:            "ochami boot config list",
			args:            []string{"boot", "config", "list"},
			expected_stdout: `[{"apiVersion":"v1","kind":"BootConfiguration","metadata":null,"spec":{"initrd":"http://fake-address/initramfs.img","kernel":"http://fake-address/vmlinuz","macs":["02:00:00:00:00:00"],"params":"nomodeset ro root=live:http://fake-address/fake-image"},"status":{}}]`,
			// Gotta remove date metadata from the output
			output_transform: func(in string) (out string, err error) {
				var temp []map[string]any
				err = json.Unmarshal([]byte(in), &temp)
				if err != nil {
					return
				}

				for _, o := range temp {
					for k, _ := range o {
						if k == "metadata" {
							o[k] = nil
						}
					}
				}

				out_byte, err := json.Marshal(temp)
				out = string(out_byte)
				return
			},
		},
		{
			name:            "ochami pcs status show x0c0s0b0",
			args:            []string{"pcs", "status", "show", "x0c0s0b0"},
			expected_stdout: `{"error":"","lastUpdated":null,"managementState":"available","powerState":"on","supportedPowerTransitions":[],"xname":"x0c0s0b0"}`,
			// Remove lastUpdated timestamp
			output_transform: func(in string) (out string, err error) {
				var temp map[string]any
				err = json.Unmarshal([]byte(in), &temp)
				if err != nil {
					return
				}

				for k, _ := range temp {
					if k == "lastUpdated" {
						temp[k] = nil
					}
				}

				out_byte, err := json.Marshal(temp)
				out = string(out_byte)
				return
			},
		},
	}
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()

	env, err := LoadImages()
	if err != nil {
		t.Errorf("Unable to load image tags: %v", err)
		return
	}

	ochami := &ochami_runner{
		ctx:         ctx,
		t:           t,
		config_path: "./config.yaml",
		env:         env,
	}
	defer ochami.clean()

	err = ochami.sync_config()
	if err != nil {
		t.Errorf("Unable to sync config: %v", err)
		return
	}

	err = ochami.gen_access_token()
	if err != nil {
		t.Errorf("Unable to get access token: %v", err)
		return
	}

	die := false
	for i, test := range ochami_tests {
		if die == true {
			t.Errorf("Aborting tests!")
			return
		}
		if test.name == "" {
			t.Errorf("Test #%d is missing a name", i)
			return
		}
		t.Run(test.name, func(t *testing.T) {
			stdout, stderr, err := ochami.run(test.stdin, test.args)
			status, ok := err.(*exec.ExitError)
			if ok && status.ExitCode() != 0 {
				t.Errorf("Process exited with code %d: %s", status.ExitCode(), stderr)
				die = true
				return
			}
			if err != nil {
				t.Errorf("Error executing process: %v\n%s", err, stderr)
				die = true
				return
			}

			if test.log_stdout == true {
				t.Log(stdout)
			}

			if test.output_transform != nil {
				stdout, err = test.output_transform(stdout)
				if err != nil {
					t.Errorf("Unable to transform output: %v", err)
					die = true
					return
				}
			}

			if !test.skip_stdout_check {
				// TODO: Should this compare the normalized JSON instead of the bytes?
				if strings.TrimSpace(stdout) != strings.TrimSpace(test.expected_stdout) {
					t.Errorf("Bad output, expected '%s' and got '%s'\n%s", test.expected_stdout, stdout, stderr)
					die = true
					return
				}
			}
		})
	}
}
