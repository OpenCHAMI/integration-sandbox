//go:build integration && declarative_boot

// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package declarativeboot

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// Wire shapes and routes are from Fabrica v0.4.15 pkg/revision/models.go,
// file_store.go Record, and docs/guides/resource-revisions.md.
type RevisionMetadata struct {
	UID          string     `json:"uid"`
	SeriesName   string     `json:"seriesName"`
	RevisionName string     `json:"revisionName,omitempty"`
	Number       uint64     `json:"number"`
	Digest       string     `json:"digest"`
	CreatedAt    time.Time  `json:"createdAt"`
	RetiredAt    *time.Time `json:"retiredAt,omitempty"`
}
type RevisionRecord struct {
	Metadata RevisionMetadata `json:"metadata"`
	Resource BootConfig       `json:"resource"`
}
type SeriesStatus struct {
	Name               string    `json:"name"`
	DefaultRevisionUID string    `json:"defaultRevisionUid"`
	LatestRevisionUID  string    `json:"latestRevisionUid"`
	RevisionCount      uint64    `json:"revisionCount"`
	UpdatedAt          time.Time `json:"updatedAt"`
}
type ResolveResponse struct {
	SeriesName string           `json:"seriesName"`
	Selector   string           `json:"selector,omitempty"`
	Revision   RevisionMetadata `json:"revision"`
}
type RevisionID struct {
	Series string
	UID    string
}
type Promotion struct {
	Series      string
	RevisionUID string
	IfMatch     string
}
type promotionBody struct {
	RevisionUID string `json:"revisionUid"`
}

func validRevision(m RevisionMetadata) bool {
	return m.UID != "" && m.SeriesName != "" && m.Number > 0 && m.Digest != "" && !m.CreatedAt.IsZero()
}
func DecodeRevision(r Response) (RevisionRecord, error) {
	v, err := DecodeJSON[RevisionRecord](r)
	if err != nil || !validRevision(v.Metadata) || v.Resource.Metadata.Name == "" || v.Resource.Spec.Kernel == "" {
		return RevisionRecord{}, fmt.Errorf("incomplete revision response: %w", ErrJSON)
	}
	return v, nil
}
func DecodeRevisions(r Response) ([]RevisionRecord, error) {
	values, err := DecodeJSON[[]RevisionRecord](r)
	if err != nil || values == nil {
		return nil, fmt.Errorf("invalid revision list: %w", ErrJSON)
	}
	for _, v := range values {
		if !validRevision(v.Metadata) || v.Resource.Metadata.Name == "" || v.Resource.Spec.Kernel == "" {
			return nil, fmt.Errorf("incomplete revision list: %w", ErrJSON)
		}
	}
	return values, nil
}
func DecodeSeries(r Response) (SeriesStatus, error) {
	v, err := DecodeJSON[SeriesStatus](r)
	if err != nil || v.Name == "" || v.DefaultRevisionUID == "" || v.LatestRevisionUID == "" || v.RevisionCount == 0 || v.UpdatedAt.IsZero() {
		return SeriesStatus{}, fmt.Errorf("incomplete series response: %w", ErrJSON)
	}
	return v, nil
}
func DecodeResolution(r Response) (ResolveResponse, error) {
	v, err := DecodeJSON[ResolveResponse](r)
	if err != nil || v.SeriesName == "" || !validRevision(v.Revision) || v.SeriesName != v.Revision.SeriesName {
		return ResolveResponse{}, fmt.Errorf("incomplete resolution response: %w", ErrJSON)
	}
	return v, nil
}
func (b BootClient) GetSeries(ctx context.Context, series string) (Response, error) {
	return b.Raw(ctx, Request{Method: http.MethodGet, Path: "/bootconfigs/" + url.PathEscape(series)})
}
func (b BootClient) ListRevisions(ctx context.Context, series string) (Response, error) {
	return b.Raw(ctx, Request{Method: http.MethodGet, Path: "/bootconfigs/" + url.PathEscape(series) + "/revisions"})
}
func (b BootClient) GetRevision(ctx context.Context, id RevisionID) (Response, error) {
	return b.Raw(ctx, Request{Method: http.MethodGet, Path: "/bootconfigs/" + url.PathEscape(id.Series) + "/revisions/by-uid/" + url.PathEscape(id.UID)})
}
func (b BootClient) ResolveRevision(ctx context.Context, ref ConfigReference) (Response, error) {
	query := url.Values{}
	if ref.UID != "" {
		query.Set("uid", ref.UID)
	}
	if ref.RevisionName != "" {
		query.Set("revisionName", ref.RevisionName)
	}
	if ref.Selector != "" {
		query.Set("selector", ref.Selector)
	}
	path := "/bootconfigs/" + url.PathEscape(ref.Name) + "/resolve"
	if len(query) > 0 {
		path += "?" + query.Encode()
	}
	return b.Raw(ctx, Request{Method: http.MethodGet, Path: path})
}
func (b BootClient) PromoteDefault(ctx context.Context, value Promotion) (Response, error) {
	body, err := EncodeJSON(promotionBody{RevisionUID: value.RevisionUID})
	if err != nil {
		return Response{}, err
	}
	header := http.Header{"Content-Type": {"application/json"}}
	if value.IfMatch != "" {
		header.Set("If-Match", value.IfMatch)
	}
	return b.Raw(ctx, Request{Method: http.MethodPut, Path: "/bootconfigs/" + url.PathEscape(value.Series) + "/default", Headers: header, Body: body})
}
