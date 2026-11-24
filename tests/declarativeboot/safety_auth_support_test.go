//go:build integration && declarative_boot

// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package declarativeboot

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"testing"
	"time"
)

type safetyRunner struct{ bin string }

type tokenFixtureRequest struct {
	Kind   string   `json:"kind"`
	Scopes []string `json:"scopes"`
}

type tokenFixtureResponse struct {
	Token string `json:"token"`
}

type brokenReferenceRequest struct {
	AssignmentName string `json:"assignmentName"`
	SeriesName     string `json:"seriesName"`
	RevisionUID    string `json:"revisionUid"`
	NodeIdentifier string `json:"nodeIdentifier"`
}

type baseURLResponse struct {
	BaseURL string `json:"baseURL"`
}

type serviceActionRequest struct {
	Service string `json:"service"`
}

type legacyBootConfiguration struct {
	Metadata struct {
		Name string `json:"name"`
		UID  string `json:"uid"`
	} `json:"metadata"`
	Spec struct {
		Hosts    []string `json:"hosts,omitempty"`
		MACs     []string `json:"macs,omitempty"`
		NIDs     []int32  `json:"nids,omitempty"`
		Groups   []string `json:"groups,omitempty"`
		Profile  string   `json:"profile,omitempty"`
		Kernel   string   `json:"kernel"`
		Initrd   string   `json:"initrd,omitempty"`
		Params   string   `json:"params,omitempty"`
		Priority int      `json:"priority,omitempty"`
	} `json:"spec"`
}

type safetyHarness struct {
	t         *testing.T
	ctx       context.Context
	runner    safetyRunner
	fixtures  Fixtures
	bootHTTP  *HTTPClient
	tokenHTTP *HTTPClient
	issuer    string
	admin     BootClient
}

func newSafetyHarness(t *testing.T) *safetyHarness {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	t.Cleanup(cancel)
	fixtures, err := LoadFixtures("../../fixtures/declarative-boot")
	if err != nil {
		t.Fatalf("load fixtures: %v", err)
	}
	bootHTTP, err := NewHTTPClient(safetyEnv(t, "SBX_DECLARATIVE_BOOT_URL"), 10*time.Second)
	if err != nil {
		t.Fatalf("boot client: %v", err)
	}
	tokenHTTP, err := NewHTTPClient(safetyEnv(t, "SBX_DECLARATIVE_TOKENSMITH_URL"), 10*time.Second)
	if err != nil {
		t.Fatalf("tokensmith client: %v", err)
	}
	issuer := os.Getenv("SBX_DECLARATIVE_ISSUER")
	if issuer == "" {
		issuer = "http://tokensmith:27780"
	}
	h := &safetyHarness{
		t:         t,
		ctx:       ctx,
		runner:    safetyRunner{bin: safetyEnv(t, "SBX_DECLARATIVE_CONTROL_BIN")},
		fixtures:  fixtures,
		bootHTTP:  bootHTTP,
		tokenHTTP: tokenHTTP,
		issuer:    issuer,
	}
	h.admin = BootClient{HTTP: bootHTTP, Bearer: h.mintToken([]string{"read:boot", "propose:boot", "promote:boot"})}
	return h
}

func (h *safetyHarness) mintToken(scopes []string) string {
	h.t.Helper()
	bootstrap, err := CreateBootstrapToken(h.ctx, DockerExec, BootstrapOptions{
		Container: safetyEnv(h.t, "SBX_DECLARATIVE_TOKENSMITH_CONTAINER"),
		Store:     safetyEnv(h.t, "SBX_DECLARATIVE_TOKENSMITH_STORE"),
		Subject:   safetyName(h.t),
		Audience:  "boot-service",
		Scopes:    scopes,
		TTL:       "5m",
	})
	if err != nil {
		h.t.Fatalf("bootstrap token: %v", err)
	}
	token, err := ExchangeToken(h.ctx, h.tokenHTTP, bootstrap)
	if err != nil {
		h.t.Fatalf("exchange token: %v", err)
	}
	if err := VerifyToken(h.ctx, h.tokenHTTP, token, TokenPolicy{Issuer: h.issuer, Audience: "boot-service", Scopes: scopes}); err != nil {
		h.t.Fatalf("verify token: %v", err)
	}
	return token
}

func (h *safetyHarness) runControl(action string, request any, response any) {
	h.t.Helper()
	body, err := json.Marshal(request)
	if err != nil {
		h.t.Fatalf("marshal %s request: %v", action, err)
	}
	out, err := exec.CommandContext(h.ctx, h.runner.bin, action, string(body)).Output()
	if err != nil {
		h.t.Fatalf("control %s: %v", action, err)
	}
	if response != nil {
		if err := json.Unmarshal(out, response); err != nil {
			h.t.Fatalf("decode %s response: %v", action, err)
		}
	}
	if v, ok := response.(*baseURLResponse); ok && v.BaseURL == "" {
		h.t.Fatalf("control %s returned empty baseURL", action)
	}
	if v, ok := response.(*tokenFixtureResponse); ok && v.Token == "" {
		h.t.Fatalf("control %s returned empty token", action)
	}
}

func (h *safetyHarness) node(role string) FixtureNode {
	h.t.Helper()
	for _, node := range h.fixtures.Nodes.Nodes {
		if node.Role == role {
			return node
		}
	}
	h.t.Fatalf("missing fixture node for role %s", role)
	return FixtureNode{}
}

func (h *safetyHarness) bootscript(base string, node FixtureNode) Response {
	h.t.Helper()
	client, err := NewHTTPClient(base, 10*time.Second)
	if err != nil {
		h.t.Fatalf("bootscript client: %v", err)
	}
	response, err := client.Do(h.ctx, Request{Method: http.MethodGet, Path: "/bootscript?host=" + url.QueryEscape(node.Xname)})
	if err != nil {
		h.t.Fatalf("GET /bootscript: %v", err)
	}
	return response
}

func (h *safetyHarness) createLegacySource(node FixtureNode, groups []string) legacyBootConfiguration {
	h.t.Helper()
	body, err := EncodeJSON(map[string]any{
		"metadata": map[string]any{"name": safetyName(h.t)},
		"spec": map[string]any{
			"hosts":  []string{node.Xname},
			"groups": groups,
			"kernel": h.fixtures.Legacy.Default.Kernel,
			"initrd": h.fixtures.Legacy.Default.Initrd,
			"params": h.fixtures.Legacy.Default.Params,
		},
	})
	if err != nil {
		h.t.Fatalf("encode legacy source: %v", err)
	}
	response, err := h.admin.Raw(h.ctx, Request{Method: http.MethodPost, Path: "/bootconfigurations", Headers: http.Header{"Content-Type": {"application/json"}}, Body: body})
	safetyStatus(h.t, response, err, http.StatusCreated)
	value, err := DecodeJSON[legacyBootConfiguration](response)
	if err != nil || value.Metadata.UID == "" {
		h.t.Fatalf("decode legacy source: %v", err)
	}
	return value
}
