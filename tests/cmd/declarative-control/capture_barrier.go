// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"errors"
	"os"

	"github.com/openchami/integration-sandbox/tests/internal/quiescence"
)

func (c controller) capturePath() (string, error) {
	path := os.Getenv("SBX_DECLARATIVE_QUIESCENCE_LOCK")
	if path == c.runDir || ownedPath(c.runDir, path, false) != nil {
		return "", errPath
	}
	return path, nil
}

func (c controller) withCapture(ctx context.Context, work func() error) (result error) {
	path, err := c.capturePath()
	if err != nil {
		return err
	}
	lease, err := quiescence.AcquireCapture(ctx, path)
	if err != nil {
		return err
	}
	defer func() { result = errors.Join(result, lease.Close()) }()
	return work()
}
