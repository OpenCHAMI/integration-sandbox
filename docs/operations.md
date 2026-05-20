# Operations

Day-to-day commands and what they do.

## Make targets

```bash
make help              # auto-generated from target ## comments
```

| Target | Purpose | Reentrant? |
|---|---|---|
| `make ci` | Full pipeline: build → up → seed → bats → integration → down. Trap installs `down` on failure and writes a log bundle. | yes |
| `make build-images` | Pulls every image referenced by the active manifest; falls back to source build for `power-control` and `ipmi-sim`. | yes |
| `make up` | `compose up -d` for infra → bmc-sim → core, with `wait-for-stack.sh` between layers. | yes |
| `make down` | `compose down -v --remove-orphans` for all stacks. Removes the `openchami-sandbox` network. | yes |
| `make seed` | Vault, S3, SMD seeds. Each script idempotent. | yes |
| `make test` | Runs `test-bats` then `test-integration`. Bats first so UC3's vault restart doesn't wipe seeded secrets before the smoke. | yes |
| `make test-bats` | `bats tests/bats/` against the live stack. | yes |
| `make test-integration` | `go test -tags integration -count=1 ./integration/...` | yes |
| `make uc1` / `uc2` / `uc3` | Single use-case suite. | yes |
| `make uc-all` | UC1 + UC2 + UC3 sequentially. | yes |
| `make show-images` | Print the resolved image set for the current `IMAGES` + overrides. Read-only. | yes |
| `make tail` | `tail -F PROGRESS.log` plus current `STATUS`. | n/a |
| `make clean` | `down` + remove `logs/*`. | yes |
| `make reset` | Hard reset: `down`, clear `logs/`, re-pull images. | yes |

## Scripts

All under `scripts/`. Each is `set -euo pipefail` and idempotent.

| Script | Job |
|---|---|
| `heartbeat.sh <status> <msg>` | Append a timestamped milestone to `PROGRESS.log` and overwrite `STATUS`. Used by every other script. |
| `load-images.sh` | Sourced (not executed). Reads the active `images/<name>.env` and exports every key the caller hasn't already set. |
| `build-images.sh` | Pulls each image in the active manifest; on miss, builds locally for power-control / ipmi-sim. |
| `up.sh` | Layered up + health waits. Writes heartbeats per layer. |
| `down.sh` | `down -v --remove-orphans` for every stack file in reverse order, plus an explicit `docker network rm`. |
| `wait-for-stack.sh` | Polls every endpoint in `ENDPOINTS=(name|url …)`. Accepts 200/204/301/302/307/308/401. Fails on first endpoint that exhausts the timeout, after writing a log bundle. |
| `log-bundle.sh [tag]` | Captures forensic state into `logs/<UTC>-<tag>/`: `state.txt` (docker ps/df/free/stats) plus per-container `log-<name>.txt`. |

## Heartbeat

Two files at the sandbox root:
- `PROGRESS.log` — append-only, timestamped, monotonic. Tail this.
- `STATUS` — single line, current state. Read this.

Use them when:
- Running unattended (long `make ci`, multi-PR matrix runs).
- Watching from a phone (`make tail`).
- Debugging "what got us into this state" after a failure.

The convention is "lowercased, hyphenated noun": `up-starting`, `seed-running`, `ci-passed`, `ci-failed`, `ucs-green`. Stay consistent so log scrapers can split on it.

## Log bundles

`scripts/log-bundle.sh` is called automatically when:
- `wait-for-stack.sh` times out on any endpoint (tag: `wait-timeout`).
- `make ci` fails for any reason (tag: `ci-failure`).

Manual bundles are useful when iterating:
```bash
bash scripts/log-bundle.sh manual-debug
ls logs/<latest>/
```

Each bundle is self-contained — copy the directory off the box and you have everything an offline reviewer needs.

## Hygiene

- `make clean` removes log bundles. Run it before commit/PR.
- Don't commit `logs/` or `PROGRESS.log` content (`.gitignore` should cover this; verify in your repo).
- `make reset` is the closest thing to a clean slate without removing the directory.

## Common one-shots

```bash
# Verify a single use case after editing it
make up && make seed && make uc2

# Manually re-seed after fiddling with vault
bash fixtures/vault-seed.sh

# Inspect the current image set the next ci will use
make show-images IMAGES=edge

# Run the integration suite against a remote stack
SBX_SMD_URL=https://smd.qa.example.com:27779 \
SBX_BOOT_URL=https://boot.qa.example.com:27791 \
make test-integration

# Generate a forensic bundle without failing anything
bash scripts/log-bundle.sh debug-$(date +%s)
```
