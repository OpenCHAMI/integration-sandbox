//go:build integration && declarative_boot

// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package declarativeboot

import (
	"context"
	"net/http"
)

type Component struct {
	ID      string `json:"ID"`
	Type    string `json:"Type"`
	State   string `json:"State,omitempty"`
	Flag    string `json:"Flag,omitempty"`
	Enabled bool   `json:"Enabled"`
	Role    string `json:"Role,omitempty"`
	NID     int32  `json:"NID,omitempty"`
}
type Components struct {
	Components []Component `json:"Components"`
}
type IPAddress struct {
	IPAddress string `json:"IPAddress"`
	Network   string `json:"Network"`
}
type EthernetInterface struct {
	ID          string      `json:"ID"`
	Description string      `json:"Description,omitempty"`
	MACAddress  string      `json:"MACAddress"`
	IPAddresses []IPAddress `json:"IPAddresses,omitempty"`
	ComponentID string      `json:"ComponentID"`
	Type        string      `json:"Type,omitempty"`
}
type SMDClient struct {
	HTTP   *HTTPClient
	Bearer string
}

func (s SMDClient) PutComponents(ctx context.Context, value Components) error {
	body, err := EncodeJSON(value)
	if err != nil {
		return err
	}
	resp, err := s.HTTP.Do(ctx, Request{Method: http.MethodPost, Path: "/hsm/v2/State/Components", Headers: s.headers(), Body: body})
	if err != nil {
		return err
	}
	return resp.RequireStatus(200, 201, 204)
}
func (s SMDClient) CreateInterface(ctx context.Context, value EthernetInterface) error {
	body, err := EncodeJSON(value)
	if err != nil {
		return err
	}
	resp, err := s.HTTP.Do(ctx, Request{Method: http.MethodPost, Path: "/hsm/v2/Inventory/EthernetInterfaces", Headers: s.headers(), Body: body})
	if err != nil {
		return err
	}
	return resp.RequireStatus(200, 201, 204)
}
func (s SMDClient) headers() http.Header {
	header := http.Header{"Content-Type": {"application/json"}}
	if s.Bearer != "" {
		header.Set("Authorization", "Bearer "+s.Bearer)
	}
	return header
}
