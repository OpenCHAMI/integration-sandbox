# Architecture

## Layers

```
┌─ host ─────────────────────────────────────────────────────────┐
│  make → scripts/{build-images,up,down,wait-for-stack}.sh       │
│  go test -tags integration → http(s) calls to 127.0.0.1:*      │
│  bats → docker exec sandbox-localstack awslocal …              │
│                                                                │
│  ┌─ docker network: openchami-sandbox ───────────────────────┐ │
│  │                                                           │ │
│  │  infra: vault (8200) | localstack (4566) | postgres       │ │
│  │                                                           │ │
│  │  bmc-sim: 8 × csm-rie (HTTPS, x0c0s{0..7}b0) | ipmi_sim   │ │
│  │                                                           │ │
│  │  core: smd (27779) | tokensmith (27780)                   │ │
│  │        boot-service (27791) | metadata-service (27792)    │ │
│  │        fru-tracker (27793) | power-control (28007)        │ │
│  │        magellan-runner (one-shot, profile=tools)          │ │
│  │                                                           │ │
│  └───────────────────────────────────────────────────────────┘ │
└────────────────────────────────────────────────────────────────┘
```

Three compose files, three layers:

| File | Purpose | Bring-up order |
|---|---|---|
| `compose/infra.yaml` | Stateful deps (vault, localstack, postgres) | first |
| `compose/bmc-sim.yaml` | Fake hardware (8 Redfish + 1 IPMI sim) | second |
| `compose/core.yaml` | OpenCHAMI services | third |

`scripts/up.sh` brings them up in that order with health waits between layers. `scripts/down.sh` tears them down in reverse.

## Why three files

- **Separation of concerns.** You can `docker compose -f compose/bmc-sim.yaml up -d` to inspect just the BMC fakes.
- **Profile-friendly.** Adding a `multi-smd` overlay later means an extra `-f compose/multi-smd.yaml` rather than a forked file.
- **Health-gating.** Each layer exits its `up` step healthy before the next starts, so `core/` services see real `vault`/`smd` rather than a half-initialized stub.

## What runs where

### Infra (third-party, all reused as-is)
- **vault** — Hashicorp Vault 1.21 in dev mode. Root token `dev-root-token`. **In-memory** — restarting it wipes state. UC3 exploits this.
- **localstack** — LocalStack 3, S3 only. Anonymous credentials `test`/`test`, region `us-east-1`. The container ships its own `awslocal`; tests `docker exec sandbox-localstack awslocal …` instead of installing awscli on the host.
- **postgres** — Postgres 16 alpine. One DB per service (`smd`, `boot`, `metadata`, `tokensmith`, `power`, `fru`), all created by `fixtures/pg-init.sql` on first boot.

### BMC sim
- **8 × csm-rie** — `ghcr.io/openchami/csm-rie:latest`, the Cray Redfish emulator. **HTTPS on :5000** with self-signed cert (gotcha — see [troubleshooting](troubleshooting.md)). Mockup `EX235a`. Auth `root`/`root_password`. Hostname-aliased so `https://x0c0s0b0/redfish/v1` resolves inside the docker network.
- **1 × ipmi_sim** — built from the
  [`OpenCHAMI/remote-console`](https://github.com/OpenCHAMI/remote-console)
  `ipmi_sim/Dockerfile` checkout pointed at by `SBX_REMOTE_CONSOLE_SRC`
  (defaults to a transient `git clone` into `.cache/`). UDP/623,
  default user `root`/`root_password`. Reachable inside the docker
  network as `x0c0s0b0-ipmi`. **No host port exposed.**

### Core
| service | image | port | healthcheck source |
|---|---|---|---|
| smd | `ghcr.io/openchami/smd:2.17` | 27779 | `curl http://localhost:27779/hsm/v2/service/ready` (image has curl) |
| tokensmith | `ghcr.io/openchami/tokensmith:latest` | 27780 | `curl http://localhost:27780/.well-known/jwks.json` |
| boot-service | `ghcr.io/openchami/boot-service:latest` | 27791 | distroless — host-side polling only |
| metadata-service | `ghcr.io/openchami/metadata-service:latest` | 27792 | distroless — host-side polling only |
| fru-tracker | `ghcr.io/openchami/fru-tracker:latest` | 27793 | debian-slim, no curl/wget — host-side polling only |
| power-control | `ghcr.io/openchami/power-control:latest` | 28007 | busybox without wget/curl — host-side polling only |
| magellan-runner | `ghcr.io/openchami/magellan:latest` | n/a | one-shot, profile `tools` |

`power-control:latest` is built locally because the upstream image
isn't published yet. `scripts/build-images.sh` resolves the source from
`SBX_POWER_CONTROL_SRC` (defaulting to a transient `git clone` of
[`OpenCHAMI/power-control`](https://github.com/OpenCHAMI/power-control))
and uses its `Dockerfile.build`.

## Networking

- One docker network: `openchami-sandbox`.
- Every service joins this network. Inter-service traffic uses container names (`http://smd:27779`).
- Host ports are bound only to **127.0.0.1** for safety (no LAN exposure). Override via env vars in compose if you need to expose externally.

## State

| What | Where | Survives container restart? |
|---|---|---|
| vault secrets | dev-mode in-memory | **NO** |
| localstack S3 | container ephemeral | NO |
| postgres data | container ephemeral | yes (volume `sandbox-postgres-data` not declared — recreate-only) |
| smd schema | postgres | as long as postgres survives |
| smd data | postgres | yes |
| boot-service nodes | tokensmith-style file backend in container | NO |
| metadata-service groups | tokensmith-style file backend in container | NO |
| fru-tracker SQLite | tmpfs `/data` | **NO** |
| tokensmith keys | tmpfs `/tokensmith/keys` | NO |

Tests must not depend on cross-restart persistence except for "postgres survives a restart of postgres" (which UC3 explicitly tests).

## Tests

`tests/` is a **standalone Go module** (`tests/go.mod`). It does not vendor or pin against any service repo. All assertions go through HTTP/JSON; no Go imports of OpenCHAMI service code. This decoupling is on purpose — the harness must work against unrelated PR builds and release versions without recompiling the suite.

Build tag `integration` gates every file. `go test ./...` without the tag is a no-op (skip), which is what `go vet` and `go build` rely on.

`tests/bats/` is a CLI smoke layer. Runs against the live host ports + `docker exec` for localstack.
