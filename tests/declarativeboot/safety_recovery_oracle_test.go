//go:build integration && declarative_boot

// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package declarativeboot

import (
	"encoding/json"
	"strings"
)

func safetyNonBootingRefusal(script string, forbidden []string) bool {
	if !strings.HasPrefix(script, "#!ipxe") {
		return false
	}
	for _, value := range forbidden {
		if value != "" && strings.Contains(script, value) {
			return false
		}
	}
	refused := false
	for _, line := range strings.Split(script, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		commands := strings.NewReplacer("&&", "\n", "||", "\n", ";", "\n").Replace(trimmed)
		for _, command := range strings.Split(commands, "\n") {
			fields := strings.Fields(command)
			if len(fields) == 0 {
				continue
			}
			switch fields[0] {
			case "kernel", "initrd", "boot", "chain", "sanboot", "imgexec", "exec":
				return false
			case "halt":
				refused = true
			case "exit":
				if len(fields) > 1 && fields[1] != "0" {
					refused = true
				}
			}
		}
	}
	return refused
}

func safetyWithoutAssignment(body []byte, name string) ([]byte, error) {
	var rows []json.RawMessage
	if err := json.Unmarshal(body, &rows); err != nil || rows == nil {
		return nil, ErrJSON
	}
	filtered := make([]json.RawMessage, 0, len(rows))
	removed := 0
	for _, row := range rows {
		var item struct {
			Metadata Metadata `json:"metadata"`
		}
		if err := json.Unmarshal(row, &item); err != nil || item.Metadata.Name == "" {
			return nil, ErrJSON
		}
		if item.Metadata.Name == name {
			removed++
			continue
		}
		filtered = append(filtered, row)
	}
	if removed != 1 {
		return nil, ErrJSON
	}
	return json.Marshal(filtered)
}
