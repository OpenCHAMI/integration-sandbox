// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"encoding/json"
)

type actionHandler func(context.Context, controller, json.RawMessage) (json.RawMessage, error)

var actions = map[string]actionHandler{}

func registerAction(name string, fn actionHandler) {
	if name == "" || fn == nil || actions[name] != nil {
		panic("invalid or duplicate control action")
	}
	actions[name] = fn
}
