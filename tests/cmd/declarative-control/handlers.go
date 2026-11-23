// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"encoding/json"
)

func init() {
	registerAction("token-fixture", tokenFixture)
	for _, name := range []string{"service-stop", "service-start", "service-restart", "service-recreate"} {
		registerAction(name, func(ctx context.Context, c controller, raw json.RawMessage) (json.RawMessage, error) {
			req, err := decodeRequest[struct {
				Service string `json:"service"`
			}](raw)
			if err != nil {
				return nil, err
			}
			if err := c.lifecycle(ctx, name, req.Service); err != nil {
				return nil, err
			}
			return json.RawMessage(`{}`), nil
		})
	}
	registerAction("snapshot-store", func(ctx context.Context, c controller, raw json.RawMessage) (json.RawMessage, error) {
		req, err := decodeRequest[struct {
			Instance string `json:"instance"`
		}](raw)
		if err != nil {
			return nil, err
		}
		digest, err := c.snapshot(ctx, req.Instance)
		if err != nil {
			return nil, err
		}
		return json.Marshal(struct {
			Digest string `json:"configurationDigest"`
		}{digest})
	})
}
