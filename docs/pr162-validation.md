# PR162 CLI validation

This change adds reproducible v3/v4 CLI builds, fixture contracts, owned real API
lifecycles, and dedicated-runner orchestration. It does **not** claim a complete
live authentication pass.

## Recorded inputs and completed checks — 2026-10-20

| Input | Identity |
|---|---|
| v3 baseline | `79051e47b1a1d57ad0ebfa755899231fc86f46ce` |
| v4 migrated tip | `26882481bfd3cdb9430b5f38514abb98a0fb206c` |
| v3 local image | `sha256:a13d4add817a146fe58b4e32d481d73d79046dd34e2fa22aba564d373615a1cb` |
| v4 local image | `sha256:ba165e373a8ab49fbaacab836fce43de7852271fbd6bf9b111177afb98e71307` |
| BSS v1.32.3 | `sha256:6bac4096dbe294b99f687f6ee570b0bb9279e58be362a91d088f0b23c3eda10c` |
| Legacy cloud-init v1.4.12 | `sha256:b5e85cf034404a7d617d69da7e5e8de0b01db83b666a771dbfb161bba5063ba9` |

Both CLI images were cross-built using the upstream Makefile and the same
explicit host toolchain, Go 1.27.3, with CGO disabled. Runtime version checks
confirm full source commits. Logs record local image IDs (these images have no
published registry digest). The default Docker build uses pinned Go 1.27.2;
the upstream Dockerfile failed at Wolfi's `apk add tini` with certificate
verification failure. The sandbox wrapper avoids that runtime installation;
host mode was used for the completed builds.

`make uc8-cli-contracts` passed for both images with race detection. Local
artifacts: `logs/20261020T174917Z/uc8-cli-contracts/` (`outcomes.txt`, per-image
identities, and per-revision test logs). Coverage:
configuration/token precedence, unchanged bearer forwarding, invalid input and
HTTP/transport failures, stdin/files, timeout, cancellation, cleanup ownership,
legacy HTTP output contracts, fixture algorithm/signature/expiry regressions,
and CLI-local token validation before HTTP. The selected test cluster explicitly
enables authentication; `--ignore-config` would bypass these local checks.

The token matrix verifies exit code 4 and no HTTP requests for expired/future
time claims. Duplicate claims are locally accepted by v3 and rejected by v4.
Missing optional time claims and invalid signatures remain locally accepted,
and accepted tokens reach the fixture byte-for-byte unchanged. Warning-window
tokens remain accepted. These are CLI contracts, not service enforcement proof.

Go vet, integration lint, shell syntax/ShellCheck, Compose configuration checks,
REUSE, and focused cleanup/fixture race checks passed during implementation.

The occupied-port guard and dedicated-runner existing-project guard were also
executed; both refused with exit code 1 before creating services. The issue
draft was retained locally and no issue with its title was published.

PostgreSQL was recovered by starting its existing container. Its anonymous
data volume and container identity were preserved; the `smd` database remained
accessible, and SMD became healthy. No existing service image was replaced.
Running metadata remains v0.2.2, while the default/release manifests select
v3.5; dedicated comparison rejects such drift rather than silently testing it.

## Live execution still required

The existing workstation stack was preserved. Full UC4/UC8 repeats, modern
metadata CRUD, real BSS/legacy cloud-init CRUD, and UC5/UC6 interactions were
implemented but were not run against it. Use the dedicated workflow or
`SBX_DEDICATED_RUNNER=1 bash scripts/pr162-ci.sh` on a free runner. Source commits
must be fetchable there; `SBX_OCHAMI_SOURCE` can supply a local repository.

The dedicated flow seeds once and tests both CLIs twice against identical
issuer/service images. It verifies image selection and readiness before every
attempt. Unique names, UID ownership, collision checks, independent HTTP reads,
and separate cleanup deadlines preserve unrelated data. UC4 additionally tests
real RFC8693 exchange, JWKS verification, refresh-token rotation, immutable
identity/scopes, bootstrap replay rejection, and SMD round-trips.

Direct legacy cloud-init container URLs have **no `/cloud-init` prefix**; native
routes are `/admin/*`, `/version`, and `/openapi.json`. Documentation examples
with a prefix presume a deployment proxy. Modern metadata remains a distinct
API with its own CLI configuration key.

## Authentication and policy blockers

- Boot v0.3.2 does not enforce inbound JWT authentication. Existing upstream
  [boot-service issue #58](https://github.com/OpenCHAMI/boot-service/issues/58)
  tracks this. Full validation requires `SBX_ENFORCING_BOOT_IMAGE` set to an
  immutable enforcing build; absence is a nonzero blocked result.
- Legacy cloud-init v1.4.12 registers admin routes outside the JWT middleware.
  Its [tagged router wiring](https://github.com/OpenCHAMI/cloud-init/blob/92b47c69cfb2ec92a511cd984ab1f4ab37174b0e/cmd/cloud-init-server/main.go#L292-L305)
  puts middleware on an empty `/secure` router. An enforcing upstream build or
  real deployment gateway is required; setting `JWKS_URL` alone is insufficient.
  The [upstream issue draft](../issues/cloud-init-admin-jwt-enforcement.md) is
  saved locally for review and will not be published automatically.
- Isolated BSS enforcement assertions cover valid, missing, wrong-signature,
  expired, future-`nbf`, and missing sub/iss/aud tokens. This new live profile
  still needs dedicated execution. BSS requires claim presence but does not
  establish issuer/audience value or scope policy. Public bootscript/service
  endpoints are not used as proof of protected-route enforcement.
- SMD, modern metadata, and PCS compatibility checks do not prove inbound
  enforcement. No enforcing deployments/gateways for them were supplied here.
  Their enforcement remains uncovered and requires explicit deployment policy.
- Signing-key rotation has no supported live mechanism established for these
  pinned deployments; BSS loads JWKS at startup. Refresh-token rotation does not
  prove signing-key rotation. External OIDC is deferred by agreement.

Do not weaken authentication assertions, relabel skips as passes, or treat an
unhealthy dependency as JWT rejection. Every failed run retains its nonzero
status and diagnostic bundle. No PR comment is posted automatically.
