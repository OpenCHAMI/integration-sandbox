// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"net"
	"os"
)

func (c controller) lifecycle(ctx context.Context, action, service string) error {
	if service == "boot" && (action == "service-start" || action == "service-restart" || action == "service-recreate") {
		return c.withCapture(ctx, func() error { return c.lifecycleWithCapture(ctx, action, service) })
	}
	return c.lifecycleWithCapture(ctx, action, service)
}

func (c controller) lifecycleWithCapture(ctx context.Context, action, service string) error {
	if service != "boot" && service != "smd" && service != "boot-auth-off" {
		return errDocker
	}
	if (action == "service-restart" || action == "service-recreate") && service != "boot" {
		return errDocker
	}
	before, err := c.locate(ctx, service)
	if err != nil {
		return err
	}
	if service == "boot" && (before.Image != c.imageID || c.imageID == "") {
		return errDocker
	}
	if action == "service-start" && before.State.Running {
		return errDocker
	}
	if action == "service-stop" && !before.State.Running {
		return errDocker
	}
	var address string
	if action == "service-restart" || action == "service-recreate" {
		if !before.State.Running {
			return errDocker
		}
		if _, err := c.dataDir(before); err != nil {
			return err
		}
		address, err = c.port(ctx, before.ID)
		if err != nil {
			return err
		}
		endpoint, err := loopbackURL(os.Getenv("SBX_DECLARATIVE_BOOT_URL"))
		if err != nil || endpoint.Host != address {
			return errDocker
		}
		_, port, err := net.SplitHostPort(address)
		if err != nil || (action == "service-recreate" && os.Getenv("SBX_DECLARATIVE_BOOT_HOST_PORT") != port) {
			return errDocker
		}
		if action == "service-recreate" {
			if err := c.configuredPort(ctx, port); err != nil {
				return err
			}
		}
	}
	if service == "boot" && (action == "service-start" || action == "service-restart" || action == "service-recreate") {
		if err := c.prepareIdentityIfRequired(ctx, before); err != nil {
			return err
		}
	}
	args := []string{"compose", "--project-name", c.project, "--file", c.compose}
	switch action {
	case "service-stop":
		args = append(args, "stop", service)
	case "service-start":
		args = append(args, "start", service)
	case "service-restart":
		args = append(args, "restart", service)
	case "service-recreate":
		args = append(args, "up", "--detach", "--no-deps", "--force-recreate", service)
	default:
		return errDocker
	}
	if _, err := c.exec.run(ctx, args...); err != nil {
		return errDocker
	}
	after, err := c.locate(ctx, service)
	if err != nil {
		return err
	}
	if action == "service-stop" {
		if !before.State.Running || after.State.Running || before.ID != after.ID {
			return errDocker
		}
		return nil
	}
	if action == "service-start" {
		if before.State.Running || !after.State.Running || before.ID != after.ID {
			return errDocker
		}
		return waitReady(ctx, service)
	}
	if action == "service-restart" && (before.ID != after.ID || before.State.StartedAt == after.State.StartedAt) {
		return errReceipt
	}
	if action == "service-recreate" && before.ID == after.ID {
		return errReceipt
	}
	if err := c.samePort(ctx, address, after.ID); err != nil {
		return err
	}
	if err := waitReady(ctx, service); err != nil {
		return err
	}
	return c.appendReceipt(action, service, "main", before, after)
}

func (c controller) snapshot(ctx context.Context, instance string) (string, error) {
	service := "boot"
	if instance == "auth-off" {
		service = c.authOffService
	}
	if instance != "main" && instance != "auth-off" || (service != "boot" && service != "boot-auth-off") {
		return "", errDocker
	}
	before, err := c.locate(ctx, service)
	if err != nil || !before.State.Running || before.State.Paused {
		return "", errDocker
	}
	if before.Image != c.imageID || c.imageID == "" {
		return "", errDocker
	}
	var digest string
	err = c.withCapture(ctx, func() error {
		if err := c.stoppedWithCapture(ctx, before, func(dir string) error {
			var err error
			digest, err = digestStore(dir)
			return err
		}); err != nil {
			return err
		}
		after, err := c.inspect(ctx, service, before.ID)
		if err != nil || after.State.Paused {
			return errDocker
		}
		return c.appendReceipt("snapshot-store", service, instance, before, after)
	})
	return digest, err
}
