# Extending the harness

How to add a service, a fixture, or a test without breaking the existing patterns.

## Adding a new OpenCHAMI service to the stack

1. **Compose entry.** Append to `compose/core.yaml`:
   ```yaml
   newsvc:
     image: ${SBX_NEWSVC_IMAGE:-ghcr.io/openchami/newsvc:latest}
     container_name: sandbox-newsvc
     environment:
       NEWSVC_PORT: "27800"
       SMD_URL: "http://smd:27779"
     depends_on:
       smd:
         condition: service_healthy
     ports:
       - "127.0.0.1:27800:27800"
     restart: unless-stopped
     # Add healthcheck only if the image has curl/wget; otherwise rely on wait-for-stack.sh.
   ```
2. **Image manifest.** Add a default to `images/default.env`:
   ```
   SBX_NEWSVC_IMAGE=ghcr.io/openchami/newsvc:latest
   ```
   And optionally to `edge.env` / `release-v1.0.env`.
3. **build-images.sh.** Add the variable to `PUBLIC_IMAGES`. If the image isn't published yet, follow the `power-control` pattern and add a build fallback.
4. **wait-for-stack.sh.** Append the new service's health URL to `ENDPOINTS`.
5. **Suite endpoints.** Add a key to `Endpoints` in `tests/integration/suite_test.go`:
   ```go
   "newsvc": envOr("SBX_NEWSVC_URL", "http://127.0.0.1:27800"),
   ```
6. **Smoke test.** Add to `tests/integration/services_test.go`'s `cases`:
   ```go
   {"newsvc-health", Endpoints["newsvc"] + "/health", []int{200, 204}},
   ```
7. **Docs.** Add the row to `docs/configuration.md` (ports table) and `docs/architecture.md` (service table).
8. **Verify.** `make ci` twice — the second run must still be green.

## Adding a fixture

1. New file under `fixtures/` (`*.json`, `*.sh`, `*.sql`). Naming convention: `<resource>-<purpose>` (e.g. `metadata-default-groups.json`).
2. If it's a script: `set -euo pipefail`, idempotent, log-line per asset created.
3. If it's data: keep it small — fixtures are read at every `make seed`. Big binaries belong in `s3-buckets.sh`'s `awslocal s3 cp`.
4. Wire it into `make seed`:
   ```make
   seed: ## Seed Vault, S3, and SMD with fixtures
   	@bash fixtures/vault-seed.sh
   	@bash fixtures/s3-buckets.sh
   	@bash fixtures/seed-smd.sh
   	@bash fixtures/your-new-seed.sh
   ```
5. Re-run `make seed` twice — second run must produce no errors.
6. Document it in `docs/configuration.md` (fixtures table).

## Adding an integration test

Read [use-cases.md](use-cases.md) before designing — most new tests fit into an existing UC's pattern.

1. New file `tests/integration/<topic>_test.go` with the build tag header:
   ```go
   //go:build integration
   // +build integration

   package integration
   ```
2. Use the helpers in `clientutil_test.go`:
   - `httpJSON(t, method, url, body)` for plain JSON calls.
   - `httpJSONWithHeaders(t, method, url, headers, body)` when you need `X-Forwarded-For` etc.
   - `bootResetByName(t, bootURL, xname)` before any boot-service create — boot-service does not enforce name uniqueness.
   - `bootCreateNode(t, bootURL, xname, mac, nid, groups)` to register a node.
   - `bootSetGroups(t, bootURL, name, groups)` to update membership.
   - `composeRestart(t, service)` + `waitFor(...)` for restart scenarios.
3. Reserve a fresh xname range. UC1 owns `x0c0s0..3`, UC2 owns `x0c0s4..7`, UC3 owns `x9c0s*`. Pick something unused.
4. If your test mutates state, add a `TestX_Cleanup` that runs only when `cleanupEnabled()` is true. This keeps `make uc-all` reentrant for debugging.
5. If it's a use case, add a `make uc<N>` target and append to `uc-all`. Document it in `docs/use-cases.md`.
6. Run twice locally to confirm idempotency:
   ```bash
   make up && make seed && go test -tags integration -run TestX -count=2 ./tests/integration/...
   ```

### Test design rules

- **No imports of OpenCHAMI service Go packages.** Keep `tests/go.mod` independent so the suite works against unrelated PR builds and release versions without recompiling.
- **No assertions on transient ordering.** `boot-service /nodes` returns nodes in storage order — use a map keyed by xname, not a list index.
- **No reliance on cross-restart vault state.** Vault is in-memory; UC3 restarts it. Tests that need vault must accept that the seed survives only until UC3 runs (or seed it themselves).
- **Always assert specific shapes, not just status codes.** A 200 with the wrong body is the most common silent failure.

## Adding a bats test

`tests/bats/cli-smoke.bats` is the smoke layer: read-only, fast, runs first. Use it for:
- "the seed actually applied"
- "the host can talk to the stack"
- "this CLI is on PATH"

Don't use it for:
- "service X behaves correctly under load Y" — that's an integration test.
- Anything that mutates state — see UC3/vault gotcha.

```bats
@test "newsvc /version returns the version string" {
  run curl -s http://127.0.0.1:27800/version
  [ "$status" -eq 0 ]
  echo "$output" | jq -e '.version' >/dev/null
}
```

## Adding a manifest

See [configuration.md](configuration.md) — it's a single env file with a known set of `SBX_*_IMAGE` keys.

## Don't

- **Don't add `kind`, `kubectl`, or `helm` calls.** The sandbox is compose-only by design. The operator's e2e is a separate concern.
- **Don't bind ports to `0.0.0.0`.** Stay on `127.0.0.1` so the sandbox doesn't accidentally serve to the network.
- **Don't import third-party fixtures from sibling repos.** Copy what you need into `fixtures/`. Tight coupling to sibling repos is what the sandbox exists to avoid.
- **Don't edit code in sibling repos from the sandbox.** If you spot a bug, append to that repo's `bugs.md`. Cross-repo edits need human review.
- **Don't add a `HEALTHCHECK` to a distroless service.** It will always fail. Use `wait-for-stack.sh`.
