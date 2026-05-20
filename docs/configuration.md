# Configuration

Everything that's tunable, in one place.

## Image manifests

The compose stack reads every image tag from a `SBX_<NAME>_IMAGE` environment variable. Defaults live in `images/<manifest>.env`. Per-service env vars override the manifest entry.

### Built-in manifests

| File | Intent |
|---|---|
| `images/default.env` | Pinned-known-good (today: smd 2.17, everything else `:latest`). What `make ci` uses without args. |
| `images/edge.env` | OpenCHAMI services track `:main`; smd tracks `:latest`. Run nightly to detect upstream breakage. |
| `images/release-v1.0.env` | Template; replace `:v1.0.0` placeholders with real tags when the v1.0 train cuts. |

### Selection precedence (highest wins)

1. `SBX_<NAME>_IMAGE` set in the caller's env or on the make command line.
2. The active manifest (`make IMAGES=<name>`, default `default`).
3. Compose-file fallback (`:latest` for everything OpenCHAMI, pinned for third-party).

### Examples

```bash
# Default
make ci

# Named manifest
make ci IMAGES=edge

# Single-service override
make ci SBX_TOKENSMITH_IMAGE=ghcr.io/openchami/tokensmith:pr-23

# Stack overrides on top of a manifest (testing complementary PRs)
make ci IMAGES=edge \
    SBX_TOKENSMITH_IMAGE=ghcr.io/openchami/tokensmith:pr-23 \
    SBX_BOOT_IMAGE=ghcr.io/openchami/boot-service:pr-7

# External manifest by absolute path
make ci IMAGES=/etc/openchami/qa-2026-Q1.env

# Show what would actually run, without bringing anything up
make show-images IMAGES=edge
```

### Adding a manifest

Drop a new file at `images/<name>.env`. Any subset of the variables below; missing keys fall back to the compose default (`:latest`).

| Variable | Service |
|---|---|
| `SBX_VAULT_IMAGE` | vault |
| `SBX_LOCALSTACK_IMAGE` | localstack |
| `SBX_POSTGRES_IMAGE` | postgres |
| `SBX_CSM_RIE_IMAGE` | the Cray Redfish emulator (8 instances) |
| `SBX_IPMI_SIM_IMAGE` | the IPMI simulator |
| `SBX_SMD_IMAGE` | smd |
| `SBX_TOKENSMITH_IMAGE` | tokensmith |
| `SBX_BOOT_IMAGE` | boot-service |
| `SBX_METADATA_IMAGE` | metadata-service |
| `SBX_FRU_IMAGE` | fru-tracker |
| `SBX_POWER_IMAGE` | power-control |
| `SBX_MAGELLAN_IMAGE` | magellan |

## Endpoint URLs (test-side)

The Go integration suite reads endpoints from a `SBX_<KEY>_URL` env var, falling back to the compose default. Override these to point the suite at a remote stack.

| Variable | Default | Notes |
|---|---|---|
| `SBX_VAULT_URL` | `http://127.0.0.1:8200` | |
| `SBX_LOCALSTACK_URL` | `http://127.0.0.1:4566` | |
| `SBX_SMD_URL` | `http://127.0.0.1:27779` | |
| `SBX_TOKENSMITH_URL` | `http://127.0.0.1:27780` | |
| `SBX_BOOT_URL` | `http://127.0.0.1:27791` | |
| `SBX_METADATA_URL` | `http://127.0.0.1:27792` | |
| `SBX_FRU_URL` | `http://127.0.0.1:27793` | |
| `SBX_POWER_URL` | `http://127.0.0.1:28007` | |
| `SBX_REDFISH_URL` | `https://127.0.0.1:5000` | HTTPS, self-signed cert |
| `SBX_WAIT_TIMEOUT` | `30s` | suite-setup health-poll deadline |
| `SBX_UC_CLEANUP` | `0` | set to `1` to enable per-UC cleanup |

## Fixtures

Files under `fixtures/` are applied by `make seed` (which is also a phase of `make ci`). All idempotent — safe to re-run.

| File | What it does |
|---|---|
| `pg-init.sql` | Creates per-service DBs on first postgres boot. Not re-applied on restart. |
| `vault-seed.sh` | Enables KV-v2 at `openchami/`, KV-v1 at `hms-creds/`; writes cluster + per-xname creds. |
| `s3-buckets.sh` | Creates `boot-images`, `openchami-logs`, `parquet`; uploads sentinel boot script and cloud-init template. |
| `seed-smd.sh` | POSTs `smd-components.json` (8 nodes) and `redfish-endpoints.json` (8 BMCs) to SMD. |
| `tokensmith-config.json` | Reference config; not currently used by the running tokensmith (we bypass the entrypoint). |
| `smd-components.json` | The 8 node definitions used by `seed-smd.sh`. |
| `redfish-endpoints.json` | The 8 BMC entries (xname/FQDN/creds) used by `seed-smd.sh`. |
| `boot-configs.json` | Reference data for boot-service tests; not auto-applied. |
| `inventory-snapshot.json` | Reference data for fru-tracker tests; not auto-applied. |

## Ports (host-published, all bound to `127.0.0.1`)

| Service | Host port |
|---|---|
| vault | 8200 |
| localstack S3 | 4566 |
| smd | 27779 |
| tokensmith | 27780 |
| boot-service | 27791 |
| metadata-service | 27792 |
| fru-tracker | 27793 |
| power-control | 28007 |
| redfish-bmc-0 | 5000 |

Other Redfish emulators (1–7) and the IPMI simulator are docker-network-only.

## Credentials (every value is sandbox-only)

| Asset | Value | Source |
|---|---|---|
| Vault root token | `dev-root-token` | `compose/infra.yaml` |
| Vault per-node creds | `Username=root`, `Password=root_password` at `hms-creds/<xname>` | `fixtures/vault-seed.sh` |
| Postgres | user/pass `openchami`/`openchami`, DB `openchami` (plus per-service DBs) | `compose/infra.yaml` + `fixtures/pg-init.sql` |
| LocalStack S3 | `test`/`test`, region `us-east-1` | LocalStack default |
| Redfish BMC | `root`/`root_password` (Administrator) | csm-rie default |
| IPMI BMC | `root`/`root_password` | ipmi_sim default |

These match the published emulator defaults — do not change without updating both fixtures and tests.

## XNAMES (the fake fleet)

| xname | NID | Role | Initial state |
|---|---|---|---|
| `x0c0s0b0n0` | 1000 | Compute | On |
| `x0c0s1b0n0` | 1001 | Compute | On |
| `x0c0s2b0n0` | 1002 | Compute | On |
| `x0c0s3b0n0` | 1003 | Compute | Off |
| `x0c0s4b0n0` | 1004 | Compute | Off |
| `x0c0s5b0n0` | 1005 | Compute | Off |
| `x0c0s6b0n0` | 1006 | Compute | Off |
| `x0c0s7b0n0` | 1007 | Compute | Off |

The corresponding BMCs are aliased on the docker network as `x0c0s{N}b0` so `https://x0c0s5b0/redfish/v1` resolves from inside any service container.

UC1 owns `x0c0s0..3`, UC2 owns `x0c0s4..7`, UC3 owns `x9c0s*` (out-of-fleet), so the use cases never collide.
