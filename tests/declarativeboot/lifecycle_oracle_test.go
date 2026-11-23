//go:build integration && declarative_boot

// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package declarativeboot

import (
	"bytes"
	"errors"
	"math"
	"net"
	"slices"
	"strconv"
	"strings"
)

func lifecycleArtifactMatches(script string, want Artifact) bool {
	var kernel, initrd []string
	values := map[string][]string{}
	shebang := false
	lines := strings.Split(script, "\n")
	for lineIndex, line := range lines {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if !shebang {
			if strings.TrimSpace(line) != "#!ipxe" {
				return false
			}
			shebang = true
			continue
		}
		if strings.HasPrefix(fields[0], "#") {
			continue
		}
		command := fields[0]
		if len(fields) >= 4 && fields[len(fields)-3] == "||" && fields[len(fields)-2] == "goto" && fields[len(fields)-1] == "failed" {
			if command != "kernel" && command != "initrd" && command != "boot" {
				return false
			}
			fields = fields[:len(fields)-3]
		}
		for _, field := range fields {
			if strings.Contains(field, "||") || strings.Contains(field, "&&") || strings.Contains(field, ";") {
				return false
			}
		}
		switch command {
		case "echo", "dhcp":
			continue
		case "boot":
			if len(fields) != 1 || len(kernel) == 0 || len(initrd) != 1 {
				return false
			}
			if strings.HasPrefix(kernel[len(kernel)-1], "BOOTIF=") {
				kernel = kernel[:len(kernel)-1]
			}
			return len(kernel) == len(want.Params)+1 && kernel[0] == want.Kernel && initrd[0] == want.Initrd && slices.Equal(kernel[1:], want.Params) && lifecycleLabeledRecovery(lines[lineIndex+1:])
		case "set":
			if len(fields) < 2 || strings.ContainsAny(fields[1], "${}") {
				return false
			}
			value, ok := lifecycleExpand(fields[2:], values)
			if !ok {
				return false
			}
			values[fields[1]] = value
		case "kernel":
			if kernel != nil {
				return false
			}
			var ok bool
			kernel, ok = lifecycleExpand(fields[1:], values)
			if !ok || len(kernel) == 0 {
				return false
			}
		case "initrd":
			if initrd != nil {
				return false
			}
			var ok bool
			initrd, ok = lifecycleExpand(fields[1:], values)
			if !ok || len(initrd) == 0 {
				return false
			}
		default:
			return false
		}
	}
	return false
}

func lifecycleLabeledRecovery(lines []string) bool {
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) == 0 || strings.HasPrefix(fields[0], "#") {
			continue
		}
		return len(fields) == 1 && len(fields[0]) > 1 && strings.HasPrefix(fields[0], ":")
	}
	return true
}

func lifecycleExpand(fields []string, values map[string][]string) ([]string, bool) {
	out := make([]string, 0, len(fields))
	for _, field := range fields {
		if strings.HasPrefix(field, "${") && strings.HasSuffix(field, "}") {
			value, ok := values[field[2:len(field)-1]]
			if !ok {
				return nil, false
			}
			out = append(out, value...)
		} else {
			if strings.ContainsAny(field, "${}") {
				return nil, false
			}
			out = append(out, field)
		}
	}
	return out, true
}

func lifecycleCacheMetric(text string) (float64, error) {
	return lifecycleMetric(text, "boot_service_declarative_cache_hits_total")
}

func lifecycleMembershipTTL(text string) (float64, error) {
	value, err := lifecycleMetric(text, "boot_service_declarative_membership_cache_ttl_seconds")
	if err != nil || value > 1 {
		return 0, errors.New("invalid membership cache TTL")
	}
	return value, nil
}

func lifecycleMetric(text, name string) (float64, error) {
	var value float64
	found := false
	for _, line := range strings.Split(text, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 || fields[0] != name {
			continue
		}
		if found || len(fields) != 2 {
			return 0, errors.New("invalid aggregate cache metric")
		}
		parsed, err := strconv.ParseFloat(fields[1], 64)
		if err != nil || math.IsNaN(parsed) || math.IsInf(parsed, 0) || parsed < 0 {
			return 0, errors.New("invalid aggregate cache metric")
		}
		value, found = parsed, true
	}
	if !found {
		return 0, errors.New("aggregate cache metric unavailable")
	}
	return value, nil
}

func lifecycleHasBootIF(script, mac string) bool {
	want, err := net.ParseMAC(mac)
	if err != nil || len(want) != 6 {
		return false
	}
	var found net.HardwareAddr
	for _, line := range strings.Split(script, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 || strings.HasPrefix(fields[0], "#") {
			continue
		}
		if fields[0] == "boot" {
			break
		}
		for _, field := range fields {
			value, ok := strings.CutPrefix(field, "BOOTIF=")
			if !ok {
				continue
			}
			if found != nil {
				return false
			}
			prefix, address, ok := strings.Cut(value, "-")
			if !ok || prefix != "01" {
				return false
			}
			found, err = net.ParseMAC(address)
			if err != nil || len(found) != 6 {
				return false
			}
		}
	}
	return found != nil && bytes.Equal(found, want)
}
