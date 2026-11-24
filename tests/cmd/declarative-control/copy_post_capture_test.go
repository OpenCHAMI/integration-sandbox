// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/openchami/integration-sandbox/tests/internal/quiescence"
)

func TestBackupBlocksWriterUntilCopyReceipt(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	attempted := make(chan struct{})
	finished := make(chan error, 1)
	probe := func(lock, receipts string, _ context.CancelFunc) error {
		go func() {
			close(attempted)
			lease, err := quiescence.BeginWrite(ctx, lock, "http")
			if err != nil {
				finished <- err
				return
			}
			data, readErr := os.ReadFile(receipts)
			var observed receipt
			if readErr == nil && (json.Unmarshal(data, &observed) != nil || observed.Action != "backup-restore" || !observed.Success || observed.AfterContainerID != "copy-id") {
				readErr = errors.New("writer acquired before backup receipt")
			}
			finished <- errors.Join(readErr, lease.Complete())
		}()
		<-attempted
		select {
		case err := <-finished:
			return fmt.Errorf("writer completed during copy startup: %v", err)
		default:
			return nil
		}
	}
	run, _, err := backupProcessWithProbe(t, false, probe)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-finished:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("writer did not finish after receipt")
	}
	assertCaptureFinished(t, run, filepath.Join(run, "writers.lock"))
}

func TestBackupStartupFailureDoesNotWriteSuccessReceipt(t *testing.T) {
	run, _, err := backupProcessWithProbe(t, false, func(_, _ string, _ context.CancelFunc) error {
		return errors.New("injected copy startup failure")
	})
	if err == nil {
		t.Fatal("failed copy startup returned success")
	}
	data, err := os.ReadFile(filepath.Join(run, "receipts.jsonl"))
	if err != nil || len(data) != 0 {
		t.Fatal("failed copy startup wrote a success receipt")
	}
	assertCaptureFinished(t, run, filepath.Join(run, "writers.lock"))
}

func TestBackupCanceledDuringCopyStartupDoesNotWriteSuccessReceipt(t *testing.T) {
	run, _, err := backupProcessWithProbe(t, false, func(_, _ string, cancel context.CancelFunc) error {
		cancel()
		return nil
	})
	if err == nil {
		t.Fatal("canceled backup returned success")
	}
	data, err := os.ReadFile(filepath.Join(run, "receipts.jsonl"))
	if err != nil || len(data) != 0 {
		t.Fatal("canceled backup wrote a success receipt")
	}
	assertCaptureFinished(t, run, filepath.Join(run, "writers.lock"))
}
