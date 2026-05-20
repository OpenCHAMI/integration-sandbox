# Known issues

## Harness limitations

| Limitation | Why | Workaround |
|---|---|---|
| Single SMD instance — "two clusters" UC2 uses node `groups` rather than two SMD deployments. | Two SMD instances + two postgres DBs is feasible but adds 2 GB RAM and ~30 s to bring-up. | Layer in `compose/multi-smd.yaml` (planned) and run with `SBX_MULTI_SMD=1`. |
| No DHCP / PXE flow. | Real PXE needs a layer-2 network the harness can't provide inside docker compose. | Test the `boot-service /bootscript` endpoint shape directly; integration with coresmd is operator-territory. |
| No real OIDC. | Tokensmith runs with `--non-enforcing` and `--enable-local-user-mint` so the sandbox doesn't have to spin up Authelia / step-ca. | `tests/integration/uc<N>` doesn't validate JWT scopes end-to-end. Use tokensmith's own test suite for that. |
| No metrics / Prometheus assertions. | Most services either don't expose metrics or stub them. | Add per-service metrics tests as the upstream services mature. |
| Vault loses state on restart (UC3). | Dev mode is in-memory by design. | Reseed after restart, or use a real Vault for tests that need persistence. |
| Distroless services have no in-container healthcheck. | distroless ships no curl/wget/shell. | `wait-for-stack.sh` polls from the host. Don't add `HEALTHCHECK:` directives to those services. |
| `power-control` not pulled from ghcr.io. | The image isn't published yet. | `build-images.sh` builds locally from a `power-control` checkout — set `SBX_POWER_CONTROL_SRC=/path/to/power-control` to point at it, otherwise the script clones the public repo on demand. |
| `power-control` Vault is bypassed. | `VAULTv0.Init` insists on Kubernetes ServiceAccount auth before falling back to token. | Set `VAULT_ENABLED=false`. Documented in [troubleshooting](troubleshooting.md). |
| `boot-service` does not enforce `metadata.name` uniqueness. | Upstream design choice (uid is the canonical key). | Use `bootResetByName` before every create. |
| `/nodes/{uid}` and `/groups/{uid}` are uid-keyed in boot-service / metadata-service. | Consistent across services that derive their REST surface from Fabrica. | Look up uid first; helpers in `clientutil_test.go` hide the trap. |

## Upstream issues found during scaffolding

The sandbox-scaffolding pass on 2026-05-03 catalogued issues in several
sibling repos. Each repo holds the authoritative record (typically a
`bugs.md` or its equivalent issue tracker); the sandbox does not maintain
its own duplicate list. Search each repo for `bugs.md` or open issues
matching the headline below:

| Repo | Class of issue surfaced |
|---|---|
| [`OpenCHAMI/openchami-operator`](https://github.com/OpenCHAMI/openchami-operator) | Dockerfile build path, CoreDHCP image typo, kind-config hardcoding, e2e placeholder block, observability stubs, lifecycle Vault address, import alias typo. (All fixed 2026-05-04 — see that repo's `bugs.md`.) |
| [`OpenCHAMI/boot-service`](https://github.com/OpenCHAMI/boot-service) | Bootstrap-token validation gap, hardcoded HSM URL fallback, distroless healthcheck, metrics stub. |
| [`OpenCHAMI/metadata-service`](https://github.com/OpenCHAMI/metadata-service) | Dockerfile comment drift, distroless healthcheck. |
| [`OpenCHAMI/tokensmith`](https://github.com/OpenCHAMI/tokensmith) | Entrypoint requires `config.json`, key-dir not writable as uid 65534, hot-reload note. |
| [`OpenCHAMI/power-control`](https://github.com/OpenCHAMI/power-control) | K8s SA auth blocks non-K8s deployments, image not published, K8s-flavored `SMS_SERVER` default. |
| [`OpenCHAMI/fru-tracker`](https://github.com/OpenCHAMI/fru-tracker) | `/data` not writable by runtime user, debian-slim base inconsistent with siblings. |
| [`OpenCHAMI/magellan`](https://github.com/OpenCHAMI/magellan) | Hardcoded demo creds, `setup.sh` non-idempotence, multi-BMC partial-failure handling. |
| `OpenCHAMI/{coresmd, legendary-funicular, versitygw-quadlet, release}` | Doc-level drift, low-severity items. |
| [`OpenCHAMI/remote-console`](https://github.com/OpenCHAMI/remote-console) | None surfaced; `ipmi_sim` Dockerfile and configs were clean. |

## Open questions / future work

- **Real SMD-multi-instance mode.** A second SMD container with its own postgres DB, gated by `compose/multi-smd.yaml` and `SBX_MULTI_SMD=1`. Closes the gap in UC2.
- **OIDC + JWKS validation flow.** Layer in step-ca + Authelia (the pattern lives in `tokensmith/tests/integration/docker-compose.yml`). Lets us validate scope enforcement end-to-end.
- **Magellan scan round-trip.** The runner container is registered with profile `tools`; a UC could `docker compose run --rm magellan-runner scan …` against the BMC sims and assert SMD picks up the discovery.
- **fru-tracker discovery snapshot ingest** + reconciler latency assertion.
- **Failure injection.** What does each service do when SMD goes away mid-call? When postgres pauses? When a BMC sim returns garbage? UC3 does the simplest version (clean restart); harder failure modes are open.
- **Bug-to-PR pipeline.** Each upstream repo's `bugs.md` (or equivalent
  issue list) is the right input for a follow-on pass that turns each
  entry into a GitHub issue or PR. Deliberately deferred.
