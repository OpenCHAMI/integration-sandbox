# OpenCHAMI integration sandbox

[![REUSE compliant](https://api.reuse.software/badge/github.com/OpenCHAMI/integration-sandbox)](https://api.reuse.software/info/github.com/OpenCHAMI/integration-sandbox)
[![CI](https://github.com/OpenCHAMI/integration-sandbox/actions/workflows/ci.yml/badge.svg)](https://github.com/OpenCHAMI/integration-sandbox/actions/workflows/ci.yml)

A docker-compose-driven, BMC-less, end-to-end test harness for the
OpenCHAMI microservices. Single-command run, idempotent, no real hardware
required. Designed to be invoked from any service repository's CI to
cross-check a PR build against the rest of the OpenCHAMI fleet.

```bash
make ci          # build/pull → up → seed → bats → integration → down
make tail        # phone-friendly progress tail
make show-images # print the image set the next ci run will use
```

Live state during a run: `cat STATUS` and `tail PROGRESS.log`.

## What this is — and what it isn't

It **is** a single command that stands up the OpenCHAMI service stack
(SMD, tokensmith, boot-service, metadata-service, fru-tracker,
power-control, magellan) on top of simulated infrastructure (Vault dev,
LocalStack S3, PostgreSQL, eight Redfish BMC emulators, one IPMI sim) and
runs three end-to-end use cases against it. No real hardware, no
Kubernetes, no operator.

## Documentation

Read in order:

1. [`docs/quickstart.md`](docs/quickstart.md) — five-minute walkthrough.
2. [`docs/architecture.md`](docs/architecture.md) — what's in the box and how the layers fit.
3. [`docs/use-cases.md`](docs/use-cases.md) — what each `make uc<N>` validates.
4. [`docs/configuration.md`](docs/configuration.md) — env vars, image manifests, fixtures, ports, credentials, xnames.
5. [`docs/operations.md`](docs/operations.md) — every make target, every script, heartbeat, log bundles.
6. [`docs/troubleshooting.md`](docs/troubleshooting.md) — every failure mode hit during scaffolding, with the fix.
7. [`docs/extending.md`](docs/extending.md) — adding a service, a fixture, a test, a manifest.
8. [`docs/ci-integration.md`](docs/ci-integration.md) — GitHub Actions, PR-build overrides, release matrix, caching.
9. [`docs/known-issues.md`](docs/known-issues.md) — harness limitations and open work.

Reference cards:

- [`docs/reference-endpoints.md`](docs/reference-endpoints.md)
- [`docs/reference-helpers.md`](docs/reference-helpers.md)
- [`docs/reference-fixtures.md`](docs/reference-fixtures.md)

## At a glance

**Stack.** Three docker-compose layers on one network (`openchami-sandbox`):

- `compose/infra.yaml` — Vault dev + LocalStack + Postgres.
- `compose/bmc-sim.yaml` — 8 csm-rie Redfish emulators with hostname
  aliases `x0c0s0b0`…`x0c0s7b0`, plus one ipmi_sim.
- `compose/core.yaml` — SMD, tokensmith, boot-service, metadata-service,
  fru-tracker, power-control, magellan-runner.

**Tests.** A standalone Go module under `tests/` (build tag `integration`)
plus a thin `bats` smoke layer. Seven named cross-service use cases:

| UC | Services exercised | What it asserts | Status |
|----|-------------------|-----------------|--------|
| [UC1](docs/use-cases/uc1-node-visibility.md) | SMD, boot-service, metadata-service | Populate SMD with nodes; node visibility chain through boot/metadata, with cloud-init lookup. | [![UC1](https://github.com/OpenCHAMI/integration-sandbox/actions/workflows/ci.yml/badge.svg?event=push)](https://github.com/OpenCHAMI/integration-sandbox/actions/workflows/ci.yml) |
| [UC2](docs/use-cases/uc2-multi-cluster.md) | boot-service, metadata-service | Two clusters with disjoint node sets; move a node between them; membership reflects on both services. | [![UC2](https://github.com/OpenCHAMI/integration-sandbox/actions/workflows/ci.yml/badge.svg?event=push)](https://github.com/OpenCHAMI/integration-sandbox/actions/workflows/ci.yml) |
| [UC3](docs/use-cases/uc3-restart-resilience.md) | All 9 services | Restart each container in turn (k8s-style); re-confirm node visibility and cross-service health. | [![UC3](https://github.com/OpenCHAMI/integration-sandbox/actions/workflows/ci.yml/badge.svg?event=push)](https://github.com/OpenCHAMI/integration-sandbox/actions/workflows/ci.yml) |
| [UC4](docs/use-cases/uc4-tokensmith-smd.md) | tokensmith, SMD | RFC 8693 bootstrap-token mint + token exchange; JWKS signature verification; authenticated SMD write round-trips through postgres. | [![UC4](https://github.com/OpenCHAMI/integration-sandbox/actions/workflows/ci.yml/badge.svg?event=push)](https://github.com/OpenCHAMI/integration-sandbox/actions/workflows/ci.yml) |
| [UC5](docs/use-cases/uc5-magellan-scan.md) | magellan, csm-rie, SMD | Run the documented `magellan scan → collect → send` pipeline against the 8 BMC sims; verify SMD's `/Inventory/RedfishEndpoints` reflects the discovered fleet. | [![UC5](https://github.com/OpenCHAMI/integration-sandbox/actions/workflows/ci.yml/badge.svg?event=push)](https://github.com/OpenCHAMI/integration-sandbox/actions/workflows/ci.yml) |
| [UC6](docs/use-cases/uc6-power-redfish.md) | power-control, SMD, csm-rie | POST a `force-off`/`on` transition; observe `PowerState` mutation on the BMC sim via an independent Redfish read; reverse and re-verify. | [![UC6](https://github.com/OpenCHAMI/integration-sandbox/actions/workflows/ci.yml/badge.svg?event=push)](https://github.com/OpenCHAMI/integration-sandbox/actions/workflows/ci.yml) |
| [UC7](docs/use-cases/uc7-fru-discovery.md) | fru-tracker | POST a 32-device discovery snapshot (8 nodes × CPU + 2 DIMMs); poll until reconciler completes; verify parent/child UID linkage in the device tree. | [![UC7](https://github.com/OpenCHAMI/integration-sandbox/actions/workflows/ci.yml/badge.svg?event=push)](https://github.com/OpenCHAMI/integration-sandbox/actions/workflows/ci.yml) |

📄 **[Detailed use case documentation →](docs/use-cases/)**

**Stub-resistance.** Every UC is built so a wiremock or canned-response
stub would fail it. UC4 uses cryptographic JWT verification + a stateful
SMD POST→GET. UC5 requires real `magellan` to scan real `csm-rie` sims.
UC6 reads the BMC sim directly to confirm the side effect actually
landed at the BMC. UC7 reads back persistent state the reconciler must
have computed (UID generation + parent resolution).

**What this does *not* test.** A short list of gaps that exist on purpose:
- No real OIDC IdP — UC4 uses tokensmith's bootstrap-token + JWKS path.
- No real BMCs — `csm-rie` only; SOL flow has its own `ipmi_sim` test in UC3.
- No `magellan` ↔ `power-control` direct path — they share SMD as the
  ground truth and are exercised independently in UC5/UC6.
- No fru-tracker → SMD propagation in UC7 — fru-tracker is a one-way
  sink today (writes its own SQLite store via Ent). If/when fru-tracker
  grows an SMD writer, extend UC7 to cover it.
- No legendary-funicular log-lake validation — out of sandbox scope.

**Image versions.** `make ci` defaults to `IMAGES=release`, which pins each
OpenCHAMI service to its **latest GitHub Release tag** (regenerate with
`make refresh-releases`). Other manifests:

- `IMAGES=default` — floating `:latest` tags (pre-release sniff tests).
- `IMAGES=edge` — `:main` builds (freshest, less stable).
- `IMAGES=release-v1.0` — pinned snapshot of a specific release train.

Override per-service with `SBX_<NAME>_IMAGE=…` — that's the hook a service
repo's PR pipeline uses to test its own build against everything else.
Details in [`docs/configuration.md`](docs/configuration.md).

## Use from a service repo's CI

Drop a workflow like the one at
[`.github/workflows/sandbox-consumer.example.yaml`](.github/workflows/sandbox-consumer.example.yaml)
into the consumer repo, set the relevant `SBX_<NAME>_IMAGE` to your PR
build (`ghcr.io/openchami/<svc>:pr-${{ github.event.pull_request.number }}`),
and the harness checks your PR against the rest of the fleet. See
[`docs/ci-integration.md`](docs/ci-integration.md) for full examples.

## GitHub Actions workflow

The CI workflow (`.github/workflows/ci.yml`) runs automatically on:
- Every push to `main`
- Every pull request
- Daily at 06:00 UTC (drift detection against floating `:main` tags)
- Manual workflow dispatch

**Manual runs** expose UI inputs for:
- `SKIP_SIM` — skip building/starting ipmi-sim and remote-console (default: `true`)
- `IMAGES` — manifest selection (`release` | `default` | `edge` | `release-v1.0`)
- `SBX_*_IMAGE` — override any of the 12 service images

Each use case runs as a **separate, traceable job** with:
- Independent artifact upload (logs retained for 30 days)
- Job summary with pass/fail status and links to documentation
- Sequential execution to avoid accidental interference

**Local testing with act:**
```bash
# 1) Install act once (https://github.com/nektos/act)
brew install act           # macOS
# or: curl -fsSL https://raw.githubusercontent.com/nektos/act/master/install.sh | sudo bash

# 2) Build the runner image once (provides docker-compose, sudo, runner user)
docker build -t integration-sandbox-act:latest -f Dockerfile.act .

# 3) Create your local secrets file (gitignored)
cp .secrets.example .secrets    # placeholder GITHUB_TOKEN is fine for public images

# 4) Run
act --list                                # show all jobs without running
act -j test-uc1 --secret-file .secrets    # run one UC job in isolation first
act push --secret-file .secrets           # full CI on push event
act pull_request --secret-file .secrets   # PR-style trigger
```

`.actrc` in the repo root binds `ubuntu-latest` to the runner image and
shares the host Docker socket so the workflow's `docker compose` calls
land on the same daemon (no DinD).

See [`docs/MAKE_TO_ACTIONS_MAPPING.md`](docs/MAKE_TO_ACTIONS_MAPPING.md) for the complete mapping from `make ci` to the new workflow.

## Donts (read before extending)

- **No edits to other repos.** If you find a bug while extending the
  sandbox, file it in that service's issue tracker. The sandbox is the
  cross-cut harness; per-repo fixes belong upstream.
- **No `kind`/`kubectl`/`helm` calls.** This is the compose harness.
  Kubernetes-side coverage is the operator's e2e suite.
- **No `HEALTHCHECK:` directives on distroless services.** They will
  always fail (no shell, no curl, no wget). Use
  `scripts/wait-for-stack.sh` from the host instead.
- **No new ports, credentials, or xnames** without updating both
  `.claude/CLAUDE.md` and the relevant `docs/` reference card.

The full list lives in [`docs/extending.md`](docs/extending.md).

## Contributing

Issues and PRs welcome. If you're working on cross-service flows the
harness doesn't cover today, please open an issue describing the use case
before sending a PR — most extensions involve fixture changes that ripple
across multiple files.

The repository follows the [REUSE](https://reuse.software/) specification
for licensing metadata. Run `reuse lint` before sending a change.
Pre-commit hooks (`.pre-commit-config.yaml`) catch this and a few other
hygiene checks; install with `pip install pre-commit && pre-commit install`.

## License

MIT — see [`LICENSE`](LICENSE).
