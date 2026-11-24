//go:build integration && declarative_boot

// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package declarativeboot

import (
	"testing"
)

func TestLifecycleHelpersVerifierR2Counterexample(t *testing.T) {
	want := Artifact{Kernel: "https://images.example.invalid/boot-images/acceptance-a-kernel", Initrd: "https://images.example.invalid/boot-images/acceptance-a-initrd", Params: []string{"console=ttyS0", "root=live:s3://boot-images/acceptance-a-rootfs.squashfs", "release=A", "ip=dhcp"}}
	script := "#!ipxe\niseq 1 2 || goto failed\nkernel https://images.example.invalid/boot-images/acceptance-a-kernel console=ttyS0 root=live:s3://boot-images/acceptance-a-rootfs.squashfs release=A ip=dhcp\ninitrd https://images.example.invalid/boot-images/acceptance-a-initrd\nboot || goto failed\n:failed\nhalt\n"
	if lifecycleArtifactMatches(script, want) {
		t.Fatal("conditional early failure was accepted as a boot path")
	}
}

func TestLifecycleHelpers(t *testing.T) {
	t.Run("artifact exact tuple", func(t *testing.T) {
		want := Artifact{Kernel: "https://example/a", Initrd: "https://example/a.img", Params: []string{"release=A", "ip=dhcp"}}
		for _, tc := range []struct {
			name, script string
			valid        bool
		}{
			{"correct", "#!ipxe\nkernel https://example/a release=A ip=dhcp\ninitrd https://example/a.img\nboot\n", true},
			{"baseline commands", "\n#!ipxe\n# node\necho loading\ndhcp\nset kernel https://example/a\nset initrd https://example/a.img\nset params release=A ip=dhcp\nkernel ${kernel} ${params}\ninitrd ${initrd}\necho booting\nboot\n", true},
			{"missing shebang", "kernel https://example/a release=A ip=dhcp\ninitrd https://example/a.img\nboot\n", false},
			{"comment before shebang", "# comment\n#!ipxe\nkernel https://example/a release=A ip=dhcp\ninitrd https://example/a.img\nboot\n", false},
			{"conditional early jump", "#!ipxe\niseq 1 2 || goto failed\nkernel https://example/a release=A ip=dhcp\ninitrd https://example/a.img\nboot || goto failed\n:failed\nhalt\n", false},
			{"unguarded unknown", "#!ipxe\nunknown\nkernel https://example/a release=A ip=dhcp\ninitrd https://example/a.img\nboot\n", false},
			{"guarded echo", "#!ipxe\necho hello || goto failed\nkernel https://example/a release=A ip=dhcp\ninitrd https://example/a.img\nboot\n", false},
			{"guarded dhcp", "#!ipxe\ndhcp || goto failed\nkernel https://example/a release=A ip=dhcp\ninitrd https://example/a.img\nboot\n", false},
			{"guarded set", "#!ipxe\nset kernel https://example/a || goto failed\nset params release=A ip=dhcp\nset initrd https://example/a.img\nkernel ${kernel} ${params}\ninitrd ${initrd}\nboot\n", false},
			{"late kernel variable", "#!ipxe\nkernel ${kernel} ${params}\nset kernel https://example/a\nset params release=A ip=dhcp\nset initrd https://example/a.img\ninitrd ${initrd}\nboot\n", false},
			{"late initrd variable", "#!ipxe\nset kernel https://example/a\nset params release=A ip=dhcp\nkernel ${kernel} ${params}\ninitrd ${initrd}\nset initrd https://example/a.img\nboot\n", false},
			{"redefinition after load", "#!ipxe\nset kernel https://example/a\nset params release=A ip=dhcp\nset initrd https://example/a.img\nkernel ${kernel} ${params}\nset kernel https://example/b\ninitrd ${initrd}\nboot\n", true},
			{"boot before loads", "#!ipxe\nboot\nkernel https://example/a release=A ip=dhcp\ninitrd https://example/a.img\n", false},
			{"label before boot", "#!ipxe\n:failed\nkernel https://example/a release=A ip=dhcp\ninitrd https://example/a.img\nboot\n", false},
			{"early exit", "#!ipxe\nexit\nkernel https://example/a release=A ip=dhcp\ninitrd https://example/a.img\nboot\n", false},
			{"wrong failure label", "#!ipxe\nkernel https://example/a release=A ip=dhcp || goto elsewhere\ninitrd https://example/a.img\nboot\n", false},
			{"missing boot", "#!ipxe\nkernel https://example/a release=A ip=dhcp\ninitrd https://example/a.img\n", false},
			{"halt before boot", "#!ipxe\nkernel https://example/a release=A ip=dhcp\ninitrd https://example/a.img\nhalt\nboot\n", false},
			{"chain before boot", "#!ipxe\nkernel https://example/a release=A ip=dhcp\ninitrd https://example/a.img\nchain https://elsewhere\nboot\n", false},
			{"goto before boot", "#!ipxe\nkernel https://example/a release=A ip=dhcp\ninitrd https://example/a.img\ngoto failed\nboot\n:failed\nhalt\n", false},
			{"recovery after boot", "#!ipxe\nkernel https://example/a release=A ip=dhcp\ninitrd https://example/a.img\nboot || goto failed\n:failed\necho failed\nshell\ngoto menu\n", true},
			{"unlabeled chain after boot", "#!ipxe\nkernel https://example/a release=A ip=dhcp\ninitrd https://example/a.img\nboot\nchain https://elsewhere\n", false},
			{"duplicate boot", "#!ipxe\nkernel https://example/a release=A ip=dhcp\ninitrd https://example/a.img\nboot\nboot\n", false},
			{"guarded template", "#!ipxe\nset kernel https://example/a\nset initrd https://example/a.img\nset params release=A ip=dhcp\nkernel ${kernel} ${params} || goto failed\ninitrd ${initrd} || goto failed\nboot || goto failed\n:failed\nshell\ngoto menu\n", true},
			{"variable commands", "#!ipxe\nset kernel https://example/a\nset initrd https://example/a.img\nset params release=A ip=dhcp BOOTIF=01-02-00-00-00-00-01\nkernel ${kernel} ${params}\ninitrd ${initrd}\nboot\n", true},
			{"variable wrong kernel", "#!ipxe\nset kernel https://example/b\nset initrd https://example/a.img\nset params release=A ip=dhcp\nkernel ${kernel} ${params}\ninitrd ${initrd}\nboot\n", false},
			{"variable not used", "#!ipxe\nset kernel https://example/a\nset initrd https://example/a.img\nset params release=A ip=dhcp\nkernel https://example/b\ninitrd ${initrd}\nboot\n", false},
			{"wrong kernel", "#!ipxe\nkernel https://example/b release=A ip=dhcp\ninitrd https://example/a.img\nboot\n", false},
			{"wrong initrd", "#!ipxe\nkernel https://example/a release=A ip=dhcp\ninitrd https://example/b.img\nboot\n", false},
			{"partial token", "#!ipxe\nkernel https://example/a release=AX ip=dhcp\ninitrd https://example/a.img\nboot\n", false},
			{"extra parameter", "#!ipxe\nkernel https://example/a release=A ip=dhcp release=B\ninitrd https://example/a.img\nboot\n", false},
			{"empty kernel", "kernel\ninitrd https://example/a.img\n", false},
			{"comment only", "# kernel https://example/a release=A ip=dhcp\n# initrd https://example/a.img\n", false},
			{"duplicate kernel", "#!ipxe\nkernel https://example/a release=A ip=dhcp\nkernel https://example/b release=A ip=dhcp\ninitrd https://example/a.img\nboot\n", false},
			{"duplicate initrd", "#!ipxe\nkernel https://example/a release=A ip=dhcp\ninitrd https://example/a.img\ninitrd https://example/b.img\nboot\n", false},
		} {
			t.Run(tc.name, func(t *testing.T) {
				if got := lifecycleArtifactMatches(tc.script, want); got != tc.valid {
					t.Fatalf("matches = %v, want %v", got, tc.valid)
				}
			})
		}
	})
	t.Run("membership ttl metric", func(t *testing.T) {
		for _, tc := range []struct {
			name, text string
			valid      bool
		}{
			{"zero", "boot_service_declarative_membership_cache_ttl_seconds 0\n", true},
			{"one", "boot_service_declarative_membership_cache_ttl_seconds 1\n", true},
			{"fraction", "boot_service_declarative_membership_cache_ttl_seconds 0.75\n", true},
			{"over one", "boot_service_declarative_membership_cache_ttl_seconds 1.01\n", false},
			{"negative", "boot_service_declarative_membership_cache_ttl_seconds -0.1\n", false},
			{"nan", "boot_service_declarative_membership_cache_ttl_seconds NaN\n", false},
			{"missing", "other 0\n", false},
			{"duplicate", "boot_service_declarative_membership_cache_ttl_seconds 1\nboot_service_declarative_membership_cache_ttl_seconds 1\n", false},
		} {
			t.Run(tc.name, func(t *testing.T) {
				_, err := lifecycleMembershipTTL(tc.text)
				if (err == nil) != tc.valid {
					t.Fatalf("valid = %v, want %v", err == nil, tc.valid)
				}
			})
		}
	})
	t.Run("bootif mac", func(t *testing.T) {
		for _, tc := range []struct {
			text, mac string
			valid     bool
		}{
			{"set params BOOTIF=01-aa-bb-cc-dd-ee-ff\nboot\n", "AA:BB:CC:DD:EE:FF", true},
			{"set params BOOTIF=01-AA-BB-CC-DD-EE-FF\nboot\n", "aa:bb:cc:dd:ee:ff", true},
			{"set params BOOTIF=01-aa-bb-cc-dd-ee-00\nboot\n", "aa:bb:cc:dd:ee:ff", false},
			{"set params BOOTIF=02-aa-bb-cc-dd-ee-ff\nboot\n", "aa:bb:cc:dd:ee:ff", false},
			{"set params BOOTIF=01-aa-bb-cc-dd-ee-ff BOOTIF=01-aa-bb-cc-dd-ee-ff\nboot\n", "aa:bb:cc:dd:ee:ff", false},
		} {
			if got := lifecycleHasBootIF(tc.text, tc.mac); got != tc.valid {
				t.Fatalf("BOOTIF match = %v, want %v", got, tc.valid)
			}
		}
	})
	t.Run("cache metrics", func(t *testing.T) {
		for _, tc := range []struct {
			name, text string
			want       float64
			valid      bool
		}{
			{"plain", "# TYPE boot_service_declarative_cache_hits_total counter\nboot_service_declarative_cache_hits_total 42\n", 42, true},
			{"missing", "other_metric 5\n", 0, false},
			{"malformed", "boot_service_declarative_cache_hits_total nope\n", 0, false},
			{"nan", "boot_service_declarative_cache_hits_total NaN\n", 0, false},
			{"negative", "boot_service_declarative_cache_hits_total -1\n", 0, false},
			{"duplicate", "boot_service_declarative_cache_hits_total 1\nboot_service_declarative_cache_hits_total 2\n", 0, false},
			{"labelled", "boot_service_declarative_cache_hits_total{node=\"x\"} 2\n", 0, false},
		} {
			t.Run(tc.name, func(t *testing.T) {
				got, err := lifecycleCacheMetric(tc.text)
				if (err == nil) != tc.valid || (tc.valid && got != tc.want) {
					t.Fatalf("metric = %v, err = %v; want %v valid=%v", got, err, tc.want, tc.valid)
				}
			})
		}
	})
}
