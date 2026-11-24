//go:build integration && declarative_boot

// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package declarativeboot

import (
	"bufio"
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

type safetyControlReceipt struct {
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

func safetyReceiptValid(r safetyControlReceipt, project string) bool {
	image, validPrefix := strings.CutPrefix(r.SourceImageID, "sha256:")
	imageBytes, imageErr := hex.DecodeString(image)
	if !r.Success || project == "" || r.Project != project || r.Service == "" || r.BeforeContainerID == "" || r.AfterContainerID == "" || r.SourceVolumeID == "" || r.TargetVolumeID == "" || !validPrefix || imageErr != nil || len(imageBytes) != 32 || r.TargetImageID != r.SourceImageID {
		return false
	}
	before, e1 := time.Parse(time.RFC3339Nano, r.BeforeStartedAt)
	after, e2 := time.Parse(time.RFC3339Nano, r.AfterStartedAt)
	if e1 != nil || e2 != nil {
		return false
	}
	switch r.Action {
	case "service-restart":
		return r.Service == "boot" && r.BeforeContainerID == r.AfterContainerID && after.After(before) && r.SourceVolumeID == r.TargetVolumeID
	case "service-recreate":
		return r.Service == "boot" && r.BeforeContainerID != r.AfterContainerID && r.SourceVolumeID == r.TargetVolumeID
	case "backup-restore":
		return r.Service == "boot" && r.BeforeContainerID != r.AfterContainerID && r.SourceVolumeID != r.TargetVolumeID
	case "snapshot-store":
		return r.BeforeContainerID == r.AfterContainerID && after.After(before) && r.SourceVolumeID == r.TargetVolumeID && (r.Instance == "auth-off" || (r.Instance == "main" && r.Service == "boot"))
	default:
		return false
	}
}

func safetyControlAction(t *testing.T, ctx context.Context, action string, request, response any) safetyControlReceipt {
	t.Helper()
	path := safetyEnv(t, "SBX_DECLARATIVE_CONTROL_RECEIPTS")
	file, err := os.Open(path)
	if err != nil {
		t.Fatal("control receipt file unavailable")
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		t.Fatal("cannot stat control receipts")
	}
	body, err := json.Marshal(request)
	if err != nil {
		t.Fatal("cannot encode control request")
	}
	output, err := exec.CommandContext(ctx, safetyEnv(t, "SBX_DECLARATIVE_CONTROL_BIN"), action, string(body)).Output()
	if err != nil {
		t.Fatalf("control action %s failed", action)
	}
	if err := json.Unmarshal(output, response); err != nil {
		t.Fatalf("control action %s returned invalid JSON", action)
	}
	currentInfo, err := os.Stat(path)
	if err != nil || !os.SameFile(info, currentInfo) || currentInfo.Size() <= info.Size() {
		t.Fatal("control receipt file was replaced or not appended")
	}
	if _, err := file.Seek(info.Size(), io.SeekStart); err != nil {
		t.Fatal("cannot seek control receipts")
	}
	appended, err := io.ReadAll(io.LimitReader(file, 1024*1024+1))
	if err != nil || len(appended) == 0 || len(appended) > 1024*1024 || appended[len(appended)-1] != '\n' {
		t.Fatal("control receipt append is incomplete")
	}
	scanner := bufio.NewScanner(bytes.NewReader(appended))
	var receipt safetyControlReceipt
	count := 0
	for scanner.Scan() {
		var current safetyControlReceipt
		if json.Unmarshal(scanner.Bytes(), &current) != nil {
			t.Fatal("invalid control receipt")
		}
		if current.Action == action {
			receipt = current
			count++
		}
	}
	if scanner.Err() != nil || count != 1 || !safetyReceiptValid(receipt, safetyEnv(t, "SBX_DECLARATIVE_COMPOSE_PROJECT")) {
		t.Fatalf("missing or invalid %s receipt", action)
	}
	return receipt
}
