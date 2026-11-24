//go:build darwin || linux

// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package quiescence

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"testing"
	"time"
)

func TestWriterProcessHelper(t *testing.T) {
	path := os.Getenv("QUIESCENCE_TEST_LOCK")
	if path == "" {
		return
	}
	lease, err := BeginWrite(context.Background(), path, "process")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fmt.Fprintln(os.Stdout, "ready"); err != nil {
		t.Fatal(err)
	}
	_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
	if err := lease.Complete(); err != nil {
		t.Fatal(err)
	}
}

func TestDescriptorChildHelper(t *testing.T) {
	if os.Getenv("QUIESCENCE_DESCRIPTOR_TEST") == "" {
		return
	}
	if _, err := fmt.Fprintln(os.Stdout, "ready"); err != nil {
		t.Fatal(err)
	}
	_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
}

func TestLockDescriptorNotInherited(t *testing.T) {
	path := lockFixture(t)
	writer, err := BeginWrite(context.Background(), path, "cli")
	if err != nil {
		t.Fatal(err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(exe, "-test.run=^TestDescriptorChildHelper$")
	cmd.Env = append(os.Environ(), "QUIESCENCE_DESCRIPTOR_TEST=1")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		stdin.Close()
		if cmd.ProcessState == nil {
			cmd.Process.Kill()
			cmd.Wait()
		}
	})
	line, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil || line != "ready\n" {
		t.Fatalf("child not ready: %q %v", line, err)
	}
	if err := writer.Abandon(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	recovery, err := AcquireRecovery(ctx, path)
	if err != nil {
		t.Fatalf("child inherited lock: %v", err)
	}
	if err := recovery.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := fmt.Fprintln(stdin, "done"); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}
}

func TestIndependentProcessSharedWriters(t *testing.T) {
	path := lockFixture(t)
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(exe, "-test.run=^TestWriterProcessHelper$")
	cmd.Env = append(os.Environ(), "QUIESCENCE_TEST_LOCK="+path)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		stdin.Close()
		if cmd.ProcessState == nil {
			cmd.Process.Kill()
			cmd.Wait()
		}
	})
	line, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil || line != "ready\n" {
		t.Fatalf("child not ready: %q %v", line, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	writer, err := BeginWrite(ctx, path, "http")
	if err != nil {
		t.Fatalf("shared writer serialized: %v", err)
	}
	if err := writer.Complete(); err != nil {
		t.Fatal(err)
	}
	blocked, stop := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer stop()
	if capture, err := AcquireCapture(blocked, path); !errors.Is(err, context.DeadlineExceeded) {
		if capture != nil {
			capture.Close()
		}
		t.Fatalf("other process was unlocked: %v", err)
	}
	if _, err := fmt.Fprintln(stdin, "done"); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}
	capture, err := AcquireCapture(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if err := capture.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestKilledWriterLeavesStickyMarker(t *testing.T) {
	path := lockFixture(t)
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(exe, "-test.run=^TestWriterProcessHelper$")
	cmd.Env = append(os.Environ(), "QUIESCENCE_TEST_LOCK="+path)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		stdin.Close()
		if cmd.ProcessState == nil {
			cmd.Process.Kill()
			cmd.Wait()
		}
	})
	line, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil || line != "ready\n" {
		t.Fatalf("child not ready: %q %v", line, err)
	}
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err == nil {
		t.Fatal("killed child exited successfully")
	}
	if capture, err := AcquireCapture(context.Background(), path); err == nil {
		capture.Close()
		t.Fatal("killed writer allowed capture")
	}
}
