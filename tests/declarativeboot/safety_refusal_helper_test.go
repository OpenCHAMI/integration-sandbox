//go:build integration && declarative_boot

// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package declarativeboot

import "testing"

func TestSafetyNonBootingRefusal(t *testing.T) {
	for _, tc := range []struct {
		name, script string
		want         bool
	}{
		{"halt", "#!ipxe\necho broken\nhalt\n", true},
		{"exit-error", "#!ipxe\necho broken\nexit 1\n", true},
		{"no-refusal", "#!ipxe\necho broken\n", false},
		{"kernel", "#!ipxe\nkernel /kernel\nhalt\n", false},
		{"chained", "#!ipxe\nchain http://example/boot\nhalt\n", false},
		{"sanboot", "#!ipxe\nsanboot http://example/image\nhalt\n", false},
		{"chained after condition", "#!ipxe\necho broken || chain http://example/boot\nhalt\n", false},
		{"exit-success", "#!ipxe\nexit 0\n", false},
		{"comment-only", "#!ipxe\n# halt\n", false},
		{"fallback-in-comment", "#!ipxe\n# /legacy-kernel\nhalt\n", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := safetyNonBootingRefusal(tc.script, []string{"/legacy-kernel"}); got != tc.want {
				t.Fatalf("non-booting refusal = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestSafetyWithoutAssignment(t *testing.T) {
	input := []byte(`[{"metadata":{"name":"a"},"spec":{"p":1}},{"metadata":{"name":"b"},"spec":{"p":2}}]`)
	filtered, err := safetyWithoutAssignment(input, "b")
	if err != nil || !safetyCollectionEqual(filtered, []byte(`[{"metadata":{"name":"a"},"spec":{"p":1}}]`)) {
		t.Fatal("filter did not preserve the unrelated record")
	}
	if _, err := safetyWithoutAssignment(input, "missing"); err == nil {
		t.Fatal("missing adopted assignment was accepted")
	}
	if _, err := safetyWithoutAssignment([]byte(`[{"metadata":{"name":"b"}},{"metadata":{"name":"b"}}]`), "b"); err == nil {
		t.Fatal("duplicate adopted assignment was accepted")
	}
}
