# OpenCHAMI integration sandbox

[![REUSE compliant](https://api.reuse.software/badge/github.com/OpenCHAMI/integration-sandbox)](https://api.reuse.software/info/github.com/OpenCHAMI/integration-sandbox)

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

| UC | Services exercised | What it asserts |
|---|---|---|
| UC1 | SMD, boot-service, metadata-service | Populate SMD with nodes; node visibility chain through boot/metadata, with cloud-init lookup. |
| UC2 | boot-service, metadata-service | Two clusters with disjoint node sets; move a node between them; membership reflects on both services. |
| UC3 | All 9 services | Restart each container in turn (k8s-style); re-confirm node visibility and cross-service health. |
| UC4 | tokensmith, SMD | RFC 8693 bootstrap-token mint + token exchange; JWKS signature verification; authenticated SMD write round-trips through postgres. |
| UC5 | magellan, csm-rie, SMD | Run the documented `magellan scan → collect → send` pipeline against the 8 BMC sims; verify SMD's `/Inventory/RedfishEndpoints` reflects the discovered fleet. |
| UC6 | power-control, SMD, csm-rie | POST a `force-off`/`on` transition; observe `PowerState` mutation on the BMC sim via an independent Redfish read; reverse and re-verify. |
| UC7 | fru-tracker | POST a 32-device discovery snapshot (8 nodes × CPU + 2 DIMMs); poll until reconciler completes; verify parent/child UID linkage in the device tree. |

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
