// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/openchami/integration-sandbox/tests/internal/quiescence"
)

func (c controller) verifyLease(ctx context.Context, lease recoveryLease) (container, error) {
	v, err := c.inspect(ctx, lease.Service, lease.ContainerID)
	if err != nil || v.ID != lease.ContainerID || v.Image != lease.ImageID {
		return container{}, errRecovery
	}
	dir, err := c.dataDir(v)
	if err != nil || dir != lease.DataDir {
		return container{}, errRecovery
	}
	return v, nil
}

func (c controller) recoverWatch(path string, input io.Reader, output io.Writer) error {
	signal.Ignore(syscall.SIGPIPE)
	lease, err := c.readLease(path)
	if err != nil || os.Getppid() != lease.OwnerPID || time.Until(lease.Expires) <= 0 || time.Until(lease.Expires) > 4*time.Minute {
		return errRecovery
	}
	ctx, cancel := context.WithDeadline(context.Background(), lease.Expires)
	defer cancel()
	if err := c.registerAsset(asset{Kind: "process", Name: filepath.Base(path), Project: c.project, Service: lease.Service, PID: os.Getpid()}); err != nil {
		return err
	}
	reader := bufio.NewReader(input)
	first := make(chan string, 1)
	go func() {
		line, err := reader.ReadString('\n')
		if err != nil {
			cancel()
		}
		first <- line
	}()
	lock, err := c.lockRecovery(ctx, lease.Service)
	if err != nil {
		return err
	}
	defer lock.Close()
	v, err := c.verifyLease(ctx, lease)
	if err != nil || !v.State.Running || v.State.Paused || v.State.StartedAt != lease.StartedAt {
		return errRecovery
	}
	if _, err := fmt.Fprintln(output, "ready"); err != nil {
		return errRecovery
	}
	var command string
	select {
	case command = <-first:
	case <-ctx.Done():
		return errRecovery
	}
	if command != "stop\n" || os.Getppid() != lease.OwnerPID {
		return errRecovery
	}
	stopCtx, stopCancel := context.WithTimeout(context.Background(), 55*time.Second)
	_, stopErr := c.exec.run(stopCtx, "stop", "--time", "40", lease.ContainerID)
	stopCancel()
	verifyCtx, verifyCancel := context.WithTimeout(context.Background(), 15*time.Second)
	stopped, inspectErr := c.verifyLease(verifyCtx, lease)
	verifyCancel()
	clean := stopErr == nil && inspectErr == nil && cleanStop(stopped)
	if !clean {
		if err := c.awaitStopped(ctx, lease); err != nil {
			return errors.Join(errRecovery, err)
		}
	}
	message := "stopped"
	if !clean {
		message = "stop-error"
	}
	if _, err := fmt.Fprintln(output, message); err != nil {
		return c.restoreOrphan(path, lease)
	}
	if watchOwner(ctx, reader, lease.OwnerPID) != "handoff\n" {
		return c.restoreOrphan(path, lease)
	}
	if _, err := fmt.Fprintln(output, "handoff"); err != nil {
		return c.restoreOrphan(path, lease)
	}
	if watchOwner(ctx, reader, lease.OwnerPID) != "done\n" {
		return c.restoreOrphan(path, lease)
	}
	if err := markLeaseFinished(c.runDir, path, lease); err != nil {
		return err
	}
	_, _ = fmt.Fprintln(output, "done")
	return nil
}

func watchOwner(ctx context.Context, reader *bufio.Reader, ownerPID int) string {
	finished := make(chan string, 1)
	go func() { line, _ := reader.ReadString('\n'); finished <- line }()
	select {
	case line := <-finished:
		return line
	case <-ctx.Done():
		if os.Getppid() == ownerPID {
			if err := syscall.Kill(ownerPID, syscall.SIGKILL); err != nil && err != syscall.ESRCH {
				return ""
			}
		}
		return <-finished
	}
}

func (c controller) restoreOrphan(path string, lease recoveryLease) (result error) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	lockPath, err := c.capturePath()
	if err != nil {
		return err
	}
	barrier, err := quiescence.AcquireRecovery(ctx, lockPath)
	if err != nil {
		return err
	}
	defer func() { result = errors.Join(result, barrier.Close()) }()
	v, err := c.verifyLease(ctx, lease)
	if err != nil {
		return err
	}
	if v.State.Running {
		before, e1 := time.Parse(time.RFC3339Nano, lease.StartedAt)
		after, e2 := time.Parse(time.RFC3339Nano, v.State.StartedAt)
		if e1 != nil || e2 != nil || !after.After(before) || v.State.Paused {
			return errRecovery
		}
		if err := waitReady(ctx, lease.Service); err != nil {
			return err
		}
	} else {
		if err := c.prepareIdentityIfRequired(ctx, v); err != nil {
			return err
		}
		if err := c.restoreLease(lease); err != nil {
			return err
		}
	}
	return markLeaseFinished(c.runDir, path, lease)
}

func (c controller) awaitStopped(ctx context.Context, lease recoveryLease) error {
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		v, err := c.verifyLease(ctx, lease)
		if err == nil && !v.State.Running {
			return nil
		}
		select {
		case <-ctx.Done():
			return errRecovery
		case <-ticker.C:
		}
	}
}

func (c controller) restoreLease(lease recoveryLease) error {
	ctx, cancel := context.WithTimeout(context.Background(), 65*time.Second)
	defer cancel()
	v, err := c.verifyLease(ctx, lease)
	if err != nil {
		return err
	}
	if v.State.Running || v.State.Paused {
		return errRecovery
	}
	if _, err := c.exec.run(ctx, "start", lease.ContainerID); err != nil {
		return errRecovery
	}
	v, err = c.verifyLease(ctx, lease)
	if err != nil || !v.State.Running || v.State.Paused {
		return errRecovery
	}
	before, beforeErr := time.Parse(time.RFC3339Nano, lease.StartedAt)
	after, afterErr := time.Parse(time.RFC3339Nano, v.State.StartedAt)
	if beforeErr != nil || afterErr != nil || !after.After(before) {
		return errRecovery
	}
	return waitReady(ctx, lease.Service)
}
