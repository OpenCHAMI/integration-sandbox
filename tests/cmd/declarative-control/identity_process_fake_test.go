// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"sync"
	"syscall"
	"testing"
)

type initialIssuer struct {
	mu    sync.Mutex
	calls int
	fail  bool
}

func (f *initialIssuer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var args []string
	if json.NewDecoder(r.Body).Decode(&args) != nil || len(args) == 0 {
		http.Error(w, "invalid", 400)
		return
	}
	switch args[0] {
	case "compose":
		if args[len(args)-1] != "tokensmith-backend" {
			http.Error(w, "unexpected service", 400)
			return
		}
		fmt.Fprintln(w, "ts-owned")
	case "inspect":
		if args[len(args)-1] != "ts-owned" {
			http.Error(w, "unexpected container", 400)
			return
		}
		v := container{ID: "ts-owned", Config: containerConfig{Labels: map[string]string{"com.docker.compose.project": "sandbox", "com.docker.compose.service": "tokensmith-backend", "openchami.acceptance.run": "sandbox"}}}
		v.State.Running = true
		json.NewEncoder(w).Encode([]container{v})
	case "exec":
		f.mu.Lock()
		defer f.mu.Unlock()
		if f.fail {
			http.Error(w, "issuer failure", 500)
			return
		}
		f.calls++
		token := "process-secret-one"
		if f.calls == 2 {
			token = "process-secret-two"
		}
		json.NewEncoder(w).Encode(struct {
			Token string `json:"bootstrap_token"`
		}{token})
	default:
		http.Error(w, "unexpected command", 400)
	}
}

func TestIdentityUmaskProcess(t *testing.T) {
	control := os.Getenv("IDENTITY_UMASK_CONTROL")
	if control == "" {
		return
	}
	mode, err := strconv.ParseUint(os.Getenv("IDENTITY_UMASK"), 8, 16)
	if err != nil {
		t.Fatal("bad test mode")
	}
	syscall.Umask(int(mode))
	if err := syscall.Exec(control, []string{control, "prepare-boot-identity", "{}"}, os.Environ()); err != nil {
		t.Fatal("exec failed")
	}
}
