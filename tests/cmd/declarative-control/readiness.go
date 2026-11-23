// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

func (c controller) configuredPort(ctx context.Context, port string) error {
	out, err := c.exec.run(ctx, "compose", "--project-name", c.project, "--file", c.compose, "config", "--format", "json")
	if err != nil {
		return errDocker
	}
	var config struct {
		Services map[string]struct {
			Ports []struct {
				HostIP    string          `json:"host_ip"`
				Target    int             `json:"target"`
				Published json.RawMessage `json:"published"`
			} `json:"ports"`
		} `json:"services"`
	}
	if json.Unmarshal(out, &config) != nil {
		return errDocker
	}
	ports := config.Services["boot"].Ports
	if len(ports) != 1 || ports[0].HostIP != "127.0.0.1" || ports[0].Target != 27790 || (string(ports[0].Published) != strconv.Quote(port) && string(ports[0].Published) != port) {
		return errDocker
	}
	return nil
}

func (c controller) port(ctx context.Context, id string) (string, error) {
	out, err := c.exec.run(ctx, "port", id, "27790/tcp")
	if err != nil {
		return "", errDocker
	}
	address := strings.TrimSpace(string(out))
	host, port, err := net.SplitHostPort(address)
	if err != nil || host != "127.0.0.1" || port == "" {
		return "", errDocker
	}
	return address, nil
}

func (c controller) samePort(ctx context.Context, before, afterID string) error {
	after, err := c.port(ctx, afterID)
	if err != nil || before != after {
		return errDocker
	}
	return nil
}

func waitReady(ctx context.Context, service string) error {
	env, path := "SBX_DECLARATIVE_BOOT_URL", "/health"
	if service == "smd" {
		env, path = "SBX_DECLARATIVE_SMD_URL", "/hsm/v2/service/ready"
	}
	if service == "boot-auth-off" {
		env = "SBX_DECLARATIVE_AUTH_OFF_URL"
	}
	u, err := loopbackURL(os.Getenv(env))
	if err != nil {
		return errDocker
	}
	u.Path = path
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	client := http.Client{Timeout: 2 * time.Second, Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
		if err != nil {
			return errDocker
		}
		response, err := client.Do(req)
		if err == nil {
			response.Body.Close()
			if response.StatusCode == http.StatusOK || response.StatusCode == http.StatusNoContent {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return errDocker
		case <-ticker.C:
		}
	}
}
