//go:build integration && declarative_boot

// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package declarativeboot

import (
	"strings"
	"testing"
)

func TestSafetyReceiptTransitions(t *testing.T) {
	image := "sha256:" + strings.Repeat("a", 64)
	base := safetyControlReceipt{Action: "service-restart", Project: "owned", Service: "boot", Success: true, BeforeContainerID: "c1", AfterContainerID: "c1", BeforeStartedAt: "2026-01-01T00:00:00Z", AfterStartedAt: "2026-01-01T00:01:00Z", SourceVolumeID: "v1", TargetVolumeID: "v1", SourceImageID: image, TargetImageID: image}
	for _, tc := range []struct {
		name   string
		change func(*safetyControlReceipt)
		valid  bool
	}{
		{"restart", func(*safetyControlReceipt) {}, true},
		{"restart-noop", func(r *safetyControlReceipt) { r.AfterStartedAt = r.BeforeStartedAt }, false},
		{"restart-new-container", func(r *safetyControlReceipt) { r.AfterContainerID = "c2" }, false},
		{"recreate", func(r *safetyControlReceipt) { r.Action = "service-recreate"; r.AfterContainerID = "c2" }, true},
		{"recreate-noop", func(r *safetyControlReceipt) { r.Action = "service-recreate" }, false},
		{"restore", func(r *safetyControlReceipt) {
			r.Action = "backup-restore"
			r.TargetVolumeID = "v2"
			r.AfterContainerID = "c2"
		}, true},
		{"restore-shared-volume", func(r *safetyControlReceipt) { r.Action = "backup-restore"; r.AfterContainerID = "c2" }, false},
		{"restore-image-mismatch", func(r *safetyControlReceipt) {
			r.Action = "backup-restore"
			r.TargetVolumeID = "v2"
			r.TargetImageID = "other"
		}, false},
		{"wrong-project", func(r *safetyControlReceipt) { r.Project = "other" }, false},
		{"not-an-image-digest", func(r *safetyControlReceipt) { r.SourceImageID = "latest"; r.TargetImageID = "latest" }, false},
		{"snapshot-noop", func(r *safetyControlReceipt) {
			r.Action = "snapshot-store"
			r.Instance = "main"
			r.AfterStartedAt = r.BeforeStartedAt
		}, false},
		{"snapshot-restarted", func(r *safetyControlReceipt) { r.Action = "snapshot-store"; r.Instance = "main" }, true},
		{"snapshot-earlier", func(r *safetyControlReceipt) {
			r.Action = "snapshot-store"
			r.Instance = "main"
			r.AfterStartedAt = "2025-12-31T23:59:59Z"
		}, false},
		{"snapshot-changed-container", func(r *safetyControlReceipt) {
			r.Action = "snapshot-store"
			r.Instance = "main"
			r.AfterContainerID = "c2"
		}, false},
		{"snapshot-changed-volume", func(r *safetyControlReceipt) {
			r.Action = "snapshot-store"
			r.Instance = "main"
			r.TargetVolumeID = "v2"
		}, false},
		{"snapshot-changed-image", func(r *safetyControlReceipt) {
			r.Action = "snapshot-store"
			r.Instance = "main"
			r.TargetImageID = "sha256:" + strings.Repeat("b", 64)
		}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := base
			tc.change(&r)
			if got := safetyReceiptValid(r, "owned"); got != tc.valid {
				t.Fatalf("valid = %v; want %v", got, tc.valid)
			}
		})
	}
}

func TestSafetyValidRevisionName(t *testing.T) {
	for _, tc := range []struct {
		name  string
		valid bool
	}{
		{"legacy-abc123", true}, {"legacy-a_b", false}, {"legacy-UPPER", false},
		{"-invalid", false}, {"invalid-", false}, {strings.Repeat("a", 64), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := safetyValidRevisionName(tc.name); got != tc.valid {
				t.Fatalf("valid = %v, want %v", got, tc.valid)
			}
		})
	}
}
