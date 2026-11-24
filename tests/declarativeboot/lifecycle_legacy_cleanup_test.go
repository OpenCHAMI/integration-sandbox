//go:build integration && declarative_boot

// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package declarativeboot

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"reflect"
)

func lifecycleSameJSON(a, b json.RawMessage) bool {
	var left, right any
	return json.Unmarshal(a, &left) == nil && json.Unmarshal(b, &right) == nil && reflect.DeepEqual(left, right)
}

func lifecycleLegacyCleanup(boot BootClient, uid, xname string, original json.RawMessage, applied *json.RawMessage, created bool) func(context.Context) error {
	return func(ctx context.Context) error {
		path := "/nodes/" + url.PathEscape(uid)
		r, err := boot.Raw(ctx, Request{Method: http.MethodGet, Path: path})
		if err != nil {
			return err
		}
		if err := r.RequireStatus(http.StatusOK); err != nil {
			return err
		}
		var current lifecycleLegacyNode
		var currentSpec lifecycleLegacyNodeSpec
		if err := json.Unmarshal(r.Body, &current); err != nil || current.Metadata.UID != uid || json.Unmarshal(current.Spec, &currentSpec) != nil || currentSpec.Xname != xname {
			return errors.New("legacy cleanup identity mismatch")
		}
		header := make(http.Header)
		if etag := r.Header.Get("ETag"); lifecycleStrongETag(etag) {
			header.Set("If-Match", etag)
		}
		if created {
			r, err = boot.Raw(ctx, Request{Method: http.MethodDelete, Path: path, Headers: header})
			if err != nil {
				return err
			}
			return r.RequireStatus(http.StatusOK, http.StatusNoContent)
		}
		if lifecycleSameJSON(current.Spec, original) {
			return nil
		}
		if !lifecycleSameJSON(current.Spec, *applied) {
			return errors.New("legacy node changed before restore")
		}
		body, err := json.Marshal(struct {
			Spec json.RawMessage `json:"spec"`
		}{Spec: original})
		if err != nil {
			return err
		}
		header.Set("Content-Type", "application/json")
		r, err = boot.Raw(ctx, Request{Method: http.MethodPut, Path: path, Headers: header, Body: body})
		if err != nil {
			return err
		}
		return r.RequireStatus(http.StatusOK)
	}
}
