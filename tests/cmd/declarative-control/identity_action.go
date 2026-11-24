// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"encoding/json"
)

func init() { registerAction("prepare-boot-identity", prepareInitialIdentity) }

func prepareInitialIdentity(ctx context.Context, c controller, raw json.RawMessage) (json.RawMessage, error) {
	if _, err := decodeRequest[struct{}](raw); err != nil {
		return nil, err
	}
	if err := c.withCapture(ctx, func() error { return c.prepareBootIdentity(ctx) }); err != nil {
		return nil, errIdentity
	}
	return json.RawMessage(`{}`), nil
}
