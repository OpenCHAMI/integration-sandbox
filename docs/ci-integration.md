# CI integration

How to drive the sandbox from GitHub Actions (or any CI), with examples for the patterns we care about.

## The shape

```yaml
- run: make ci
  working-directory: integration-sandbox
  env:
    IMAGES: edge                                   # optional manifest selector
    SBX_TOKENSMITH_IMAGE: ghcr.io/openchami/...    # optional per-service override
```

That's it. `make ci` is fully self-contained: build → up → seed → bats → integration → down. Failure writes a forensic bundle to `integration-sandbox/logs/` which you upload as an artifact.

## Reference workflow

A copy-pasteable starting point lives at `.github-workflow.example.yaml`. Drop it into a consumer repo as `.github/workflows/sandbox.yml`.

## Patterns

### 1. PR-build under test (single service)

The most common case: a PR in (say) tokensmith should run the sandbox with the PR's image substituted in.

```yaml
on: pull_request

jobs:
  sandbox:
    runs-on: ubuntu-24.04
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: "1.24.6"
      - run: sudo apt-get update && sudo apt-get install -y bats
      - uses: docker/login-action@v3
        with:
          registry: ghcr.io
          username: ${{ github.actor }}
          password: ${{ secrets.GITHUB_TOKEN }}
      - run: make ci
        working-directory: integration-sandbox
        env:
          SBX_TOKENSMITH_IMAGE: ghcr.io/openchami/tokensmith:pr-${{ github.event.pull_request.number }}
      - if: failure()
        uses: actions/upload-artifact@v4
        with:
          name: sandbox-logs
          path: integration-sandbox/logs/
```

### 2. Complementary PRs (two services together)

Two PRs that depend on each other. Either pin both via env, or pass them as workflow inputs.

```yaml
on:
  workflow_dispatch:
    inputs:
      tokensmith_pr:
        description: "tokensmith PR number"
        type: string
      boot_pr:
        description: "boot-service PR number"
        type: string

jobs:
  sandbox:
    # …
    steps:
      - run: make ci
        working-directory: integration-sandbox
        env:
          SBX_TOKENSMITH_IMAGE: ghcr.io/openchami/tokensmith:pr-${{ inputs.tokensmith_pr }}
          SBX_BOOT_IMAGE: ghcr.io/openchami/boot-service:pr-${{ inputs.boot_pr }}
```

### 3. Release matrix

Pin every service to a release tag.

```yaml
- run: make ci IMAGES=release-v1.0
  working-directory: integration-sandbox
```

### 4. Nightly edge

Track the latest published builds across the fleet.

```yaml
on:
  schedule:
    - cron: "0 5 * * *"    # 05:00 UTC daily

jobs:
  sandbox-edge:
    # …
    - run: make ci IMAGES=edge
      working-directory: integration-sandbox
```

### 5. Use-case targeted runs

Per-job parallelism for faster feedback.

```yaml
strategy:
  matrix:
    uc: [uc1, uc2, uc3]
steps:
  - run: |
      make up
      make seed
      make ${{ matrix.uc }}
    working-directory: integration-sandbox
  - run: make down
    working-directory: integration-sandbox
    if: always()
```

## What CI needs from the runner

| Resource | Floor | Recommended |
|---|---|---|
| RAM | 8 GiB | 16+ GiB (every emulator + service runs concurrently) |
| Disk | 15 GiB free | 30+ GiB (image cache grows) |
| CPU | 2 vCPU | 4 vCPU (compose can build images in parallel) |
| Outbound network | ghcr.io, docker.io, hashicorp.com | same |

`ubuntu-24.04` GitHub-hosted runners (4 vCPU, 16 GiB) are the cheapest tier that consistently fits.

## Caching

Image pull is the slowest step on a cold runner. Two reasonable approaches:

### Option A — registry cache (preferred)
GHCR is fast enough that no host-side cache is needed for `:main` / `:latest`. For pinned tags, the docker daemon's content-addressable layer cache hits on every re-run within the same job.

### Option B — buildx cache action for the locally-built images
`power-control` and `ipmi-sim` are built locally. To cache them across runs,
check out the upstream source on the runner first, then prime the layer
cache before invoking the sandbox:
```yaml
- name: Check out power-control
  uses: actions/checkout@v4
  with:
    repository: OpenCHAMI/power-control
    path: power-control
- uses: docker/setup-buildx-action@v3
- uses: docker/build-push-action@v5
  with:
    context: power-control
    file: power-control/Dockerfile.build
    tags: ghcr.io/openchami/power-control:latest
    cache-from: type=gha
    cache-to: type=gha,mode=max
    load: true
- run: SBX_POWER_CONTROL_SRC=$PWD/power-control make -C integration-sandbox ci
```
`build-images.sh` only invokes `docker build` when `docker image inspect`
fails, so a primed image is reused as-is.

## Log artifacts

On failure, `make ci` leaves `integration-sandbox/logs/<UTC>-ci-failure/`. Upload it:
```yaml
- if: failure()
  uses: actions/upload-artifact@v4
  with:
    name: sandbox-logs-${{ github.run_id }}
    path: integration-sandbox/logs/
    retention-days: 14
```

Inside the bundle:
- `state.txt` — `docker ps`, `df -h`, `free -h`, `docker stats`.
- `log-<container>.txt` — last 500 lines per container.

## Required secrets

None for default usage. The sandbox uses public images and self-contained credentials. Override only if you point at a private registry.

## Anti-patterns to avoid in CI

- **Don't `set -e` and expect partial success.** `make ci` returns non-zero on any failure; use `if: failure()` for cleanup.
- **Don't run `make up` in one step and `make test` in another without `if: always()` on `make down`.** A failed step leaves containers running and blocks subsequent runs on the same self-hosted runner.
- **Don't expose host ports above 1024 from a self-hosted runner without a network policy.** The sandbox binds to 127.0.0.1 by default, but if you remove that, multiple runs can collide.
- **Don't share the docker daemon across PR runs.** Two runs of UC3 against the same daemon will fight over container names. Use isolated runners or job-scoped containers.
