// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
)

type ports struct {
	Boot       int `json:"boot"`
	AuthOff    int `json:"authOff"`
	SMD        int `json:"smd"`
	TokenSmith int `json:"tokensmith"`
}

func allocatePorts() (result ports, resultErr error) {
	listeners := make([]net.Listener, 0, 4)
	defer func() {
		for _, listener := range listeners {
			resultErr = errors.Join(resultErr, listener.Close())
		}
	}()
	var selected [4]int
	for i := range selected {
		listener, err := net.Listen("tcp4", "127.0.0.1:0")
		if err != nil {
			return ports{}, fmt.Errorf("allocate loopback port: %w", err)
		}
		listeners = append(listeners, listener)
		address, ok := listener.Addr().(*net.TCPAddr)
		if !ok || address.Port < 1 || address.Port > 65535 {
			return ports{}, errors.New("invalid loopback address")
		}
		selected[i] = address.Port
	}
	return ports{Boot: selected[0], AuthOff: selected[1], SMD: selected[2], TokenSmith: selected[3]}, nil
}

func main() {
	selected, err := allocatePorts()
	if err == nil {
		err = json.NewEncoder(os.Stdout).Encode(selected)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
