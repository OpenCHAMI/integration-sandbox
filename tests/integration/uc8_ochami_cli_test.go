//go:build integration
// +build integration

// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
//
// SPDX-License-Identifier: MIT

package integration

import (
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

	"github.com/stretchr/testify/require"
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

func (r *ochami_runner) run(stdin string, args []string) (string, string, error) {
	result := r.execute(CLIInput{Stdin: stdin}, args...)
	return result.Stdout, result.Stderr, result.Err
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

// validateUC8Version accepts release and development build values while requiring
// the identifying fields printed by the CLI's version command.
func validateUC8Version(out string) error {
	fields := make(map[string]string)
	for _, line := range strings.Split(out, "\n") {
		key, value, ok := strings.Cut(line, ":")
		if ok {
			fields[strings.TrimSpace(key)] = strings.TrimSpace(value)
		}
	}
	for _, key := range []string{"Version", "Go", "Compiler"} {
		if fields[key] == "" {
			return fmt.Errorf("version output missing nonempty %s field", key)
		}
	}
	return nil
}

type uc8PowerStatus struct {
	Xname           string `json:"xname"`
	Error           string `json:"error"`
	PowerState      string `json:"powerState"`
	ManagementState string `json:"managementState"`
}

func (s uc8PowerStatus) ready(xname string) error {
	if s.Xname != xname || s.Error != "" || s.PowerState != "on" || s.ManagementState != "available" {
		return fmt.Errorf("PCS component not ready: %+v", s)
	}
	return nil
}

// waitUC8PCSReady checks service data, not just HTTP 200: PCS returns error
// entries with a successful HTTP status while discovery or sampling is pending.
func waitUC8PCSReady(ctx context.Context, xname string) error {
	var lastErr error
	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet,
			Endpoints["power"]+"/v1/power-status?xname="+xname, nil)
		if err != nil {
			return err
		}
		resp, err := httpClient.Do(req)
		if err == nil {
			var body struct {
				Status []uc8PowerStatus `json:"status"`
			}
			if resp.StatusCode != http.StatusOK {
				err = fmt.Errorf("PCS status: HTTP %d", resp.StatusCode)
			} else if err = json.NewDecoder(resp.Body).Decode(&body); err == nil {
				err = fmt.Errorf("PCS returned no status for %s", xname)
				for _, status := range body.Status {
					if status.Xname == xname {
						err = status.ready(xname)
						break
					}
				}
			}
			resp.Body.Close()
		}
		if err == nil {
			return nil
		}
		lastErr = err
		select {
		case <-ctx.Done():
			return fmt.Errorf("waiting for PCS %s: %w (last response: %v)", xname, ctx.Err(), lastErr)
		case <-time.After(2 * time.Second):
		}
	}
}

func uc8Command(t *testing.T, r *ochami_runner, stdin string, args ...string) string {
	t.Helper()
	stdout, stderr, err := r.run(stdin, args)
	require.NoErrorf(t, err, "ochami %v failed: %s", args, stderr)
	return strings.TrimSpace(stdout)
}

func TestUC8_Ochami_CLI(t *testing.T) {
	resourceName := fmt.Sprintf("uc8-%d", time.Now().UnixNano())
	env, err := LoadImages()
	require.NoError(t, err, "load image tags")
	base := ochami_runner{
		config_path: filepath.Join(t.TempDir(), "config.yaml"),
		env:         env,
	}
	require.NoError(t, base.sync_config(), "write CLI configuration")

	// Independent service flows continue even if another flow fails. Each has
	// its own deadline; discovery cannot consume the SMD or boot command budget.
	runner := func(t *testing.T, timeout time.Duration) *ochami_runner {
		t.Helper()
		ctx, cancel := context.WithTimeout(t.Context(), timeout)
		t.Cleanup(cancel)
		r := base
		r.ctx, r.t = ctx, t
		return &r
	}
	cleanupRegistered := false
	prepareToken := func(child *testing.T, r *ochami_runner) {
		child.Helper()
		require.NoError(child, r.gen_access_token(), "mint access token")
		if !cleanupRegistered {
			cleanupRegistered = true
			token := r.access_token
			t.Cleanup(func() {
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				if err := cleanupUC8Resources(ctx, resourceName, token); err != nil {
					t.Errorf("clean up CLI resources: %v", err)
				}
			})
		}
	}

	t.Run("version", func(t *testing.T) {
		r := runner(t, 30*time.Second)
		out := uc8Command(t, r, "", "version")
		require.NoError(t, validateUC8Version(out), "version output: %s", out)
		t.Log(out)
	})
	t.Run("smd_group_membership", func(t *testing.T) {
		r := runner(t, time.Minute)
		prepareToken(t, r)
		require.Empty(t, uc8Command(t, r, "", "smd", "group", "add", resourceName))
		require.Empty(t, uc8Command(t, r, "", "smd", "group", "member", "add", resourceName, "x0c0s0b0"))
		out := uc8Command(t, r, "", "smd", "group", "get", "--name", resourceName)
		require.JSONEq(t, fmt.Sprintf(`[{"description":"","label":%q,"members":{"ids":["x0c0s0b0"]}}]`, resourceName), out)
		require.JSONEq(t, `{"ids":["x0c0s0b0"]}`, uc8Command(t, r, "", "smd", "group", "member", "get", resourceName))
		uc8Command(t, r, "", "smd", "group", "update", "--description", "uc8 updated", "--tag", resourceName, resourceName)
		var updated []struct {
			Label       string   `json:"label"`
			Description string   `json:"description"`
			Tags        []string `json:"tags"`
		}
		require.NoError(t, json.Unmarshal([]byte(uc8Command(t, r, "", "smd", "group", "get", "--name", resourceName)), &updated))
		require.Len(t, updated, 1)
		require.Equal(t, resourceName, updated[0].Label)
		require.Equal(t, "uc8 updated", updated[0].Description)
		require.Contains(t, updated[0].Tags, resourceName)
		require.JSONEq(t, `{"ids":["x0c0s0b0"]}`, uc8Command(t, r, "", "smd", "group", "member", "get", resourceName), "description/tag update must preserve membership")

		require.Empty(t, uc8Command(t, r, "", "smd", "group", "member", "delete", "--no-confirm", resourceName, "x0c0s0b0"))
		out = uc8Command(t, r, "", "smd", "group", "member", "get", resourceName)
		require.JSONEq(t, `{"ids":[]}`, out,
			"member deletion must be observable in the group GET")
		uc8Command(t, r, "", "smd", "group", "delete", "--no-confirm", resourceName)
		require.JSONEq(t, `[]`, uc8Command(t, r, "", "smd", "group", "get", "--name", resourceName))
	})
	t.Run("boot_configuration", func(t *testing.T) {
		r := runner(t, time.Minute)
		prepareToken(t, r)
		input := fmt.Sprintf(`{"metadata":{"name":%q},"spec":{"kernel":"http://fake-address/vmlinuz","initrd":"http://fake-address/initramfs.img","params":"nomodeset ro root=live:http://fake-address/fake-image","macs":["02:00:00:00:00:00"]}}`, resourceName)
		require.Empty(t, uc8Command(t, r, input, "boot", "config", "add", "-d", "@-", "-f", "json"))
		out := uc8Command(t, r, "", "boot", "config", "list")
		var configs []map[string]any
		require.NoError(t, json.Unmarshal([]byte(out), &configs))
		owned := make([]map[string]any, 0, 1)
		for _, config := range configs {
			metadata, ok := config["metadata"].(map[string]any)
			if ok && metadata["name"] == resourceName {
				require.NotEmpty(t, metadata["uid"], "created configuration must have a UID")
				config["metadata"] = nil
				owned = append(owned, config)
			}
		}
		actual, err := json.Marshal(owned)
		require.NoError(t, err)
		require.JSONEq(t, `[{"apiVersion":"v1","kind":"BootConfiguration","metadata":null,"spec":{"initrd":"http://fake-address/initramfs.img","kernel":"http://fake-address/vmlinuz","macs":["02:00:00:00:00:00"],"params":"nomodeset ro root=live:http://fake-address/fake-image"},"status":{}}]`, string(actual))
	})
	t.Run("pcs_status", func(t *testing.T) {
		r := runner(t, 3*time.Minute)
		prepareToken(t, r)
		ensureBMCDiscovered(r.ctx, t, Endpoints["smd"], "x0c0s0b0", env)
		require.NoError(t, waitUC8PCSReady(r.ctx, "x0c0s0b0"))
		out := uc8Command(t, r, "", "pcs", "status", "show", "x0c0s0b0")
		var status uc8PowerStatus
		require.NoError(t, json.Unmarshal([]byte(out), &status))
		require.NoError(t, status.ready("x0c0s0b0"), "CLI status: %s", out)
	})
}
