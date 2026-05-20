# integration-sandbox — agent context

This directory is the **OpenCHAMI integration test sandbox**. A future agent session opening this dir should know:

## What it is
A docker-compose-driven, BMC-less, end-to-end test harness for the
OpenCHAMI microservices. It does **not** build the operator and does
**not** require kind.

It pulls published images from `ghcr.io/openchami/*` for everything that
has a release. The two services that don't (yet) — `power-control` and
the `ipmi_sim` simulator — are built from upstream source. Point at a
local checkout via `SBX_POWER_CONTROL_SRC` / `SBX_REMOTE_CONSOLE_SRC`,
or let `scripts/build-images.sh` clone them on demand.

## Single entrypoint
```
make ci
```
Everything else (`make build-images`, `make up`, `make seed`, `make test`, `make down`) is just a slice of that.

## What's where
- `compose/infra.yaml` — vault dev + localstack S3 + postgres
- `compose/bmc-sim.yaml` — 8 Redfish emulators (csm-rie) + 1 ipmi_sim, with hostname aliases like `x0c0s0b0`
- `compose/core.yaml` — smd, tokensmith, boot, metadata, fru-tracker, power-control, magellan-runner
- `fixtures/` — vault-seed.sh, s3-buckets.sh, smd-components.json, boot-configs.json, inventory-snapshot.json
- `scripts/` — build-images.sh, up.sh, down.sh, wait-for-stack.sh, log-bundle.sh, heartbeat.sh
- `tests/integration/` — Go suite (`//go:build integration`)
- `tests/bats/` — CLI smoke
- `PROGRESS.log` / `STATUS` — append-only heartbeat for human/phone tailing
- `logs/<UTC>/` — per-failure log bundles

## Conventions (don't reinvent)
- 8 fake nodes, xnames `x0c0s0b0` … `x0c0s7b0`.
- BMC creds everywhere: `root` / `root_password` (matches both emulator hardcoded defaults).
- Vault root token: `dev-root-token` (matches operator's `hack/local-dev/seed-vault.sh`).
- LocalStack S3: `test`/`test`, region `us-east-1`, buckets `boot-images`, `openchami-logs`, `parquet`.
- Image tag policy: `make ci` defaults to `IMAGES=release` (`images/release.env`,
  each OpenCHAMI service pinned to its latest GitHub Release tag). Refresh
  with `make refresh-releases`. Use `IMAGES=default` for floating `:latest`,
  `IMAGES=edge` for `:main` builds, or `SBX_<NAME>_IMAGE=…` to override one
  service. Only build locally if a pull fails.

## Hard rules for any agent working here
1. **The sandbox is the cross-cut harness — fixes belong upstream.** If
   you find a bug in a service, file an issue in that service's repo
   (e.g. `OpenCHAMI/boot-service`). Don't reach across into a checkout
   you happen to have on disk and "fix while you're there"; that hides
   the change from the upstream community.
2. **No kind, no operator, no Kubernetes.** The operator's e2e is a
   separate concern; see `docs/relationship-to-operator.md`.
3. **Idempotency**: `make ci` must be runnable twice back-to-back and
   stay green. Every script must check-then-create.
4. **Failure protocol**: if a service refuses to come healthy, write a
   log bundle to `logs/<UTC>/`, append a one-liner to `PROGRESS.log`,
   write the *root cause hypothesis* to `BLOCKED.md`, and stop. Do not
   thrash.
5. **No destructive shortcuts.** `down -v` only inside the trap of the
   script that owned the matching `up`.
6. **Pre-approved tools** are listed in `.claude/settings.json`; if you
   need something outside that list (e.g. `git`), ask.
