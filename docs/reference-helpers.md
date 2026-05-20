# Helper reference

Every helper exposed in `tests/integration/clientutil_test.go`. All are package-internal (the file is `*_test.go`); use them only from other `_test.go` files in the same package.

## HTTP

### `httpJSON(t, method, url, body) (int, []byte)`
Marshal `body` to JSON, send the request, return status + raw response body.
- `body == nil` → no body, no Content-Type set.
- Sets `Content-Type: application/json` when `body != nil`.
- Uses the package-level `httpClient` (10 s timeout, `InsecureSkipVerify` for csm-rie's self-signed cert).
- Calls `t.Fatalf` on any transport error; status is *not* asserted.

### `httpJSONWithHeaders(t, method, url, headers, body) (int, []byte)`
Same as `httpJSON`, plus extra request headers. Use for `X-Forwarded-For` and friends.

### `expectStatus(t, want, got, body, ctx)`
Two-line replacement for the verbose `require.Equalf(t, want, got, "%s: body=%s", ctx, pretty(body))` pattern.

### `waitForHTTP200(t, url, timeout)`
Poll until the URL returns 200, 204, or 401, or timeout. Used after `composeRestart`.

### `waitFor(t, what, timeout, fn)`
Generic poll-until-no-error. `fn` returns `nil` on success or any error. Useful when the success condition is "the response body now contains X."

## boot-service

### `bootNodeUIDsByName(t, bootURL, name) []string`
List `/nodes`, return every uid whose `metadata.name` matches. boot-service does not enforce uniqueness, so this returns a slice (often of length 1, sometimes more after a partial failure).

### `bootResetByName(t, bootURL, name)`
Delete every node entry that shares `name`. Idempotent — safe to call when there are zero matches.

### `bootCreateNode(t, bootURL, xname, mac, nid, groups)`
POST a fresh node. Caller must `bootResetByName` first to guarantee uniqueness. Fails the test on any non-2xx.

### `bootSetGroups(t, bootURL, name, groups)`
Resolve uid → GET current spec → mutate `spec.groups` → PUT back. Asserts exactly one match for `name`. Use this for "move node X to cluster Y" flows.

## docker compose

### `composeRestart(t, service)`
Runs `docker compose -f infra.yaml -f bmc-sim.yaml -f core.yaml restart <service>`. Fails with full stderr on non-zero exit. Used by UC3.

## Misc

### `pretty(b []byte) string`
Truncates a JSON body to 400 chars and replaces newlines with spaces. Use it in `t.Fatalf` messages so a 5-MB Redfish response doesn't drown the test log.

### `errorf(format, ...) error`
Tiny wrapper around `fmt.Errorf` so `waitFor` callers can stay terse.

## Suite-level (in `suite_test.go`)

### `Endpoints map[string]string`
Canonical service-name → URL map. Override individual entries via `SBX_<KEY>_URL` env vars. Tests should always read from `Endpoints[…]` instead of hardcoding URLs.

### `Xnames []string`
The 8-node fake fleet (`x0c0s0b0`–`x0c0s7b0`). Tests use this for fixture iteration.

### `httpClient`
Shared across all tests for connection reuse. 10 s timeout, `TLSClientConfig{InsecureSkipVerify:true}` for csm-rie.

### `envOr(key, default) string`
`os.Getenv(key)` with a non-empty fallback. Used by every `Endpoints` entry.

### `waitFor200(t, url, timeout)`
Older variant of `waitForHTTP200` kept in `suite_test.go` for backward compatibility. Prefer `waitForHTTP200` in new code.

### `cleanupEnabled() bool`
Returns true when `SBX_UC_CLEANUP=1`. Use it as the first line of any `TestX_Cleanup` so default runs leave fixtures in place for inspection.

## Patterns

### Hermetic create
```go
bootResetByName(t, bootURL, xname)
bootCreateNode(t, bootURL, xname, mac, nid, []string{cluster})
```

### Move
```go
bootSetGroups(t, bootURL, xname, []string{newCluster})
```

### Restart-then-verify
```go
composeRestart(t, "smd")
waitForHTTP200(t, Endpoints["smd"]+"/hsm/v2/service/ready", 60*time.Second)
// ... do work ...
waitFor(t, "x visible after restart", 30*time.Second, func() error {
    code, body := httpJSON(t, http.MethodGet, ...)
    if code != 200 { return errorf("HTTP %d", code) }
    if !strings.Contains(string(body), expected) { return errorf("missing") }
    return nil
})
```

### Cleanup gated on env var
```go
func TestX_Cleanup(t *testing.T) {
    if !cleanupEnabled() { t.Skip("set SBX_UC_CLEANUP=1") }
    bootResetByName(t, bootURL, xname)
    // …
}
```
