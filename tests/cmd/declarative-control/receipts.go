// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"time"
)

type receipt struct {
	Action            string `json:"action"`
	Project           string `json:"project"`
	Service           string `json:"service"`
	Instance          string `json:"instance"`
	BeforeContainerID string `json:"beforeContainerID"`
	AfterContainerID  string `json:"afterContainerID"`
	BeforeStartedAt   string `json:"beforeStartedAt"`
	AfterStartedAt    string `json:"afterStartedAt"`
	SourceVolumeID    string `json:"sourceVolumeID"`
	TargetVolumeID    string `json:"targetVolumeID"`
	SourceImageID     string `json:"sourceImageID"`
	TargetImageID     string `json:"targetImageID"`
	Success           bool   `json:"success"`
}

var errReceipt = errors.New("invalid receipt transition")

func (c controller) appendReceipt(action, service, instance string, before, after container) error {
	source, err := c.dataDir(before)
	if err != nil {
		return err
	}
	target, err := c.dataDir(after)
	if err != nil {
		return err
	}
	a, errA := time.Parse(time.RFC3339Nano, before.State.StartedAt)
	b, errB := time.Parse(time.RFC3339Nano, after.State.StartedAt)
	digest, ok := strings.CutPrefix(before.Image, "sha256:")
	decoded, errHex := hex.DecodeString(digest)
	if errA != nil || errB != nil || !ok || errHex != nil || len(decoded) != 32 || before.Image != after.Image || !after.State.Running {
		return errReceipt
	}
	switch action {
	case "snapshot-store":
		if before.ID != after.ID || !b.After(a) || source != target {
			return errReceipt
		}
	case "service-restart":
		if before.ID != after.ID || !b.After(a) || source != target {
			return errReceipt
		}
	case "service-recreate":
		if before.ID == after.ID || source != target {
			return errReceipt
		}
	case "backup-restore":
		if before.ID == after.ID || source == target {
			return errReceipt
		}
	default:
		return errReceipt
	}
	r := receipt{action, c.project, service, instance, before.ID, after.ID, before.State.StartedAt, after.State.StartedAt, source, target, before.Image, after.Image, true}
	return appendJSON(c.runDir, c.receipts, r)
}

func appendJSON(root, path string, value interface{}) error {
	if err := ownedPath(root, path, false); err != nil {
		return err
	}
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = file.Write(append(data, '\n'))
	return err
}
