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
- Declarative lifecycle scenarios reserve `x1c0s0b0n0` … `x1c0s7b0n0`
  with NIDs `10000` … `10007` in their isolated Compose run. Cases run
  serially, use per-case randomized locally administered MACs, require SMD
  absence before creation, and clean up only their own resources. Canonical
  `x0` safety witnesses remain unchanged. Only the three legacy
  characterization scenarios may seed/update a legacy boot-service Node;
  new-provider scenarios remain SMD-only.
- BMC creds everywhere: `root` / `root_password` (matches both emulator hardcoded defaults).
- Vault root token: `dev-root-token` (matches operator's `hack/local-dev/seed-vault.sh`).
- LocalStack S3: `test`/`test`, region `us-east-1`, buckets `boot-images`, `openchami-logs`, `parquet`.
- Declarative acceptance uses a separate Compose project. PostgreSQL uses the
  test-only `openchami`/`openchami` credentials; no BMC credentials are used.
  SMD `27779`, the TokenSmith facade `27780`, and two boot instances `27790`
  use distinct preselected ephemeral loopback host ports, exported before
  Compose creates containers as `SBX_DECLARATIVE_BOOT_HOST_PORT`,
  `SBX_DECLARATIVE_AUTH_OFF_HOST_PORT`, `SBX_DECLARATIVE_SMD_HOST_PORT`, and
  `SBX_DECLARATIVE_TOKENSMITH_HOST_PORT`. Docker's bind is authoritative; the
  preselection is not a reservation, and collisions fail the run.
  Metrics are on the main boot `/metrics`;
  its dedicated internal metrics listener is `27791`. Registry port 5000 is
  untouched. The facade's test signing keys are run-only.
- The host boot client uses `--server` and `BOOT_SERVICE_TOKEN`, never a token
  argument. Required control environment, witness nodes and teardown are in
  `docs/use-cases/uc8-declarative-boot.md`.
- Declarative runs export a run-owned `SBX_DECLARATIVE_QUIESCENCE_LOCK` before
  seeding; its `.inflight` markers must never be automatically cleared. This
  covers cooperating captured-record writers, not global volume atomicity or
  HSM background Node writes.
- Baseline-only failure labels require the exact PR75 image, image revision and
  clean source HEAD, actual `/openapi.json` exact-path absence, a clock anchor
  and unambiguous timestamped request evidence. The three adoption cases may
  include a separately evidenced cleanup 405 on an absent assignment path;
  conflicting requests or missing evidence remain unclassified, never passing.
- The auth-enabled boot instance reads its HSM bootstrap token from the
  read-only `/run/boot-config` directory bind, not a static token environment
  variable. A run-local provisioner must mint a fresh real TokenSmith token for
  every controlled start, including initial start via `prepare-boot-identity`;
  this is not production durable identity. If
  `identity-redaction-incomplete` exists, failure capture is metadata-only.
  Raw config
  and the private redaction ledger are removed after owned processes stop and
  excluded from CI evidence uploads even if cleanup fails.
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
