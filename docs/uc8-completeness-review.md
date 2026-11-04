# UC8 completeness review

UC8 implements the approved runner, failure, configuration, CRUD,
transition, cleanup, and repeatability gaps. Full-stack verification and
upstream compatibility gaps remain open.

| Area | Implemented assertions | Validation on 2026-10-13 |
| --- | --- | --- |
| Runner | Separate stdout/stderr/exit status, stdin, read-only JSON/YAML mounts, environment/config overrides, unique container ownership | Real CLI fixture tests pass with race detection |
| Failures | Invalid command/flag/input, HTTP 404/500, malformed response, inaccessible endpoint; rejected input sends no mutation | Default PR image and release v0.10.4 pass |
| Configuration | Default/explicit cluster, cluster/service URI, environment/explicit/suppressed token, ignored config, conflicting flags | Default PR #169 passes; release v0.10.4 fails explicit cluster selection |
| Timeout/cancellation | Request deadline, cancellation after request starts, diagnostics and owned container removal | Both CLI images pass |
| SMD | Group create/get/update/delete; membership add/get/delete; description/tags preserve membership; seeded component reads | Implemented; live run blocked by stopped Postgres/unhealthy SMD |
| Boot configuration | JSON stdin/file creation; UID get; JSON/pretty/YAML equivalence; YAML replacement; preserving merge patch; CLI delete and missing-resource failure | Passed twice against existing boot v0.3.2 with default CLI |
| Profile | Configuration spec.profile persistence through replacement and patch | Passed; organizational label, no separate resource or bootscript-selection assertion |
| Boot node | Owned stdin create/list/get, YAML replacement/read, merge patch, CLI delete; reserved xname collision guard | Passed twice against existing boot v0.3.2 with default CLI |
| PCS | Discovery/readiness/status and inventory; force-off/on CLI start/monitor/show; successful task plus independent Redfish state; restore On | Implemented; live flow unverified on unhealthy stack |
| Legacy cloud-init | Defaults, group raw/metadata, node metadata/user-data GET routes and formats | HTTP-fixture contracts pass; pinned metadata has no legacy admin routes |
| Cleanup | Exact-name/UID scopes, HTTP failures, interrupted bodies, transport deadlines, canceled context; independent removals | Race-tested helpers pass; twice-run boot cleanup succeeds |
| CI | Default/release jobs; two runs without reseeding; 20-minute test/60-minute job budgets; per-attempt logs/image identities; actual step outcome | YAML/static checks pass; hosted runs outstanding |
| Authentication | Separate boot/tokensmith stack; valid RSA, missing, wrong signature, expired signature beyond two-minute skew; independent HTTP enforcement plus CLI checks | Profile and occupancy guard checked; boot v0.3.2 lacks inbound middleware, so rejection tests are expected to fail |

The default CLI uses the immutable published [PR #169](https://github.com/OpenCHAMI/ochami/pull/169)
image, digest `sha256:94a4d4e35287a77f107c4739f53388537a659244f53d8b94d76b80bbc1c52c6b`.
Its reported build commit is `215bda0006c21d0ddcc43a031bf1daeb80553570`.
Release remains v0.10.4: retain the explicit-cluster regression as a failing
compatibility assertion. Token forwarding was fixed by merged
[PR #112](https://github.com/OpenCHAMI/ochami/pull/112).

Metadata image v3.5 reports source commit
`bc4e60c31fdb39722c0fea88ea687ca55ffc2cd9`; its
[server wiring](https://github.com/OpenCHAMI/metadata-service/blob/bc4e60c31fdb39722c0fea88ea687ca55ffc2cd9/cmd/server/server_extensions.go)
exposes modern resources and rendering, without legacy admin routes.
Successful fixtures do not close this live integration gap. Modern ochami
metadata lifecycle coverage remains additional work.

Authentication tests require real service rejection. The
[boot v0.3.2 server](https://github.com/OpenCHAMI/boot-service/blob/v0.3.2/cmd/server/main.go)
does not attach inbound JWT middleware even with enable-auth. A configured
probe returned HTTP 200 without a token. No open boot-service PR found
during this work supplies that wiring. Scope, issuer, audience,
refresh/exchange, and authorization policy are not claimed as covered by
these signature/expiry tests.

Remaining verification: run both full UC8 jobs twice on healthy seeded
stacks, inspect SMD/PCS side effects, run auth contracts against an enforcing
service, and run the full suite for interactions with UC5/UC6. Abrupt kills
can still leave uniquely named records because Go cleanup cannot run. The
existing stack was preserved; no upstream code was patched here.
