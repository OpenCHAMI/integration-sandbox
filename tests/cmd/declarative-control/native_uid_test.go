// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package main

import (
	"strings"
	"testing"
)

func TestNativeUIDUsesRevisionTypeID(t *testing.T) {
	first, err := nativeRevisionUID()
	if err != nil {
		t.Fatal(err)
	}
	second, err := nativeRevisionUID()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(first, "rev_") || len(first) != 30 || first == second {
		t.Fatalf("bad revision identities %q %q", first, second)
	}
}
