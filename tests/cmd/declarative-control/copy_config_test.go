// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestCopyBootConfigValidatesOwnedReadOnlyDirectory(t *testing.T) {
	run := nativeTempDir(t)
	config := filepath.Join(run, "config", "boot")
	if err := os.MkdirAll(config, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(config, "boot.json"), []byte(`{}`), 0444); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SBX_DECLARATIVE_BOOT_CONFIG_DIR", config)
	c := controller{runDir: run}
	tests := []struct {
		name, cmd, env, mounts, want string
		fail                         bool
	}{
		{"no auth", `["serve"]`, `[]`, `[]`, "", false},
		{"owned directory", `["serve","--tokensmith-url=http://tokensmith:27780","--config=/run/boot-config/boot.json"]`, `[]`, fmt.Sprintf(`[{"Type":"bind","Source":%q,"Destination":"/run/boot-config","RW":false}]`, config), config, false},
		{"missing bind", `["serve","--tokensmith-url=http://tokensmith:27780","--config=/run/boot-config/boot.json"]`, `[]`, `[]`, "", true},
		{"file bind", `["serve","--config=/run/boot-config/boot.json"]`, `[]`, fmt.Sprintf(`[{"Type":"bind","Source":%q,"Destination":"/run/boot-config/boot.json","RW":false}]`, filepath.Join(config, "boot.json")), "", true},
		{"wrong source", `["serve","--config=/run/boot-config/boot.json"]`, `[]`, fmt.Sprintf(`[{"Type":"bind","Source":%q,"Destination":"/run/boot-config","RW":false}]`, run), "", true},
		{"unowned source", `["serve","--config=/run/boot-config/boot.json"]`, `[]`, fmt.Sprintf(`[{"Type":"bind","Source":%q,"Destination":"/run/boot-config","RW":false}]`, t.TempDir()), "", true},
		{"duplicate bind", `["serve","--config=/run/boot-config/boot.json"]`, `[]`, fmt.Sprintf(`[{"Type":"bind","Source":%q,"Destination":"/run/boot-config","RW":false},{"Type":"bind","Source":%q,"Destination":"/run/boot-config","RW":false}]`, config, config), "", true},
		{"writable", `["serve","--config=/run/boot-config/boot.json"]`, `[]`, fmt.Sprintf(`[{"Type":"bind","Source":%q,"Destination":"/run/boot-config","RW":true}]`, config), "", true},
		{"unknown mode", `["serve","--config=/run/boot-config/boot.json"]`, `[]`, fmt.Sprintf(`[{"Type":"bind","Source":%q,"Destination":"/run/boot-config"}]`, config), "", true},
		{"obsolete env credential", `["serve","--config=/run/boot-config/boot.json"]`, `["TOKENSMITH_BOOTSTRAP_TOKEN=old"]`, fmt.Sprintf(`[{"Type":"bind","Source":%q,"Destination":"/run/boot-config","RW":false}]`, config), "", true},
		{"obsolete command credential", `["serve","--tokensmith-bootstrap-token=old","--config=/run/boot-config/boot.json"]`, `[]`, fmt.Sprintf(`[{"Type":"bind","Source":%q,"Destination":"/run/boot-config","RW":false}]`, config), "", true},
		{"wrong config argument", `["serve","--tokensmith-url=http://tokensmith:27780","--config=/tmp/boot.json"]`, `[]`, fmt.Sprintf(`[{"Type":"bind","Source":%q,"Destination":"/run/boot-config","RW":false}]`, config), "", true},
		{"split config argument", `["serve","--auth-enabled","true","--config","/run/boot-config/boot.json"]`, `[]`, fmt.Sprintf(`[{"Type":"bind","Source":%q,"Destination":"/run/boot-config","RW":false}]`, config), config, false},
		{"split auth without config", `["serve","--auth-enabled","true"]`, `[]`, `[]`, "", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var inspect copyInspect
			body := fmt.Sprintf(`{"Config":{"Cmd":%s,"Env":%s},"Mounts":%s}`, tc.cmd, tc.env, tc.mounts)
			if err := json.Unmarshal([]byte(body), &inspect); err != nil {
				t.Fatal(err)
			}
			got, err := c.copyBootConfig(inspect)
			if (err != nil) != tc.fail || got != tc.want {
				t.Fatalf("config directory %q error %v", got, err)
			}
		})
	}
}
