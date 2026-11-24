// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"encoding/json"
	"os"
	"regexp"
	"strings"
)

var proxyName = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9.-]{0,251}[a-z0-9])?$`)

func adoptionInterrupt(ctx context.Context, c controller, raw json.RawMessage) (json.RawMessage, error) {
	var req struct {
		SourceUID      string `json:"sourceUid"`
		SeriesName     string `json:"seriesName"`
		AssignmentName string `json:"assignmentName"`
	}
	if err := strictRequest(raw, &req); err != nil {
		return nil, err
	}
	if !assetName.MatchString(req.SourceUID) || !proxyName.MatchString(req.SeriesName) || !proxyName.MatchString(req.AssignmentName) {
		return nil, errDocker
	}
	main, err := c.locate(ctx, "boot")
	if err != nil || !main.State.Running || main.Image != c.imageID || c.imageID == "" {
		return nil, errDocker
	}
	facade, err := c.locate(ctx, "tokensmith")
	if err != nil || !facade.State.Running || !validImageID(facade.Image) {
		return nil, errDocker
	}
	image := os.Getenv("SBX_DECLARATIVE_FIXTURE_IMAGE")
	if image == "" || strings.HasPrefix(image, "-") || strings.ContainsAny(image, " \t\r\n") {
		return nil, errDocker
	}
	output, err := c.exec.run(ctx, "image", "inspect", "--format", "{{.Id}}", image)
	imageID := strings.TrimSpace(string(output))
	if err != nil || !validImageID(imageID) || imageID != facade.Image {
		return nil, errDocker
	}
	suffix, err := randomSuffix()
	if err != nil {
		return nil, err
	}
	service := "fault-proxy-" + suffix
	name := c.project + "-" + service
	if err := c.registerAsset(asset{Kind: "container", Name: name, Project: c.project, Service: service}); err != nil {
		return nil, err
	}
	out, err := c.exec.run(ctx, "run", "--detach", "--name", name, "--label", "com.docker.compose.project="+c.project, "--label", "com.docker.compose.service="+service, "--label", "openchami.acceptance.run="+c.project, "--network", c.project+"_default", "--publish", "127.0.0.1::27780", imageID, "fault-proxy", "--listen", ":27780", "--upstream", "http://boot:27790")
	if err != nil {
		return nil, errDocker
	}
	id := strings.TrimSpace(string(out))
	started, err := c.inspect(ctx, service, id)
	if err != nil || !started.State.Running || started.Image != facade.Image {
		return nil, errDocker
	}
	if err := c.registerAsset(asset{Kind: "container", Name: name, Project: c.project, Service: service, ImageID: imageID}); err != nil {
		return nil, err
	}
	base, err := c.endpoint(ctx, id, "27780/tcp", "/__fixtures/health")
	if err != nil {
		return nil, err
	}
	return json.Marshal(struct {
		ProxyURL string `json:"proxyURL"`
	}{base})
}

func init() { registerAction("adoption-interrupt-once", adoptionInterrupt) }
