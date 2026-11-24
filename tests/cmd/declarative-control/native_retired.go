// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"encoding/json"
)

func retiredReference(ctx context.Context, c controller, raw json.RawMessage) (json.RawMessage, error) {
	var req struct{}
	if err := strictRequest(raw, &req); err != nil {
		return nil, err
	}
	before, err := c.locate(ctx, "boot")
	if err != nil || !before.State.Running || before.State.Paused || before.Image != c.imageID || c.imageID == "" {
		return nil, errDocker
	}
	var name, uid string
	err = c.withStopped(ctx, before, func(data string) error {
		var fixtureErr error
		name, uid, fixtureErr = makeRetiredFixture(data)
		return fixtureErr
	})
	if err != nil {
		return nil, err
	}
	return json.Marshal(struct {
		SeriesName  string `json:"seriesName"`
		RevisionUID string `json:"revisionUid"`
	}{name, uid})
}

func init() { registerAction("retired-reference-fixture", retiredReference) }
