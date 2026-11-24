#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
: "${SBX_DECLARATIVE_RUN_DIR:?required}"
exec > >(tee -a "$SBX_DECLARATIVE_RUN_DIR/verify-token.log") 2>&1
cat > "$SBX_DECLARATIVE_RUN_DIR/verify-token.go" <<'GO'
package main

import (
 "context"
 "encoding/base64"
 "encoding/json"
 "fmt"
 "os"
 "slices"
 "strings"
 "time"
 db "github.com/openchami/integration-sandbox/tests/declarativeboot"
)

func main() {
 ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
 defer cancel()
 client, err := db.NewHTTPClient(os.Getenv("SBX_DECLARATIVE_TOKENSMITH_URL"), 10*time.Second)
 if err != nil { fmt.Fprintln(os.Stderr, "invalid TokenSmith URL"); os.Exit(1) }
 scopes := []string{"read:boot", "propose:boot", "promote:boot"}
 token, err := db.CreateBootstrapToken(ctx, db.DockerExec, db.BootstrapOptions{Container: os.Getenv("SBX_DECLARATIVE_TOKENSMITH_CONTAINER"), Store: os.Getenv("SBX_DECLARATIVE_TOKENSMITH_STORE"), Subject: "acceptance-preflight", Audience: "boot-service", Scopes: scopes, TTL: "5m"})
 if err != nil { fmt.Fprintln(os.Stderr, "bootstrap creation failed"); os.Exit(1) }
 jwt, err := db.ExchangeToken(ctx, client, token)
 if err != nil { fmt.Fprintln(os.Stderr, "token exchange failed"); os.Exit(1) }
 if err := db.VerifyToken(ctx, client, jwt, db.TokenPolicy{Issuer: "http://tokensmith:27780", Audience: "boot-service", Scopes: scopes}); err != nil { fmt.Fprintln(os.Stderr, "token verification failed"); os.Exit(1) }
 parts := strings.Split(jwt, ".")
 if len(parts) != 3 { fmt.Fprintln(os.Stderr, "invalid JWT structure"); os.Exit(1) }
 payload, err := base64.RawURLEncoding.DecodeString(parts[1])
 if err != nil { fmt.Fprintln(os.Stderr, "invalid JWT payload"); os.Exit(1) }
 var claims struct { Scope json.RawMessage `json:"scope"` }
 if json.Unmarshal(payload, &claims) != nil { fmt.Fprintln(os.Stderr, "invalid JWT claims"); os.Exit(1) }
 var got []string
 if json.Unmarshal(claims.Scope, &got) != nil {
  var text string
  if json.Unmarshal(claims.Scope, &text) != nil { fmt.Fprintln(os.Stderr, "invalid scopes"); os.Exit(1) }
  got = strings.Fields(text)
 }
 slices.Sort(got)
 slices.Sort(scopes)
 if !slices.Equal(got, scopes) { fmt.Fprintln(os.Stderr, "unexpected scope set"); os.Exit(1) }
 fmt.Println("real TokenSmith signature and required claims verified")
}
GO
GOTOOLCHAIN=go1.26.10 go -C "$root/tests" run -tags 'integration declarative_boot' "$SBX_DECLARATIVE_RUN_DIR/verify-token.go"
