//go:build integration && declarative_boot

// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package declarativeboot

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
)

type ConditionalName struct {
	Name        string
	IfMatch     string
	IfNoneMatch string
}

func (b BootClient) GetAssignment(ctx context.Context, name string) (Response, error) {
	return b.GetAssignmentConditional(ctx, ConditionalName{Name: name})
}
func (b BootClient) GetAssignmentConditional(ctx context.Context, value ConditionalName) (Response, error) {
	header := make(http.Header)
	if value.IfMatch != "" {
		header.Set("If-Match", value.IfMatch)
	}
	if value.IfNoneMatch != "" {
		header.Set("If-None-Match", value.IfNoneMatch)
	}
	return b.Raw(ctx, Request{Method: http.MethodGet, Path: "/bootassignments/by-name/" + url.PathEscape(value.Name), Headers: header})
}
func (b BootClient) DeleteAssignment(ctx context.Context, value ConditionalName) (Response, error) {
	header := make(http.Header)
	if value.IfMatch != "" {
		header.Set("If-Match", value.IfMatch)
	}
	if value.IfNoneMatch != "" {
		header.Set("If-None-Match", value.IfNoneMatch)
	}
	return b.Raw(ctx, Request{Method: http.MethodDelete, Path: "/bootassignments/by-name/" + url.PathEscape(value.Name), Headers: header})
}
func DecodeAssignment(r Response) (BootAssignment, error) {
	v, err := DecodeJSON[BootAssignment](r)
	if err != nil || v.Metadata.Name == "" || v.Metadata.UID == "" || v.Spec.ConfigRef.Name == "" {
		return BootAssignment{}, fmt.Errorf("incomplete assignment response: %w", ErrJSON)
	}
	return v, nil
}
