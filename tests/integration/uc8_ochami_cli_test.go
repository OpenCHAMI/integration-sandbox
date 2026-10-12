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
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"text/template"
	"time"

	"github.com/stretchr/testify/assert"
)

// UC8 exercises the CLI binary against live services. Unique resource names
// make the SMD and boot POST-to-GET checks resistant to canned responses.

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

func (r *ochami_runner) sync_config() (err error) {
	tmpl, err := template.New("config").Parse(config_template)
	if err != nil {
		return fmt.Errorf("config template error: %w", err)
	}

	if r.config_path == "" {
		return fmt.Errorf("config_path is not set")
	}

	f, err := os.Create(r.config_path)
	if err != nil {
		return fmt.Errorf("unable to create config file: %w", err)
	}
	defer func() {
		if closeErr := f.Close(); err == nil {
			err = closeErr
		}
	}()

	err = tmpl.Execute(f, Endpoints)
	if err != nil {
		return fmt.Errorf("config template execution error: %w", err)
	}

	return nil
}

func (r *ochami_runner) run(stdin string, args []string) (stdout string, stderr string, err error) {
	stdin_reader := strings.NewReader(stdin)

	stdout_writer := bytes.NewBuffer(nil)
	stderr_writer := bytes.NewBuffer(nil)

	abs_config_path, err := filepath.Abs(r.config_path)
	if err != nil {
		return "", "", fmt.Errorf("unable to resolve config file path: %w", err)
	}

	cmd := exec.CommandContext(r.ctx,
		"docker", "compose",
		"-f", "../../compose/infra.yaml",
		"-f", "../../compose/bmc-sim.yaml",
		"-f", "../../compose/core.yaml",
		"run", "--rm", "-T",
		"--volume", abs_config_path+":/config.yaml",
		"--entrypoint", "ochami",
		"--env", "TEST_ACCESS_TOKEN="+r.access_token,
		"ochami-runner",
		"--config", "/config.yaml",
	)

	cmd.Env = os.Environ()
	for k, v := range r.env {
		cmd.Env = append(cmd.Env, k+"="+v)
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
		"exec", "-T", "--interactive=false", "tokensmith",
		"tokensmith", "user-token", "create",
		"--subject", "testing",
		"--scopes", "admin,audit",
		"--enable-local-user-mint",
		"--key-file", "/tokensmith/keys/private.pem",
		"--audience", "smd",
		"--issuer", Endpoints["tokensmith"],
	)

	cmd.Env = os.Environ()
	for k, v := range r.env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}

	stdout, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return fmt.Errorf("mint access token: %w: %s", err, strings.TrimSpace(string(exitErr.Stderr)))
		}
		return fmt.Errorf("mint access token: %w", err)
	}

	r.access_token = strings.TrimSpace(string(stdout))

	return nil
}

// cleanupUC8Resources tolerates absent records but reports all other failures.
// HTTP cleanup does not depend on the CLI context, which may have expired.
func cleanupUC8Resources(ctx context.Context, name, token string) error {
	var failures []error
	request := func(method, uri string) (*http.Response, error) {
		req, err := http.NewRequestWithContext(ctx, method, uri, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		return httpClient.Do(req)
	}
	remove := func(uri string) {
		resp, err := request(http.MethodDelete, uri)
		if err != nil {
			failures = append(failures, fmt.Errorf("DELETE %s: %w", uri, err))
			return
		}
		defer resp.Body.Close()
		if (resp.StatusCode < 200 || resp.StatusCode >= 300) && resp.StatusCode != http.StatusNotFound {
			failures = append(failures, fmt.Errorf("DELETE %s: HTTP %d", uri, resp.StatusCode))
		}
	}

	bootURI := Endpoints["boot"] + "/bootconfigurations"
	resp, err := request(http.MethodGet, bootURI)
	if err != nil {
		failures = append(failures, fmt.Errorf("list boot configurations: %w", err))
	} else {
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			failures = append(failures, fmt.Errorf("list boot configurations: HTTP %d", resp.StatusCode))
		} else {
			var configs []struct {
				Metadata struct {
					Name string `json:"name"`
					UID  string `json:"uid"`
				} `json:"metadata"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&configs); err != nil {
				failures = append(failures, fmt.Errorf("decode boot configurations: %w", err))
			} else {
				for _, config := range configs {
					if config.Metadata.Name != name {
						continue
					}
					if config.Metadata.UID == "" {
						failures = append(failures, fmt.Errorf("boot configuration %s has no UID", name))
						continue
					}
					remove(bootURI + "/" + config.Metadata.UID)
				}
			}
		}
	}
	remove(Endpoints["smd"] + "/hsm/v2/groups/" + name)
	return errors.Join(failures...)
}

func TestUC8_Ochami_CLI(t *testing.T) {
	// Each run owns a unique group and boot configuration, including on a
	// persistent stack with unrelated records or leftovers from interrupted runs.
	resourceName := fmt.Sprintf("uc8-%d", time.Now().UnixNano())
	var ochami_tests = []struct {
		name  string
		stdin string
		args  []string

		expected_stdout   string
		log_stdout        bool
		skip_stdout_check bool
		compare_json      bool
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
			args:            []string{"smd", "group", "add", resourceName},
			expected_stdout: "",
		},
		{
			name:            "smd group add member x0c0s0b0",
			args:            []string{"smd", "group", "member", "add", resourceName, "x0c0s0b0"},
			expected_stdout: "",
		},
		{
			name:            "smd group get testing",
			args:            []string{"smd", "group", "get", "--name", resourceName},
			expected_stdout: fmt.Sprintf(`[{"description":"","label":%q,"members":{"ids":["x0c0s0b0"]}}]`, resourceName),
			compare_json:    true,
		},
		{
			name:            "smd group membership testing",
			args:            []string{"smd", "group", "get", "--name", resourceName},
			expected_stdout: fmt.Sprintf(`[{"description":"","label":%q,"members":{"ids":["x0c0s0b0"]}}]`, resourceName),
			compare_json:    true,
		},
		{
			name:            "ochami boot config add",
			args:            []string{"boot", "config", "add", "-d", "@-", "-f", "json"},
			expected_stdout: "",
			stdin: fmt.Sprintf(`{"metadata":{"name":%q},"spec":
			{
				"kernel": "http://fake-address/vmlinuz",
				"initrd": "http://fake-address/initramfs.img",
				"params": "nomodeset ro root=live:http://fake-address/fake-image",
				"macs": ["02:00:00:00:00:00"]
			}}`, resourceName),
		},
		{
			name:            "ochami boot config list",
			args:            []string{"boot", "config", "list"},
			expected_stdout: `[{"apiVersion":"v1","kind":"BootConfiguration","metadata":null,"spec":{"initrd":"http://fake-address/initramfs.img","kernel":"http://fake-address/vmlinuz","macs":["02:00:00:00:00:00"],"params":"nomodeset ro root=live:http://fake-address/fake-image"},"status":{}}]`,
			compare_json:    true,
			// Gotta remove date metadata from the output
			output_transform: func(in string) (out string, err error) {
				var temp []map[string]any
				err = json.Unmarshal([]byte(in), &temp)
				if err != nil {
					return
				}

				owned := make([]map[string]any, 0, 1)
				for _, o := range temp {
					metadata, ok := o["metadata"].(map[string]any)
					if ok && metadata["name"] == resourceName {
						o["metadata"] = nil
						owned = append(owned, o)
					}
				}

				out_byte, err := json.Marshal(owned)
				out = string(out_byte)
				return
			},
		},
		{
			name:            "ochami pcs status show x0c0s0b0",
			args:            []string{"pcs", "status", "show", "x0c0s0b0"},
			expected_stdout: `{"error":"","lastUpdated":null,"managementState":"available","powerState":"on","supportedPowerTransitions":[],"xname":"x0c0s0b0"}`,
			compare_json:    true,
			// Remove lastUpdated timestamp
			output_transform: func(in string) (out string, err error) {
				var temp map[string]any
				err = json.Unmarshal([]byte(in), &temp)
				if err != nil {
					return
				}

				for k := range temp {
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
		config_path: filepath.Join(t.TempDir(), "config.yaml"),
		env:         env,
	}

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
	t.Cleanup(func() {
		// Cleanup must still run after the command deadline or a failed subtest.
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		if err := cleanupUC8Resources(cleanupCtx, resourceName, ochami.access_token); err != nil {
			t.Errorf("clean up CLI resources: %v", err)
		}
	})

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
				if test.compare_json {
					if !assert.JSONEqf(t,
						test.expected_stdout,
						stdout,
						"Bad output, expected '%s' and got '%s'\n%s",
						test.expected_stdout, stdout, stderr,
					) {
						die = true
						return
					}
				} else {
					if !assert.Equalf(t,
						strings.TrimSpace(test.expected_stdout),
						strings.TrimSpace(stdout),
						"Bad output, expected '%s' and got '%s'\n%s",
						test.expected_stdout, stdout, stderr,
					) {
						die = true
						return
					}
				}
			}
		})
	}
}
