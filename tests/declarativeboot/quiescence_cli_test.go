//go:build integration && declarative_boot

// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package declarativeboot

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/openchami/integration-sandbox/tests/internal/quiescence"
)

func TestAdoptionReceiptProcess(t *testing.T) {
	mode := os.Getenv("QUIESCENCE_CLI_TEST")
	if mode == "" {
		return
	}
	switch mode {
	case "complete":
		fmt.Fprintln(os.Stdout, `{"operation":"boot-adopt","phase":"complete","requests":{"started":1,"completed":1,"ambiguous":false}}`)
		os.Exit(0)
	case "response":
		fmt.Fprintln(os.Stdout, `{"operation":"boot-adopt","phase":"response","requests":{"started":1,"completed":1,"ambiguous":false}}`)
		os.Exit(4)
	case "response-zero":
		fmt.Fprintln(os.Stdout, `{"operation":"boot-adopt","phase":"response","requests":{"started":0,"completed":0,"ambiguous":false}}`)
		os.Exit(4)
	case "preflight-zero":
		fmt.Fprintln(os.Stdout, `{"operation":"boot-adopt","phase":"preflight","requests":{"started":0,"completed":0,"ambiguous":false}}`)
		os.Exit(4)
	case "preflight-reads":
		fmt.Fprintln(os.Stdout, `{"operation":"boot-adopt","phase":"preflight","requests":{"started":1,"completed":1,"ambiguous":false}}`)
		os.Exit(4)
	case "unknown-phase":
		fmt.Fprintln(os.Stdout, `{"operation":"boot-adopt","phase":"error","requests":{"started":1,"completed":1,"ambiguous":false}}`)
		os.Exit(4)
	case "complete-error":
		fmt.Fprintln(os.Stdout, `{"operation":"boot-adopt","phase":"complete","requests":{"started":1,"completed":1,"ambiguous":false}}`)
		os.Exit(4)
	case "stderr":
		fmt.Fprintln(os.Stderr, `{"operation":"boot-adopt","phase":"complete","requests":{"started":1,"completed":1,"ambiguous":false}}`)
		os.Exit(0)
	case "transport":
		fmt.Fprintln(os.Stdout, `{"operation":"boot-adopt","phase":"transport","requests":{"started":1,"completed":0,"ambiguous":true}}`)
		os.Exit(4)
	case "wait":
		notify := os.NewFile(3, "notify")
		fmt.Fprintln(notify, "ready")
		_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
		fmt.Fprintln(os.Stdout, `{"operation":"boot-adopt","phase":"complete","requests":{"started":1,"completed":1,"ambiguous":false}}`)
		os.Exit(0)
	}
}

func TestCLIQuiescenceWaitsForProcess(t *testing.T) {
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
	defer writer.Close()
	inRead, inWrite, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer inRead.Close()
	defer inWrite.Close()
	cmd := exec.Command(exe, "-test.run=^TestAdoptionReceiptProcess$")
	cmd.Env = append(os.Environ(), "QUIESCENCE_CLI_TEST=wait")
	cmd.ExtraFiles = []*os.File{writer}
	cmd.Stdin = inRead
	result := make(chan error, 1)
	go func() { _, err := runAdoptLeased(context.Background(), path, cmd); result <- err }()
	ready := make(chan string, 1)
	go func() { line, _ := bufio.NewReader(reader).ReadString('\n'); ready <- line }()
	select {
	case line := <-ready:
		if line != "ready\n" {
			t.Fatalf("wrong ready signal %q", line)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("child did not start")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	if capture, err := quiescence.AcquireCapture(ctx, path); !errors.Is(err, context.DeadlineExceeded) {
		if capture != nil {
			capture.Close()
		}
		fmt.Fprintln(inWrite)
		t.Fatalf("capture did not wait: %v", err)
	}
	if _, err := fmt.Fprintln(inWrite); err != nil {
		t.Fatal(err)
	}
	if err := <-result; err != nil {
		t.Fatal(err)
	}
	capture, err := quiescence.AcquireCapture(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if err := capture.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestCLIQuiescenceCanceledProcessLeavesMarker(t *testing.T) {
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
	defer writer.Close()
	inRead, inWrite, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer inRead.Close()
	defer inWrite.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cmd := exec.CommandContext(ctx, exe, "-test.run=^TestAdoptionReceiptProcess$")
	cmd.Env = append(os.Environ(), "QUIESCENCE_CLI_TEST=wait")
	cmd.ExtraFiles = []*os.File{writer}
	cmd.Stdin = inRead
	result := make(chan error, 1)
	go func() { _, err := runAdoptLeased(ctx, path, cmd); result <- err }()
	ready := make(chan string, 1)
	go func() { line, _ := bufio.NewReader(reader).ReadString('\n'); ready <- line }()
	select {
	case line := <-ready:
		if line != "ready\n" {
			t.Fatalf("wrong ready signal %q", line)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("child did not start")
	}
	cancel()
	if err := <-result; err == nil {
		t.Fatal("canceled process succeeded")
	}
	if capture, err := quiescence.AcquireCapture(context.Background(), path); !errors.Is(err, quiescence.ErrInFlight) {
		if capture != nil {
			capture.Close()
		}
		t.Fatalf("canceled command did not leave marker: %v", err)
	}
}

func TestCLIQuiescenceRejectsIncompleteReceipts(t *testing.T) {
	for _, body := range []string{
		`{`, `{"operation":"boot-adopt","phase":"complete","requests":{"started":0,"completed":0}}`,
		`{"operation":"boot-adopt","phase":"complete","requests":{"started":2,"completed":1,"ambiguous":false}}`,
		`{"operation":"boot-adopt","phase":"preflight","requests":{"started":0,"completed":0,"ambiguous":false}}`,
		`{"operation":"other","phase":"complete","requests":{"started":0,"completed":0,"ambiguous":false}}`,
	} {
		if receiptCertifies(context.Background(), []byte(body), nil) {
			t.Fatalf("invalid receipt certified: %s", body)
		}
	}
}

func TestCLIQuiescenceReceiptClassification(t *testing.T) {
	for _, tc := range []struct {
		name      string
		eligible  bool
		exitError bool
	}{{"complete", true, false}, {"response", true, true}, {"response-zero", false, true}, {"preflight-zero", true, true}, {"preflight-reads", true, true}, {"unknown-phase", false, true}, {"complete-error", false, true}, {"stderr", false, false}, {"transport", false, true}} {
		t.Run(tc.name, func(t *testing.T) {
			path := quiescenceTestLock(t)
			exe, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command(exe, "-test.run=^TestAdoptionReceiptProcess$")
			cmd.Env = append(os.Environ(), "QUIESCENCE_CLI_TEST="+tc.name)
			output, runErr := runAdoptLeased(context.Background(), path, cmd)
			if (runErr != nil) != tc.exitError {
				t.Fatalf("unexpected command result: %v", runErr)
			}
			if tc.name == "stderr" && !bytes.Contains(output, []byte(`"operation":"boot-adopt"`)) {
				t.Fatal("combined output lost stderr")
			}
			capture, err := quiescence.AcquireCapture(context.Background(), path)
			if tc.eligible {
				if err != nil {
					t.Fatal(err)
				}
				if err := capture.Close(); err != nil {
					t.Fatal(err)
				}
			} else if !errors.Is(err, quiescence.ErrInFlight) {
				if capture != nil {
					capture.Close()
				}
				t.Fatalf("expected sticky marker: %v", err)
			}
		})
	}
}
