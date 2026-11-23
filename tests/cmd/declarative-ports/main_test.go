// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"fmt"
	"net"
	"os/exec"
	"testing"
)

func TestAllocatePortsReturnsDistinctReleasedLoopbackPorts(t *testing.T) {
	ports, err := allocatePorts()
	if err != nil {
		t.Fatal(err)
	}
	seen := make(map[int]bool)
	for _, port := range []int{ports.Boot, ports.AuthOff, ports.SMD, ports.TokenSmith} {
		if port < 1 || port > 65535 || seen[port] {
			t.Fatalf("invalid or duplicate selected port: %d", port)
		}
		seen[port] = true
		listener, err := net.Listen("tcp4", fmt.Sprintf("127.0.0.1:%d", port))
		if err != nil {
			t.Fatalf("selected listener was not released: %v", err)
		}
		t.Cleanup(func() {
			if err := listener.Close(); err != nil {
				t.Errorf("close selected listener: %v", err)
			}
		})
	}
}

func TestCommandEmitsSelectedPortsAsJSON(t *testing.T) {
	output, err := exec.Command("go", "run", ".").Output()
	if err != nil {
		t.Fatal(err)
	}
	var selected ports
	if err := json.Unmarshal(output, &selected); err != nil {
		t.Fatal(err)
	}
	if selected.Boot == 0 || selected.AuthOff == 0 || selected.SMD == 0 || selected.TokenSmith == 0 {
		t.Fatal("command returned an empty port")
	}
}
