// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestInitialIdentityActionProcessUmasks(t *testing.T) {
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(base, "bin")
	if err := os.Mkdir(bin, 0700); err != nil {
		t.Fatal(err)
	}
	control := filepath.Join(bin, "control")
	buildQuiesceTestBinary(t, control, ".")
	buildQuiesceTestBinary(t, filepath.Join(bin, "docker"), "./testdata/quiesce-fakedocker")
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, mask := range []string{"022", "077"} {
		t.Run(mask, func(t *testing.T) {
			root := filepath.Join(base, mask)
			run := filepath.Join(root, "logs", "run")
			dir := filepath.Join(run, "config", "boot")
			composeDir := filepath.Join(root, "compose")
			for _, p := range []string{dir, composeDir} {
				if err := os.MkdirAll(p, 0700); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.Chmod(run, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(dir, 0755); err != nil {
				t.Fatal(err)
			}
			compose, receipts, lock := filepath.Join(composeDir, "test.yaml"), filepath.Join(run, "receipts.jsonl"), filepath.Join(run, "quiescence.lock")
			for _, p := range []string{compose, receipts, lock} {
				if err := os.WriteFile(p, nil, 0600); err != nil {
					t.Fatal(err)
				}
			}
			issuer := &initialIssuer{}
			server := httptest.NewServer(issuer)
			defer server.Close()
			env := append(os.Environ(), "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"), "QUIESCE_FAKE_DOCKER_URL="+server.URL, "IDENTITY_UMASK_CONTROL="+control, "IDENTITY_UMASK="+mask, "PROJECT=sandbox", "SBX_DECLARATIVE_COMPOSE_PROJECT=sandbox", "SBX_DECLARATIVE_COMPOSE_FILE="+compose, "SBX_DECLARATIVE_RUN_DIR="+run, "SBX_DECLARATIVE_CONTROL_RECEIPTS="+receipts, "SBX_DECLARATIVE_BOOT_IMAGE_ID=sha256:"+strings.Repeat("a", 64), "SBX_DECLARATIVE_BOOT_CONFIG_DIR="+dir, "SBX_DECLARATIVE_HSM_BOOTSTRAP_REDACTIONS="+filepath.Join(run, "credentials.redactions"), "SBX_DECLARATIVE_TOKENSMITH_STORE=/tokensmith/data/bootstrap-tokens", "SBX_DECLARATIVE_TOKENSMITH_CONTAINER=", "SBX_DECLARATIVE_QUIESCENCE_LOCK="+lock, "SBX_DECLARATIVE_BOOT_URL=")
			for attempt := 0; attempt < 3; attempt++ {
				ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
				cmd := exec.CommandContext(ctx, self, "-test.run=^TestIdentityUmaskProcess$")
				cmd.Env = env
				var out, stderr bytes.Buffer
				cmd.Stdout, cmd.Stderr = &out, &stderr
				err := cmd.Run()
				cancel()
				if attempt < 2 {
					if err != nil || out.String() != "{}\n" || stderr.Len() != 0 {
						t.Fatal("initial or fresh identity action failed")
					}
				} else if err == nil || out.Len() != 0 || stderr.String() != "declarative-control: operation failed\n" {
					t.Fatal("duplicate action did not fail safely")
				}
			}
			sum := sha256.Sum256([]byte("process-secret-one"))
			auditPath := filepath.Join(run, "credential-issuance.jsonl")
			audit, err := os.ReadFile(auditPath)
			if err != nil {
				t.Fatal(err)
			}
			lines := bytes.Split(bytes.TrimSpace(audit), []byte("\n"))
			var first struct {
				Digest string `json:"tokenSHA256"`
			}
			if len(lines) != 2 || json.Unmarshal(lines[0], &first) != nil || first.Digest != "sha256:"+hex.EncodeToString(sum[:]) || bytes.Contains(audit, []byte("process-secret")) {
				t.Fatal("invalid initial audit")
			}
			for _, p := range []string{auditPath, filepath.Join(run, "credentials.redactions")} {
				info, err := os.Stat(p)
				if err != nil || info.Mode().Perm() != 0600 {
					t.Fatal("private file mode incorrect")
				}
			}
			info, err := os.Stat(filepath.Join(dir, "boot.json"))
			if err != nil || info.Mode().Perm() != 0444 {
				t.Fatal("config mode incorrect")
			}
			ledger, err := os.ReadFile(filepath.Join(run, "credentials.redactions"))
			if err != nil || bytes.Count(ledger, []byte("process-secret-one\n")) != 2 || bytes.Count(ledger, []byte("process-secret-two\n")) != 1 {
				t.Fatal("issued tokens missing from redaction ledger")
			}
			info, err = os.Stat(receipts)
			if err != nil || info.Size() != 0 {
				t.Fatal("initial action wrote control receipt")
			}
			issuer.mu.Lock()
			issuer.fail = true
			issuer.mu.Unlock()
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			cmd := exec.CommandContext(ctx, self, "-test.run=^TestIdentityUmaskProcess$")
			cmd.Env = env
			var out, stderr bytes.Buffer
			cmd.Stdout, cmd.Stderr = &out, &stderr
			err = cmd.Run()
			cancel()
			if err == nil || out.Len() != 0 || stderr.String() != "declarative-control: operation failed\n" {
				t.Fatal("issuer stderr was not suppressed")
			}
			if _, err := os.Stat(filepath.Join(run, identityIncomplete)); err != nil {
				t.Fatal("issuer failure did not leave redaction fence")
			}
		})
	}
}
