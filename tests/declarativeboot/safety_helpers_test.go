//go:build integration && declarative_boot

// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package declarativeboot

import (
	"context"
	"errors"
	"net/http"
	"testing"
)

func TestSafetyStrongETag(t *testing.T) {
	for _, tc := range []struct {
		name  string
		etag  string
		valid bool
	}{
		{"strong", `"abc"`, true},
		{"empty opaque tag", `""`, true},
		{"weak", `W/"abc"`, false},
		{"wildcard", "*", false},
		{"unquoted", "abc", false},
		{"multiple", `"a", "b"`, false},
		{"invalid space", `"a b"`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := safetyStrongETag(tc.etag); got != tc.valid {
				t.Fatalf("safetyStrongETag(%q) = %v; want %v", tc.etag, got, tc.valid)
			}
		})
	}
}

func TestSafetyBarrierCollectsAllResults(t *testing.T) {
	ctx := context.Background()
	jobs := []func(context.Context) (Response, error){
		func(context.Context) (Response, error) { return Response{Status: http.StatusCreated}, nil },
		func(context.Context) (Response, error) { return Response{Status: http.StatusPreconditionFailed}, nil },
		func(context.Context) (Response, error) { return Response{}, errors.New("transport") },
	}
	results := safetyRace(ctx, jobs)
	if len(results) != 3 || results[0].response.Status != http.StatusCreated || results[1].response.Status != http.StatusPreconditionFailed || results[2].err == nil {
		t.Fatalf("barrier did not retain each independent result: %#v", results)
	}
}

func TestSafetyJSONEqual(t *testing.T) {
	for _, tc := range []struct {
		name, left, right string
		equal             bool
	}{
		{"key order", `{"a":1,"b":2}`, `{"b":2,"a":1}`, true},
		{"unmodeled mutation", `{"metadata":{"labels":{"x":"a"}}}`, `{"metadata":{"labels":{"x":"b"}}}`, false},
		{"large integers", `{"v":9007199254740992}`, `{"v":9007199254740993}`, false},
		{"invalid input", `{`, `{`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := safetyJSONEqual([]byte(tc.left), []byte(tc.right)); got != tc.equal {
				t.Fatalf("JSON equality = %v; want %v", got, tc.equal)
			}
		})
	}
}

func TestSafetyArtifactTuple(t *testing.T) {
	want := BootConfigSpec{Kernel: "/kernel-a", Initrd: "/initrd-a", Params: "release=A ip=dhcp"}
	for _, tc := range []struct {
		name, script string
		valid        bool
	}{
		{"exact", "#!ipxe\nkernel /kernel-a release=A ip=dhcp\ninitrd /initrd-a\nboot\n", true},
		{"wrong kernel", "#!ipxe\nkernel /kernel-b release=A ip=dhcp\ninitrd /initrd-a\nboot\n", false},
		{"wrong initrd", "#!ipxe\nkernel /kernel-a release=A ip=dhcp\ninitrd /initrd-b\nboot\n", false},
		{"extra token", "#!ipxe\nkernel /kernel-a release=A ip=dhcp release=B\ninitrd /initrd-a\nboot\n", false},
		{"duplicate", "#!ipxe\nkernel /kernel-a release=A ip=dhcp\nkernel /kernel-b\ninitrd /initrd-a\nboot\n", false},
		{"empty then duplicate", "#!ipxe\nkernel\nkernel /kernel-a release=A ip=dhcp\ninitrd /initrd-a\nboot\n", false},
		{"comments only", "#!ipxe\n# kernel /kernel-a release=A ip=dhcp\n# initrd /initrd-a\nboot\n", false},
		{"early exit", "#!ipxe\nexit 1\nkernel /kernel-a release=A ip=dhcp\ninitrd /initrd-a\nboot\n", false},
		{"early chain", "#!ipxe\nchain /elsewhere\nkernel /kernel-a release=A ip=dhcp\ninitrd /initrd-a\nboot\n", false},
		{"early goto", "#!ipxe\ngoto failed\nkernel /kernel-a release=A ip=dhcp\ninitrd /initrd-a\nboot\n", false},
		{"template recovery", "#!ipxe\nkernel /kernel-a release=A ip=dhcp || goto failed\ninitrd /initrd-a || goto failed\nboot || goto failed\n:failed\necho failed\nhalt\n", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := safetyArtifactTuple(tc.script, want); got != tc.valid {
				t.Fatalf("artifact match = %v; want %v", got, tc.valid)
			}
		})
	}
}

func TestSafetyCollectionEqual(t *testing.T) {
	for _, tc := range []struct {
		name, left, right string
		equal             bool
	}{
		{"reordered", `[{"metadata":{"name":"a"}},{"metadata":{"name":"b"}}]`, `[{"metadata":{"name":"b"}},{"metadata":{"name":"a"}}]`, true},
		{"changed unmodeled field", `[{"metadata":{"name":"a"},"other":1}]`, `[{"metadata":{"name":"a"},"other":2}]`, false},
		{"duplicate name", `[{"metadata":{"name":"a"}},{"metadata":{"name":"a"}}]`, `[{"metadata":{"name":"a"}},{"metadata":{"name":"a"}}]`, false},
		{"missing name", `[{"metadata":{}}]`, `[{"metadata":{}}]`, false},
		{"invalid JSON", `null`, `null`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := safetyCollectionEqual([]byte(tc.left), []byte(tc.right)); got != tc.equal {
				t.Fatalf("collection equality = %v; want %v", got, tc.equal)
			}
		})
	}
}

func TestSafetySMDIdentity(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		valid      bool
	}{
		{"direct", `{"ID":"x0c0s3b0n0"}`, true},
		{"wrapped", `{"Components":[{"ID":"x0c0s3b0n0"}]}`, true},
		{"wrong", `{"ID":"x0c0s4b0n0"}`, false},
		{"empty", `{"Components":[]}`, false},
		{"invalid", `{`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := safetySMDIdentity([]byte(tc.body)); got != tc.valid {
				t.Fatalf("SMD identity match = %v; want %v", got, tc.valid)
			}
		})
	}
}
