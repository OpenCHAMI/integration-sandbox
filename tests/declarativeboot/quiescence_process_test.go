//go:build integration && declarative_boot

// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package declarativeboot

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/openchami/integration-sandbox/tests/internal/quiescence"
)

func TestBarrierBindingProcessHelper(t *testing.T) {
	switch os.Getenv("QUIESCENCE_BINDING_PROCESS") {
	case "http":
		client, err := NewHTTPClient(os.Getenv("SBX_DECLARATIVE_BOOT_URL"), 30*time.Second)
		if err != nil {
			t.Fatal(err)
		}
		_, err = client.Do(context.Background(), Request{Method: http.MethodPost, Path: "/"})
		if err != nil {
			t.Fatal(err)
		}
	case "wrapper":
		exe, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		child := exec.Command(exe, "-test.run=^TestBarrierBindingProcessHelper$")
		child.Env = append(os.Environ(), "QUIESCENCE_BINDING_PROCESS=child")
		child.Stdin = os.Stdin
		child.ExtraFiles = []*os.File{os.NewFile(3, "notify")}
		_, err = runAdoptLeased(context.Background(), os.Getenv("SBX_DECLARATIVE_QUIESCENCE_LOCK"), child)
		if err != nil {
			t.Fatal(err)
		}
	case "child":
		notify := os.NewFile(3, "notify")
		if _, err := fmt.Fprintln(notify, os.Getpid()); err != nil {
			os.Exit(2)
		}
		_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
		os.Exit(0)
	}
}

func TestHTTPQuiescenceKilledBindingLeavesMarker(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.(http.Flusher).Flush()
		close(entered)
		<-release
	}))
	defer server.Close()
	defer close(release)
	path := quiescenceTestLock(t)
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe, "-test.run=^TestBarrierBindingProcessHelper$")
	cmd.Env = append(os.Environ(), "QUIESCENCE_BINDING_PROCESS=http", "SBX_DECLARATIVE_BOOT_URL="+server.URL, "SBX_DECLARATIVE_QUIESCENCE_LOCK="+path)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if cmd.ProcessState == nil {
			cmd.Process.Kill()
			cmd.Wait()
		}
	})
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("HTTP client did not dispatch")
	}
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err == nil {
		t.Fatal("killed client exited successfully")
	}
	recovery, err := quiescence.AcquireRecovery(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if err := recovery.Close(); err != nil {
		t.Fatal(err)
	}
	if capture, err := quiescence.AcquireCapture(ctx, path); !errors.Is(err, quiescence.ErrInFlight) {
		if capture != nil {
			capture.Close()
		}
		t.Fatalf("killed HTTP client did not retain marker: %v", err)
	}
}

func TestCLIQuiescenceKilledWrapperLeavesMarker(t *testing.T) {
	path := quiescenceTestLock(t)
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe, "-test.run=^TestBarrierBindingProcessHelper$")
	cmd.Env = append(os.Environ(), "QUIESCENCE_BINDING_PROCESS=wrapper", "SBX_DECLARATIVE_QUIESCENCE_LOCK="+path)
	cmd.ExtraFiles = []*os.File{writer}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	defer stdin.Close()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	writer.Close()
	t.Cleanup(func() {
		if cmd.ProcessState == nil {
			cmd.Process.Kill()
			cmd.Wait()
		}
	})
	lines := bufio.NewReader(reader)
	ready := make(chan string, 1)
	go func() { line, _ := lines.ReadString('\n'); ready <- line }()
	var line string
	select {
	case line = <-ready:
	case <-ctx.Done():
		t.Fatal("CLI child did not start")
	}
	pid, err := strconv.Atoi(strings.TrimSpace(line))
	if err != nil || pid <= 0 {
		t.Fatalf("invalid child pid %q", line)
	}
	child, err := os.FindProcess(pid)
	if err != nil {
		t.Fatal(err)
	}
	childKilled := false
	t.Cleanup(func() {
		if !childKilled {
			child.Kill()
		}
	})
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err == nil {
		t.Fatal("killed wrapper exited successfully")
	}
	if err := syscall.Kill(pid, 0); err != nil {
		t.Fatalf("child not alive after wrapper death: %v", err)
	}
	recovery, err := quiescence.AcquireRecovery(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if err := recovery.Close(); err != nil {
		t.Fatal(err)
	}
	if capture, err := quiescence.AcquireCapture(ctx, path); !errors.Is(err, quiescence.ErrInFlight) {
		if capture != nil {
			capture.Close()
		}
		t.Fatalf("killed wrapper did not retain marker: %v", err)
	}
	if err := child.Kill(); err != nil {
		t.Fatal(err)
	}
	childKilled = true
	closed := make(chan error, 1)
	go func() { _, err := lines.ReadString('\n'); closed <- err }()
	select {
	case <-closed:
	case <-ctx.Done():
		t.Fatal("CLI child did not close its process pipe")
	}
}
