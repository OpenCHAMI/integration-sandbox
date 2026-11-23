// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func strictRequest(raw json.RawMessage, value interface{}) error {
	if len(raw) == 0 || len(raw) > 16384 || raw[0] != '{' || !uniqueJSON(raw) {
		return errInput
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	if err := decoder.Decode(new(json.RawMessage)); err != io.EOF {
		return errDocker
	}
	return nil
}

func (c controller) endpoint(ctx context.Context, id, port, health string) (string, error) {
	out, err := c.exec.run(ctx, "port", id, port)
	if err != nil {
		return "", errDocker
	}
	host, number, err := net.SplitHostPort(strings.TrimSpace(string(out)))
	portNumber, parseErr := strconv.Atoi(number)
	if err != nil || host != "127.0.0.1" || parseErr != nil || portNumber < 1 || portNumber > 65535 {
		return "", errDocker
	}
	base := "http://" + net.JoinHostPort(host, number)
	wait, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	client := &http.Client{Timeout: 2 * time.Second}
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		req, err := http.NewRequestWithContext(wait, http.MethodGet, base+health, nil)
		if err != nil {
			return "", err
		}
		resp, err := client.Do(req)
		if err == nil {
			if err := resp.Body.Close(); err != nil {
				return "", err
			}
			if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusNoContent {
				return base, nil
			}
		}
		select {
		case <-wait.Done():
			return "", errDocker
		case <-ticker.C:
		}
	}
}
