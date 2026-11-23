//go:build integration && declarative_boot

// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package declarativeboot

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"testing"
)

type safetyResult struct {
	response Response
	err      error
}

func safetyRace(ctx context.Context, jobs []func(context.Context) (Response, error)) []safetyResult {
	results := make([]safetyResult, len(jobs))
	var ready, done sync.WaitGroup
	ready.Add(len(jobs))
	done.Add(len(jobs))
	start := make(chan struct{})
	for i, job := range jobs {
		go func() {
			defer done.Done()
			ready.Done()
			select {
			case <-start:
				results[i].response, results[i].err = job(ctx)
			case <-ctx.Done():
				results[i].err = ctx.Err()
			}
		}()
	}
	ready.Wait()
	close(start)
	done.Wait()
	return results
}

func safetyStrongETag(value string) bool {
	if len(value) < 2 || value[0] != '"' || value[len(value)-1] != '"' {
		return false
	}
	for i := 1; i < len(value)-1; i++ {
		c := value[i]
		if c < 0x21 || c == 0x22 || c == 0x7f {
			return false
		}
	}
	return true
}

func safetyJSONEqual(left, right []byte) bool {
	decode := func(data []byte) (any, bool) {
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.UseNumber()
		var value any
		if err := decoder.Decode(&value); err != nil {
			return nil, false
		}
		var extra any
		if err := decoder.Decode(&extra); err != io.EOF {
			return nil, false
		}
		return value, true
	}
	a, aOK := decode(left)
	b, bOK := decode(right)
	return aOK && bOK && reflect.DeepEqual(a, b)
}

func safetyCollectionEqual(left, right []byte) bool {
	index := func(data []byte) (map[string]json.RawMessage, bool) {
		var rows []json.RawMessage
		if json.Unmarshal(data, &rows) != nil || rows == nil {
			return nil, false
		}
		result := make(map[string]json.RawMessage, len(rows))
		for _, row := range rows {
			var identity struct {
				Metadata Metadata `json:"metadata"`
			}
			if json.Unmarshal(row, &identity) != nil || identity.Metadata.Name == "" {
				return nil, false
			}
			if _, exists := result[identity.Metadata.Name]; exists {
				return nil, false
			}
			result[identity.Metadata.Name] = row
		}
		return result, true
	}
	a, aOK := index(left)
	b, bOK := index(right)
	if !aOK || !bOK || len(a) != len(b) {
		return false
	}
	for name, row := range a {
		other, found := b[name]
		if !found || !safetyJSONEqual(row, other) {
			return false
		}
	}
	return true
}

func safetyCheckRace(t *testing.T, results []safetyResult, winner int) int {
	t.Helper()
	winners := 0
	winningIndex := -1
	for i, result := range results {
		if result.err != nil {
			t.Fatalf("race request %d: %v", i, result.err)
		}
		switch result.response.Status {
		case winner:
			winners++
			winningIndex = i
		case http.StatusPreconditionFailed:
		default:
			t.Fatalf("race request %d returned HTTP %d", i, result.response.Status)
		}
	}
	if winners != 1 {
		t.Fatalf("race returned %d winners; want exactly one", winners)
	}
	return winningIndex
}

func safetySameAssignment(t *testing.T, before, after []byte, etag, afterETag string) {
	t.Helper()
	if !safetyJSONEqual(before, after) || etag != afterETag {
		t.Fatal("assignment or ETag changed after denied mutation")
	}
}

func safetyStale(etag string) string {
	if strings.Contains(etag, "safety-stale") {
		return `"safety-other-stale"`
	}
	return `"safety-stale"`
}
