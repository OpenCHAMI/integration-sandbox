//go:build integration && declarative_boot

// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package declarativeboot

import (
	"context"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

func lifecycleRefreshFailureCase(t *testing.T) {
	bin := os.Getenv("SBX_DECLARATIVE_CONTROL_BIN")
	if bin == "" {
		t.Fatal("required environment variable SBX_DECLARATIVE_CONTROL_BIN is missing")
	}
	c := lifecycleNew(t)
	n := c.nodes(1)[0]
	group := c.group("outage", n)
	c.createA()
	c.createAssignment("group", AssignmentSpec{Selector: &Selector{Groups: []string{group}}, ConfigRef: ConfigReference{Name: c.series, RevisionName: "release-a"}})
	query := url.Values{"host": {n.xname}}
	c.requireMembershipTTL()
	c.warm(query, "A")
	control := lifecycleController{bin: bin, run: lifecycleControlRun}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if err := control.action(ctx, "service-start"); err != nil {
			t.Error("failed to restart owned SMD")
			return
		}
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			r, err := c.smd.HTTP.Do(ctx, Request{Method: http.MethodGet, Path: "/hsm/v2/service/ready"})
			if err == nil && r.Status == http.StatusOK {
				return
			}
			select {
			case <-ctx.Done():
				t.Error("owned SMD did not become ready after restart")
				return
			case <-ticker.C:
			}
		}
	}()
	stopCtx, stopCancel := context.WithTimeout(c.ctx, 15*time.Second)
	err := control.action(stopCtx, "service-stop")
	stopCancel()
	if err != nil {
		t.Fatal("failed to stop owned SMD")
	}
	ctx, cancel := context.WithTimeout(c.ctx, 5*time.Second)
	defer cancel()
	probeCtx, probeCancel := context.WithTimeout(ctx, 500*time.Millisecond)
	probe, probeErr := c.smd.HTTP.Do(probeCtx, Request{Method: http.MethodGet, Path: "/hsm/v2/service/ready"})
	probeCancel()
	if probeErr == nil && probe.Status < 500 {
		t.Fatal("SMD remained reachable after service-stop")
	}
	timer := time.NewTimer(1250 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		t.Fatal("SMD refresh deadline expired before the cache bound")
	case <-timer.C:
	}
	response, err := c.query(ctx, query)
	if err != nil {
		t.Fatalf("boot request after SMD stop: %v", err)
	}
	if response.Status != http.StatusServiceUnavailable {
		t.Fatalf("failed refresh returned HTTP %d, want 503", response.Status)
	}
	text := string(response.Body)
	if !strings.HasPrefix(text, "#!ipxe") || c.matches(response.Body, "A") || c.matches(response.Body, "B") {
		t.Fatal("failed refresh did not return a nonbooting refusal")
	}
	for _, key := range []string{"A", "B"} {
		artifact := c.fixtures.Expected.Artifacts[key]
		if strings.Contains(text, artifact.Kernel) || strings.Contains(text, artifact.Initrd) {
			t.Fatal("refusal leaked a cached boot artifact")
		}
	}
	if strings.Contains(text, c.fixtures.Legacy.Default.Kernel) || strings.Contains(text, c.fixtures.Legacy.Default.Initrd) {
		t.Fatal("failed refresh fell back to legacy content")
	}
	for _, line := range strings.Split(text, "\n") {
		fields := strings.Fields(line)
		if len(fields) > 0 && (fields[0] == "kernel" || fields[0] == "initrd" || fields[0] == "boot" || fields[0] == "chain") {
			t.Fatal("failed refresh returned an executable boot command")
		}
	}
}
