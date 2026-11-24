// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"strconv"
	"strings"
)

func needsBootIdentity(v container) (bool, error) {
	values := map[string]string{}
	for _, entry := range v.Config.Env {
		key, value, ok := strings.Cut(entry, "=")
		if ok {
			values[key] = value
		}
	}
	auth := values["BOOT_SERVICE_ENABLE_AUTH"]
	if auth == "" {
		auth = values["BOOT_SERVICE_AUTH_ENABLED"]
	}
	hsm := values["BOOT_SERVICE_HSM_URL"]
	tokensmith := values["TOKENSMITH_URL"]
	if tokensmith == "" {
		tokensmith = values["BOOT_SERVICE_TOKENSMITH_URL"]
	}
	config := ""
	secretFlag := ""
	for i := 0; i < len(v.Config.Cmd); i++ {
		arg := v.Config.Cmd[i]
		if !strings.HasPrefix(arg, "--") {
			continue
		}
		name, value, present := strings.Cut(strings.TrimPrefix(arg, "--"), "=")
		switch name {
		case "enable-auth", "auth-enabled", "hsm-url", "tokensmith-url", "config", "tokensmith-bootstrap-token":
			if !present {
				if i+1 < len(v.Config.Cmd) && !strings.HasPrefix(v.Config.Cmd[i+1], "-") {
					i++
					value = v.Config.Cmd[i]
				} else if name == "enable-auth" || name == "auth-enabled" {
					value = "true"
				} else {
					return false, errIdentity
				}
			}
			switch name {
			case "enable-auth", "auth-enabled":
				auth = value
			case "hsm-url":
				hsm = value
			case "tokensmith-url":
				tokensmith = value
			case "config":
				config = value
			case "tokensmith-bootstrap-token":
				secretFlag = value
			}
		}
	}
	if auth == "" {
		if strings.TrimSpace(hsm) != "" && strings.TrimSpace(tokensmith) != "" {
			return false, errIdentity
		}
		return false, nil
	}
	enabled, err := strconv.ParseBool(auth)
	if err != nil {
		return false, errIdentity
	}
	if !enabled || strings.TrimSpace(hsm) == "" || strings.TrimSpace(tokensmith) == "" {
		return false, nil
	}
	if config != "/run/boot-config/boot.json" || secretFlag != "" || values["TOKENSMITH_BOOTSTRAP_TOKEN"] != "" || values["BOOT_SERVICE_TOKENSMITH_BOOTSTRAP_TOKEN"] != "" || !v.identityMountReadOnly {
		return false, errIdentity
	}
	return true, nil
}

func (c controller) prepareIdentityIfRequired(ctx context.Context, v container) error {
	required, err := needsBootIdentity(v)
	if err != nil {
		return err
	}
	if !required {
		return nil
	}
	return c.prepareBootIdentity(ctx)
}
