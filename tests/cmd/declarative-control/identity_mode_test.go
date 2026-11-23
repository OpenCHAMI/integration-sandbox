// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package main

import "testing"

func TestIdentityModeUsesInspectedCommandAndEnvironment(t *testing.T) {
	valid := []string{"serve", "--enable-auth=true", "--hsm-url=http://smd:27779", "--tokensmith-url=http://tokensmith:27780", "--config=/run/boot-config/boot.json"}
	for _, tc := range []struct {
		name               string
		cmd, env           []string
		mount, want, fails bool
	}{
		{"authenticated", valid, nil, true, true, false},
		{"auth off", []string{"serve", "--auth-enabled=false", "--hsm-url=http://smd"}, nil, false, false, false},
		{"environment", []string{"serve", "--config=/run/boot-config/boot.json"}, []string{"BOOT_SERVICE_ENABLE_AUTH=true", "BOOT_SERVICE_HSM_URL=http://smd", "TOKENSMITH_URL=http://tokensmith"}, true, true, false},
		{"missing mount", valid, nil, false, false, true},
		{"static token", valid, []string{"TOKENSMITH_BOOTSTRAP_TOKEN=old"}, true, false, true},
		{"ambiguous auth", []string{"serve", "--hsm-url=http://smd", "--tokensmith-url=http://tokensmith"}, nil, true, false, true},
		{"empty private test command", nil, nil, false, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := container{Config: containerConfig{Cmd: tc.cmd, Env: tc.env}, identityMountReadOnly: tc.mount}
			got, err := needsBootIdentity(v)
			if got != tc.want || (err != nil) != tc.fails {
				t.Fatal("unexpected identity mode")
			}
		})
	}
}
