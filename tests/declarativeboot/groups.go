//go:build integration && declarative_boot

// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package declarativeboot

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
)

// Group wire fields come from SMD pkg/sm/groups.go and its /hsm/v2/groups route.
type GroupMembers struct {
	IDs []string `json:"ids"`
}
type Group struct {
	Label          string       `json:"label"`
	Description    string       `json:"description"`
	ExclusiveGroup string       `json:"exclusiveGroup,omitempty"`
	Tags           []string     `json:"tags,omitempty"`
	Members        GroupMembers `json:"members"`
}

var ErrGroupConflict = errors.New("existing SMD group differs from fixture")

func (s SMDClient) CreateGroup(ctx context.Context, group Group) (Response, error) {
	body, err := EncodeJSON(group)
	if err != nil {
		return Response{}, err
	}
	return s.HTTP.Do(ctx, Request{Method: http.MethodPost, Path: "/hsm/v2/groups", Headers: s.headers(), Body: body})
}

// UpdateGroup replaces only membership using SMD's documented members resource.
// Group metadata is intentionally left unchanged.
func (s SMDClient) UpdateGroup(ctx context.Context, group Group) (Response, error) {
	body, err := EncodeJSON(group.Members)
	if err != nil {
		return Response{}, err
	}
	return s.HTTP.Do(ctx, Request{Method: http.MethodPut, Path: "/hsm/v2/groups/" + url.PathEscape(group.Label) + "/members", Headers: s.headers(), Body: body})
}

func (s SMDClient) GetGroup(ctx context.Context, label string) (Response, error) {
	return s.HTTP.Do(ctx, Request{Method: http.MethodGet, Path: "/hsm/v2/groups/" + url.PathEscape(label), Headers: s.headers()})
}
func DecodeGroup(response Response) (Group, error) {
	group, err := DecodeJSON[Group](response)
	fields, fieldsErr := DecodeJSON[struct {
		Description *string `json:"description"`
	}](response)
	if err != nil || fieldsErr != nil || fields.Description == nil || group.Label == "" || group.Members.IDs == nil {
		return Group{}, fmt.Errorf("incomplete group response: %w", ErrJSON)
	}
	return group, nil
}
func (s SMDClient) EnsureGroup(ctx context.Context, desired Group) error {
	if desired.Label == "" || desired.Members.IDs == nil {
		return fmt.Errorf("invalid group fixture: %w", ErrFixture)
	}
	response, err := s.CreateGroup(ctx, desired)
	if err != nil {
		return err
	}
	if response.Status != http.StatusConflict {
		return response.RequireStatus(http.StatusOK, http.StatusCreated, http.StatusNoContent)
	}
	response, err = s.GetGroup(ctx, desired.Label)
	if err != nil {
		return err
	}
	if err := response.RequireStatus(http.StatusOK); err != nil {
		return err
	}
	actual, err := DecodeGroup(response)
	if err != nil {
		return err
	}
	if actual.Label != desired.Label || actual.Description != desired.Description || !strings.EqualFold(actual.ExclusiveGroup, desired.ExclusiveGroup) || !equalNormalized(actual.Tags, desired.Tags) || !slices.Equal(slices.Sorted(slices.Values(actual.Members.IDs)), slices.Sorted(slices.Values(desired.Members.IDs))) {
		return ErrGroupConflict
	}
	return nil
}
func equalNormalized(a, b []string) bool {
	left := make([]string, len(a))
	right := make([]string, len(b))
	for i, v := range a {
		left[i] = strings.ToLower(v)
	}
	for i, v := range b {
		right[i] = strings.ToLower(v)
	}
	slices.Sort(left)
	slices.Sort(right)
	return slices.Equal(left, right)
}
