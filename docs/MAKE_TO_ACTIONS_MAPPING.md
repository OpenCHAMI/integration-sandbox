# Make → GitHub Actions Mapping

This document maps the current `make ci` workflow to the new GitHub Actions-based CI pipeline.

## High-level comparison

| Old (make ci) | New (GitHub Actions) |
|---------------|----------------------|
| Single monolithic job | 15+ explicit, traceable jobs |
| Failures hidden in nested scripts | Each job reports independently |
| Log bundles only on full failure | Per-use-case artifacts (30 days) |
| No UI for overriding images | Workflow dispatch inputs for all variables |
| Simulators always built | `SKIP_SIM` flag (default: true) |
| No per-UC documentation | Auto-generated docs + job summaries |

## Detailed mapping

### Setup phase

| Make target | GitHub Actions job(s) | What changed |
|-------------|----------------------|--------------|
| _(implicit checkout)_ | `setup` | Now explicit with Go caching |
| _(implicit Go install)_ | `setup` → Install Go | Uses `actions/setup-go@v5` with `go.mod` |
| _(implicit Docker)_ | `setup` → Set up Docker Buildx | Explicit BuildKit enablement |
| _(implicit GHCR login)_ | `setup` → Log in to GHCR | Explicit authentication |

### Build phase

| Make target | GitHub Actions job(s) | What changed |
|-------------|----------------------|--------------|
| `make build-images` | `build-images` | Now respects `SKIP_SIM` env var |
| `scripts/build-images.sh` | `build-images` → Build/pull images | Same script, new flag support |

### Infrastructure phase

| Make target | GitHub Actions job(s) | What changed |
|-------------|----------------------|--------------|
| `make up` (infra) | `infra-up` | Split into separate job |
| `docker compose -f compose/infra.yaml up -d` | `infra-up` → Start infra stack | Explicit health check wait |
| _(implicit health check)_ | `infra-up` → Wait for infra health | Uses `scripts/wait-for-stack.sh` |

### BMC simulator phase

| Make target | GitHub Actions job(s) | What changed |
|-------------|----------------------|--------------|
| `make up` (bmc-sim) | `bmc-sim-up` | **Now optional** (gated on `SKIP_SIM`) |
| `docker compose -f compose/bmc-sim.yaml up -d` | `bmc-sim-up` → Start BMC sim stack | Skipped by default |

### Core services phase

| Make target | GitHub Actions job(s) | What changed |
|-------------|----------------------|--------------|
| `make up` (core) | `core-up` | Split into separate job |
| `docker compose -f compose/core.yaml up -d` | `core-up` → Start core stack | Explicit health check wait |
| _(implicit health check)_ | `core-up` → Wait for core services health | Uses `scripts/wait-for-stack.sh` |

### Seed phase

| Make target | GitHub Actions job(s) | What changed |
|-------------|----------------------|--------------|
| `make seed` | `seed` | Separate job with explicit env vars |
| `bash fixtures/vault-seed.sh` | `seed` → Seed Vault | Explicit `VAULT_ADDR` / `VAULT_TOKEN` |
| `bash fixtures/s3-buckets.sh` | `seed` → Seed S3 buckets | Explicit AWS env vars |
| `bash fixtures/seed-smd.sh` | `seed` → Seed SMD | Same script |

### Test phase (BATS)

| Make target | GitHub Actions job(s) | What changed |
|-------------|----------------------|--------------|
| `make test-bats` | `test-bats` | Separate job, runs before integration tests |
| `bats tests/bats/` | `test-bats` → Run BATS tests | Same command |
| _(no artifact upload)_ | `test-bats` → Upload BATS logs on failure | **New**: 30-day retention |

### Test phase (Integration)

| Make target | GitHub Actions job(s) | What changed |
|-------------|----------------------|--------------|
| `make test-integration` | `test-uc1` … `test-uc7` | **Split into 7 sequential jobs** |
| `cd tests && go test -tags integration ...` | Each `test-uc<N>` job | One job per use case |
| _(no per-UC logs)_ | Each job → Upload artifacts | **New**: per-UC log bundles (30 days) |
| _(no per-UC status)_ | Each job → Generate test report | **New**: GitHub job summary with badges |

### Teardown phase

| Make target | GitHub Actions job(s) | What changed |
|-------------|----------------------|--------------|
| `make down` (in trap) | `teardown` | Explicit job, always runs |
| `bash scripts/down.sh` | `teardown` → Stop and remove all containers | Same script |

## Environment variables

### Image selection

| Old | New | Notes |
|-----|-----|-------|
| `IMAGES=release` | `inputs.images` (default: `release`) | Exposed in workflow dispatch UI |
| `SBX_SMD_IMAGE=...` | `inputs.sbx_smd_image` | **New**: all 12 services exposed in UI |
| _(not available)_ | `inputs.skip_sim` | **New**: skip building ipmi-sim / remote-console |

### Infrastructure credentials

| Old | New | Notes |
|-----|-----|-------|
| _(implicit from `.claude/settings.json`)_ | Explicit `env:` in each job | Vault, AWS, etc. |

## Job dependencies (sequential execution)

```
setup
  ↓
build-images
  ↓
infra-up
  ↓
bmc-sim-up (optional, if SKIP_SIM != 'true')
  ↓
core-up
  ↓
seed
  ↓
test-bats
  ↓
test-uc1
  ↓
test-uc2
  ↓
test-uc3
  ↓
test-uc4
  ↓
test-uc5
  ↓
test-uc6
  ↓
test-uc7
  ↓
teardown (always)
```

## Artifact retention

| Old | New | Retention |
|-----|-----|-----------|
| `logs/` (local only) | `bats-logs-*` artifact | 30 days |
| _(no per-UC logs)_ | `uc1-logs-*` … `uc7-logs-*` artifacts | 30 days |
| _(no job summaries)_ | GitHub job summary (markdown) | 90 days (GitHub default) |

## Use case documentation

| Old | New |
|-----|-----|
| _(manual in `docs/use-cases.md`)_ | Auto-generated per-UC markdown in `docs/use-cases/uc<N>-*.md` |
| _(no badges)_ | Live status badges in `docs/use-cases/README.md` |
| _(no CI links)_ | Direct links to workflow runs in each UC doc |

## Local testing

| Old | New |
|-----|-----|
| `make ci` | `make ci` (still works) |
| _(no local CI simulation)_ | `act -j test-uc1` (run a single UC locally) |
| _(no local workflow validation)_ | `act --list` (show all jobs without running) |

## Migration checklist

- [x] Create new workflow file (`.github/workflows/ci.yml`)
- [x] Add `SKIP_SIM` support to `scripts/build-images.sh`
- [x] Create `scripts/gen-usecase-docs.sh`
- [x] Generate initial use case documentation
- [ ] Update main `README.md` with UC badges
- [ ] Update `.github/workflows/ci.yaml` (old file) to point to new workflow
- [ ] Test locally with `act`
- [ ] Run a test workflow dispatch to verify all inputs work
- [ ] Update `docs/ci-integration.md` with new workflow instructions

## Rollback plan

If the new workflow has issues:

1. Revert `.github/workflows/ci.yml` to `.github/workflows/ci.yaml.bak`
2. The old `make ci` flow is unchanged and still works locally
3. Service repos using `sandbox-consumer.example.yaml` are unaffected (they still call `make ci`)

## Future improvements

- [ ] Add job-level caching for Docker layers
- [ ] Parallelize independent UC jobs (requires state isolation)
- [ ] Add self-hosted runner support
- [ ] Auto-commit generated docs on push to main
- [ ] Add Slack/Discord notifications for nightly drift runs
