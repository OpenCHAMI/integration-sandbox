//go:build integration && declarative_boot

// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package declarativeboot

import (
	"context"
	"net/http"
	"net/url"
)

type BootClient struct {
	HTTP   *HTTPClient
	Bearer string
}
type NamedConfig struct {
	RevisionName string
	Resource     BootConfig
}
type ConditionalAssignment struct {
	Resource    BootAssignment
	IfMatch     string
	IfNoneMatch string
}

func (b BootClient) Raw(ctx context.Context, request Request) (Response, error) {
	request.Headers = request.Headers.Clone()
	if request.Headers == nil {
		request.Headers = make(http.Header)
	}
	if b.Bearer != "" {
		request.Headers.Set("Authorization", "Bearer "+b.Bearer)
	}
	return b.HTTP.Do(ctx, request)
}
func (b BootClient) CreateConfig(ctx context.Context, value NamedConfig) (Response, error) {
	body, err := EncodeJSON(value.Resource)
	if err != nil {
		return Response{}, err
	}
	query := url.Values{"revisionName": {value.RevisionName}}
	return b.Raw(ctx, Request{Method: http.MethodPost, Path: "/bootconfigs?" + query.Encode(), Headers: http.Header{"Content-Type": {"application/json"}}, Body: body})
}
func (b BootClient) EnsureRevision(ctx context.Context, series string, value NamedConfig) (Response, error) {
	body, err := EncodeJSON(value.Resource)
	if err != nil {
		return Response{}, err
	}
	return b.Raw(ctx, Request{Method: http.MethodPut, Path: "/bootconfigs/" + url.PathEscape(series) + "/revisions/" + url.PathEscape(value.RevisionName), Headers: http.Header{"Content-Type": {"application/json"}}, Body: body})
}

func (b BootClient) ApplyAssignment(ctx context.Context, value ConditionalAssignment) (Response, error) {
	body, err := EncodeJSON(value.Resource)
	if err != nil {
		return Response{}, err
	}
	header := http.Header{"Content-Type": {"application/json"}}
	if value.IfMatch != "" {
		header.Set("If-Match", value.IfMatch)
	}
	if value.IfNoneMatch != "" {
		header.Set("If-None-Match", value.IfNoneMatch)
	}
	return b.Raw(ctx, Request{Method: http.MethodPut, Path: "/bootassignments/by-name/" + url.PathEscape(value.Resource.Metadata.Name), Headers: header, Body: body})
}
