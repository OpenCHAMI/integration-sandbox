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
	"os/exec"
	"syscall"
	"time"
)

func (c controller) withStopped(ctx context.Context, before container, work func(string) error) error {
	return c.withStoppedAfter(ctx, before, work, nil)
}

func (c controller) withStoppedAfter(ctx context.Context, before container, work func(string) error, after func() error) error {
	return c.withCapture(ctx, func() error {
		var marker string
		err := c.stoppedWithCapture(ctx, before, func(dir string) error {
			if captureMayMutate(ctx) {
				var err error
				marker, err = c.createOfflineMarker()
				if err != nil {
					return err
				}
			}
			return work(dir)
		})
		if err != nil {
			return err
		}
		if marker != "" {
			if err := c.clearOfflineMarker(marker); err != nil {
				return err
			}
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if after != nil {
			if err := after(); err != nil {
				return err
			}
		}
		return ctx.Err()
	})
}

func (c controller) stoppedWithCapture(ctx context.Context, before container, work func(string) error) (result error) {
	service := before.Config.Labels["com.docker.compose.service"]
	current, err := c.inspect(ctx, service, before.ID)
	if err != nil || current.ID != before.ID || current.Image != before.Image || current.State.StartedAt != before.State.StartedAt {
		return errRecovery
	}
	dir, err := c.dataDir(current)
	if err != nil {
		return err
	}
	if err := c.prepareIdentityIfRequired(ctx, current); err != nil {
		return err
	}
	leasePath, err := c.newLease(current)
	if err != nil {
		return err
	}
	executable, err := os.Executable()
	if err != nil {
		return errRecovery
	}
	cmd := exec.Command(executable, "recover-watch", leasePath)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	input, err := cmd.StdinPipe()
	if err != nil {
		return errRecovery
	}
	output, err := cmd.StdoutPipe()
	if err != nil {
		input.Close()
		return errRecovery
	}
	if err := cmd.Start(); err != nil {
		input.Close()
		output.Close()
		return errRecovery
	}
	events := watchEvents(output)
	var stopSent, stoppedSeen bool
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 110*time.Second)
		defer cancel()
		if stopSent {
			if !stoppedSeen {
				message, err := nextWatchEvent(cleanup, events)
				if err == nil && (message == "stopped" || message == "stop-error") {
					stoppedSeen = true
				} else {
					result = errors.Join(result, errRecovery)
				}
			}
			if stoppedSeen {
				result = errors.Join(result, c.restoreWithCapture(cleanup, leasePath, input, events))
			}
		}
		input.Close()
		waited := make(chan error, 1)
		go func() { waited <- cmd.Wait() }()
		select {
		case err := <-waited:
			result = errors.Join(result, err)
		case <-cleanup.Done():
			result = errors.Join(result, errRecovery)
		}
	}()
	if message, err := nextWatchEvent(ctx, events); err != nil || message != "ready" {
		return errRecovery
	}
	stopSent = true
	if _, err := fmt.Fprintln(input, "stop"); err != nil {
		return errRecovery
	}
	message, err := nextWatchEvent(ctx, events)
	if err != nil {
		return err
	}
	stoppedSeen = message == "stopped" || message == "stop-error"
	if message != "stopped" {
		return errRecovery
	}
	return work(dir)
}

func watchEvents(input io.Reader) <-chan string {
	events := make(chan string, 8)
	go func() {
		defer close(events)
		scanner := bufio.NewScanner(input)
		for scanner.Scan() {
			events <- scanner.Text()
		}
	}()
	return events
}

func nextWatchEvent(ctx context.Context, events <-chan string) (string, error) {
	select {
	case value, ok := <-events:
		if !ok {
			return "", errRecovery
		}
		return value, nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func (c controller) restoreWithCapture(ctx context.Context, path string, input io.Writer, events <-chan string) error {
	if _, err := fmt.Fprintln(input, "handoff"); err != nil {
		return errRecovery
	}
	message, err := nextWatchEvent(ctx, events)
	if err != nil || message != "handoff" {
		return errRecovery
	}
	lease, err := c.readLease(path)
	if err != nil {
		return err
	}
	if err := c.restoreLease(lease); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(input, "done"); err != nil {
		return errRecovery
	}
	message, err = nextWatchEvent(ctx, events)
	if err != nil || message != "done" {
		return errRecovery
	}
	return nil
}
