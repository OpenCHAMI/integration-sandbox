// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package main

import (
	"os"
	"path/filepath"
	"regexp"
)

var projectName = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,62}$`)

func configFromEnv() (controller, error) {
	project := os.Getenv("SBX_DECLARATIVE_COMPOSE_PROJECT")
	if alias := os.Getenv("PROJECT"); alias != "" && alias != project {
		return controller{}, errInput
	}
	c := controller{project: project, compose: os.Getenv("SBX_DECLARATIVE_COMPOSE_FILE"), runDir: os.Getenv("SBX_DECLARATIVE_RUN_DIR"), receipts: os.Getenv("SBX_DECLARATIVE_CONTROL_RECEIPTS"), imageID: os.Getenv("SBX_DECLARATIVE_BOOT_IMAGE_ID"), authOffService: os.Getenv("SBX_DECLARATIVE_AUTH_OFF_SERVICE"), exec: dockerExec{}}
	if c.authOffService == "" {
		c.authOffService = "boot-auth-off"
	}
	if !projectName.MatchString(c.project) || !filepath.IsAbs(c.compose) || !validImageID(c.imageID) || c.authOffService != "boot-auth-off" {
		return controller{}, errInput
	}
	if ownedPath("/", c.compose, false) != nil {
		return controller{}, errPath
	}
	info, err := os.Stat(c.compose)
	if err != nil || !info.Mode().IsRegular() {
		return controller{}, errInput
	}
	logs := filepath.Join(filepath.Dir(filepath.Dir(c.compose)), "logs")
	if c.runDir == logs || ownedPath(logs, c.runDir, false) != nil || ownedPath(c.runDir, c.receipts, false) != nil {
		return controller{}, errPath
	}
	info, err = os.Stat(c.runDir)
	if err != nil || !info.IsDir() {
		return controller{}, errPath
	}
	return c, nil
}
