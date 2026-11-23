//go:build integration && declarative_boot

// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package declarativeboot

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os/exec"
	"sync"

	"github.com/openchami/integration-sandbox/tests/internal/quiescence"
)

type adoptionReceipt struct {
	Operation string `json:"operation"`
	Phase     string `json:"phase"`
	Requests  *struct {
		Started   *uint64 `json:"started"`
		Completed *uint64 `json:"completed"`
		Ambiguous *bool   `json:"ambiguous"`
	} `json:"requests"`
}
type synchronizedOutput struct {
	mu     sync.Mutex
	buffer bytes.Buffer
}

func (b *synchronizedOutput) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buffer.Write(p)
}

func receiptCertifies(ctx context.Context, stdout []byte, runErr error) bool {
	var receipt adoptionReceipt
	if ctx.Err() != nil || json.Unmarshal(stdout, &receipt) != nil || receipt.Operation != "boot-adopt" || receipt.Requests == nil || receipt.Requests.Started == nil || receipt.Requests.Completed == nil || receipt.Requests.Ambiguous == nil || *receipt.Requests.Ambiguous || *receipt.Requests.Started != *receipt.Requests.Completed {
		return false
	}
	if runErr == nil {
		return receipt.Phase == "complete"
	}
	var exit *exec.ExitError
	if !errors.As(runErr, &exit) || exit.ExitCode() < 0 {
		return false
	}
	return receipt.Phase == "preflight" || (receipt.Phase == "response" && *receipt.Requests.Started > 0)
}

func runAdoptLeased(ctx context.Context, path string, cmd *exec.Cmd) ([]byte, error) {
	lease, err := quiescence.BeginWrite(ctx, path, "cli")
	if err != nil {
		return nil, err
	}
	var stdout bytes.Buffer
	var combined synchronizedOutput
	cmd.Stdout = io.MultiWriter(&stdout, &combined)
	cmd.Stderr = &combined
	if ctx.Err() != nil {
		return nil, errors.Join(ctx.Err(), lease.Complete())
	}
	if err := cmd.Start(); err != nil {
		return combined.buffer.Bytes(), errors.Join(err, lease.Complete())
	}
	runErr := cmd.Wait()
	var leaseErr error
	if receiptCertifies(ctx, stdout.Bytes(), runErr) {
		leaseErr = lease.Complete()
	} else {
		leaseErr = lease.Abandon()
	}
	if leaseErr != nil {
		return combined.buffer.Bytes(), errors.Join(runErr, leaseErr)
	}
	return combined.buffer.Bytes(), runErr
}
