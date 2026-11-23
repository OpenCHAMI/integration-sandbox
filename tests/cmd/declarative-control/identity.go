// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package main

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"time"
)

var errIdentity = errors.New("test identity provisioning failed")

func (c controller) prepareBootIdentity(ctx context.Context) error {
	files, err := c.openIdentityFiles(ctx)
	if err != nil {
		return errIdentity
	}
	defer files.Close()
	if os.Getenv("SBX_DECLARATIVE_TOKENSMITH_STORE") != "/tokensmith/data/bootstrap-tokens" {
		return errIdentity
	}
	backend, err := c.locate(ctx, "tokensmith-backend")
	if err != nil || !backend.State.Running {
		return errIdentity
	}
	if expected := os.Getenv("SBX_DECLARATIVE_TOKENSMITH_CONTAINER"); expected != "" && expected != backend.ID {
		return errIdentity
	}
	fence, err := c.beginRedactionFence()
	if err != nil {
		return errIdentity
	}
	defer fence.Close()
	output, err := c.exec.run(ctx, "exec", backend.ID, "tokensmith", "bootstrap-token", "create", "--bootstrap-store=/tokensmith/data/bootstrap-tokens", "--subject=acceptance-hsm", "--audience=hsm", "--scopes=read:smd", "--ttl=1h", "--output-format=json")
	if err != nil || len(output) > 16384 {
		return errIdentity
	}
	var response struct {
		Token string `json:"bootstrap_token"`
	}
	if !uniqueJSON(output) || json.Unmarshal(output, &response) != nil || response.Token == "" || len(response.Token) > 8192 || strings.ContainsAny(response.Token, " \t\r\n\x00") {
		return errIdentity
	}
	for _, b := range []byte(response.Token) {
		if b < 33 || b > 126 {
			return errIdentity
		}
	}
	if _, err := files.redactions.WriteString(response.Token + "\n"); err != nil {
		return errIdentity
	}
	if err := files.redactions.Sync(); err != nil {
		return errIdentity
	}
	if err := fence.protected(); err != nil {
		return errIdentity
	}
	sum := sha256.Sum256([]byte(response.Token))
	digest := "sha256:" + hex.EncodeToString(sum[:])
	if files.previousToken == response.Token {
		return errIdentity
	}
	if _, err := files.audit.Seek(0, io.SeekStart); err != nil {
		return errIdentity
	}
	scanner := bufio.NewScanner(io.LimitReader(files.audit, 16<<20))
	for scanner.Scan() {
		var line struct {
			Digest string `json:"tokenSHA256"`
		}
		if json.Unmarshal(scanner.Bytes(), &line) != nil || line.Digest == "" || line.Digest == digest {
			return errIdentity
		}
	}
	if scanner.Err() != nil {
		return errIdentity
	}
	audit := struct {
		Digest   string    `json:"tokenSHA256"`
		Subject  string    `json:"subject"`
		Audience string    `json:"audience"`
		Scopes   []string  `json:"scopes"`
		Time     time.Time `json:"time"`
	}{digest, "acceptance-hsm", "hsm", []string{"read:smd"}, time.Now().UTC()}
	line, err := json.Marshal(audit)
	if err != nil {
		return errIdentity
	}
	if _, err := files.audit.Write(append(line, '\n')); err != nil {
		return errIdentity
	}
	if err := files.audit.Sync(); err != nil {
		return errIdentity
	}
	return files.publish(response.Token)
}
