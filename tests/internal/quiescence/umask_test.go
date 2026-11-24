//go:build darwin || linux

// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package quiescence

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
)

func TestRestrictedUmaskChild(t *testing.T) {
	path := os.Getenv("QUIESCENCE_UMASK_LOCK")
	if path == "" {
		return
	}
	syscall.Umask(0600)
	lease, err := BeginWrite(context.Background(), path, "http")
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

func TestRestrictedUmaskPermissions(t *testing.T) {
	for _, existing := range []bool{true, false} {
		t.Run(fmt.Sprintf("existing=%v", existing), func(t *testing.T) {
			path := lockFixture(t)
			if existing {
				if err := os.Mkdir(path+".inflight", 0700); err != nil {
					t.Fatal(err)
				}
			}
			exe, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command(exe, "-test.run=^TestRestrictedUmaskChild$")
			cmd.Env = append(os.Environ(), "QUIESCENCE_UMASK_LOCK="+path)
			stdin, err := cmd.StdinPipe()
			if err != nil {
				t.Fatal(err)
			}
			stdout, err := cmd.StdoutPipe()
			if err != nil {
				t.Fatal(err)
			}
			cmd.Stderr = os.Stderr
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
				t.Fatalf("child failed: %q %v", line, err)
			}
			info, err := os.Stat(path + ".inflight")
			if err != nil || info.Mode().Perm() != 0700 {
				t.Fatalf("directory mode: %v %v", info, err)
			}
			entries, err := os.ReadDir(path + ".inflight")
			if err != nil || len(entries) != 1 {
				t.Fatalf("markers: %v %v", entries, err)
			}
			info, err = os.Stat(filepath.Join(path+".inflight", entries[0].Name()))
			if err != nil || info.Mode().Perm() != 0600 {
				t.Fatalf("marker mode: %v %v", info, err)
			}
			if _, err := fmt.Fprintln(stdin, "done"); err != nil {
				t.Fatal(err)
			}
			if err := cmd.Wait(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
