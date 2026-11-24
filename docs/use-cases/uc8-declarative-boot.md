# Use Case 8: Declarative boot acceptance contract

## Purpose and boundary

This is a **proposed, independent oracle**, not a claim that the pinned
boot-service already implements it. The contract is the approved
`boot-service-50-additive-acceptance` plan, frozen clauses 1–13. The sandbox
must use public HTTP and its own DTOs, not import the service or Fabrica
models/resolver to determine expectations. There are no overlays, merging or
inheritance.

Fixtures in `../../fixtures/declarative-boot/` are data for the runner:

| File | Purpose |
| --- | --- |
| `configs.json` | Literal BootConfig series, named revisions A and B and their specs. |
| `nodes.json` | Existing SMD node xnames, NIDs and MACs; test-owned group memberships. |
| `assignments.json` | Group/default, MAC canary, pinned A and reference variants. |
| `expected.json` | Independent literal delivered tuples and every rollout stage. |
| `legacy.json` | Legacy default derived from `fixtures/boot-configs.json` with baseline-valid HTTPS kernel/initrd, and adoption mapping. |
| `negative.json` | Explicitly negative request inputs and expected denial. |
| `scenarios.json` | Required scenario IDs and clause mapping for the zero-skip runner. |

The runner must execute **every** manifest ID, report exactly one PASS/FAIL per
ID, reject missing, duplicate, unexpected or skipped IDs, and exit nonzero on
any failure or unsupported API, auth, fixture or environment. An expected
negative is PASS only when its denial **and** unchanged state/artifacts are
verified. The known unrelated UC3 metadata restart/group-loss failure is not
hidden by this lane or evidence presented as a green full suite.

## Isolated execution

After both helper commands have been built and reviewed, run with Go 1.26.10,
Docker Compose, jq, curl, Perl and a matching host boot client:

```sh
SBX_DECLARATIVE_BOOT_IMAGE=<candidate-image> \
SBX_DECLARATIVE_BOOT_CLIENT_BIN=/absolute/path/to/boot-client \
make test-declarative-boot
```

The host client must come from the same candidate as the image; it receives
`--server` and `BOOT_SERVICE_TOKEN` from the tests. The dedicated workflow's
manual/reusable lane requires both an image and a matching source ref. Its PR
job compiles and tests helpers but does not assert the feature exists.

The run owns a unique Compose project, private network and two durable bind
directories. PostgreSQL has test-only credentials `openchami`/`openchami`.
SMD (container port 27779), TokenSmith facade (27780), and both boot instances
(27790) use four distinct preselected ephemeral `127.0.0.1` ports. The runner
exports `SBX_DECLARATIVE_BOOT_HOST_PORT`, `SBX_DECLARATIVE_AUTH_OFF_HOST_PORT`,
`SBX_DECLARATIVE_SMD_HOST_PORT`, and `SBX_DECLARATIVE_TOKENSMITH_HOST_PORT`
before Compose creates containers, preserving their bindings across restarts.
The selection is not a reservation; Docker's bind is authoritative and a
collision fails the run. Port 5000 and the ordinary
sandbox are not used. The boot service also enables internal metrics port
27791 and serves `/metrics` on its main listener. Real TokenSmith uses issuer
`http://tokensmith:27780`; the facade forwards positive exchanges and adds
ephemeral test public keys for invalid-claim tests. The private keys are never
published and are discarded with the owned run.

The frozen fixture packet retains the four canonical xnames `x0c0s0b0n0`
through `x0c0s3b0n0`; `x0c0s2b0n0` and `x0c0s3b0n0` remain safety witnesses.
Serial lifecycle cases instead reserve `x1c0s0b0n0` through `x1c0s7b0n0`
with NIDs `10000`–`10007` and per-case randomized locally administered MACs.
Each requires the SMD component to be absent before creation and cleans up its
own components, interfaces and groups. These fixed slots are isolated by the
run-owned project and must not be used concurrently within that project.
Only `routes-additive-legacy`, `profile-and-legacy-selection`, and
`unknown-and-no-assignment` seed/update a boot-service Node for the existing
legacy local provider. They inspect by xname, refuse duplicates, and restore
the captured preexisting spec or remove their own created UID. New-provider
and freshness scenarios remain SMD-only.
The separate auth-off instance is seeded via the old public API with the
literal legacy tuple. The service data directories are not tmpfs and never
shared between writers.

The runner exports the `SBX_DECLARATIVE_*` endpoints, compose project and file,
run directory, image ID, host control/client paths, TokenSmith container and
bootstrap store, auth-off service, asset-manifest path, and append-only control
receipt path. Control also receives `RUN_DIR`, `COMPOSE_FILE`,
`COMPOSE_PROJECT`, `PROJECT`, `FIXTURE_URL`, `BOOT_URL`, `BOOT_IMAGE_ID`,
`FIXTURE_IMAGE`, and `CONTROL_RECEIPTS`. Control commands write JSON to stdout;
token responses are consumed by tests and must never enter evidence. The
auth-enabled boot instance reads `tokensmith_bootstrap_token` from the run-owned
read-only directory bind `/run/boot-config/boot.json`, outside the business-data
store. The harness provisions a fresh, real one-use TokenSmith HSM bootstrap
token before each controlled start; auth-off has no TokenSmith config. This is
test provisioning, not a claim of production durable HSM identity or #77
closure. All issued token values enter a private redaction ledger. The config
directory and ledger are removed after owned containers/watchers stop and are
excluded from CI artifact uploads even if cleanup cannot finish; only
nonsecret issuance metadata may remain. Initial provisioning uses the same
`prepare-boot-identity` control action as later starts. If the run contains
`identity-redaction-incomplete`, failure diagnostics are metadata-only and
report `credential-safe log capture unavailable`; raw Compose logs are not
captured. Data durability assertions do not
include the credential file. The runner creates and exports
`SBX_DECLARATIVE_QUIESCENCE_LOCK` as a stable,
run-owned 0600 file with a 0700 `.inflight` directory before seeding. It never
clears outstanding markers: a sticky ambiguous write invalidates capture and
the owned run must be discarded. The runner's seed requests finish and consume
their responses before the controlled phase. The barrier covers cooperating
HTTP/CLI/control writers for new-kind namespaces and legacy BootConfiguration;
it does not claim a globally atomic volume snapshot or cover HSM Node background
writes. Future candidate work (Tasks 8/15) must audit the service for asynchronous
or reconciliation writes to captured records after HTTP completion. The
snapshot preflight gracefully stops and restarts each owned container: it
independently verifies the same container, bind and image, the actual inspected
start timestamps, and a strictly later post-snapshot start. A no-op is failure.
Legacy seed HTTP failures retain a bounded, redacted response in the run log
directory and remain nonzero. Runtime logs are under
`logs/issue50/run-<UTC>-<nonce>/`. Failure is nonzero, including
missing IDs, skips, fixture failures and unsupported endpoints. Teardown is
restricted to the run's Compose project and labelled assets. This document is
not evidence that a baseline or candidate has passed.

Before the suite the runner captures the served `/openapi.json`, immutable
image metadata, source HEAD and checkout cleanliness. `SBX_DECLARATIVE_BASELINE_SOURCE`
may select the read-only source checkout; by default the runner looks for the
adjacent `boot-service-pr75-review` worktree. Missing source or OpenAPI evidence
does not skip the suite: it leaves failures unclassified. A baseline-only 404
or 405 label requires the exact PR75 image ID and revision/HEAD, successful
OpenAPI with exact `/bootconfigs` and `/bootassignments` paths absent, a clock
anchor, and one unambiguous timestamped POST to one of those exact paths during
the failed scenario. Ambiguous, unrelated or differently sourced failures stay
unclassified. For the three adoption cases only, an ordered primary POST 405
and cleanup GET 405 may both be attributed when each exact or templated path
is absent from served OpenAPI and no other non-success request or failure is
present in the case window. The classifier records both requests; no
classification changes a failing test to pass.

## Wire contract and runtime bindings

Add v1 `BootConfig` at `/bootconfigs` and mutable `BootAssignment` at
`/bootassignments`, side-by-side with unchanged mutable `/bootconfigurations`,
`/nodes`, `/bmcs`, modern boot and enabled `/boot/v1/*` paths. Do not rewrite
legacy records on startup. BootConfig spec has required `kernel` and optional
`initrd` and `params`, all strings, with existing URL/path validation. Fabrica
canonical digest and immutable metadata apply; selectors, profile and priority
are **not** BootConfig fields.

Assignment spec is exactly `selector?` (`macs?`, `nids?` as int32,
`xnames?`, `groups?`), `profile?`, `priority?`, and required `configRef`
(`name`, optional `uid`, `revisionName`, or `selector` of `default` or
`latest`). There are zero or one reference discriminators, not multiple.
Priority is an integer 0–100. Names are case-sensitive DNS-subdomain-style,
at most 253 bytes, with no implicit lowercasing, renaming or recycling while a
record exists. Assignment metadata name and UID are immutable; status cannot
change spec or reference identity.

The JSON fixture `bind` maps are **instructions to the client**, not wire
fields: capture `revisionA.uid` from the actual successful A response, then
insert that value into `/uid` of `referenceVariants.uidA.value`. Likewise,
capture `retiredRevision.uid` from an intentionally prepared retired/corrupt
test store for the negative reference case; retirement cannot be performed
through this API. For the wrong-series case first create `different-series`,
then bind A's UID to that series' `configRef`; this must not be merely a
missing-series test. Captured UID, digest, revision metadata and strong ETags are
opaque runtime values, never literal expected output strings. Do not send
`bind`, placeholder text, scenario IDs or expectation metadata as resource
fields. Use separate logical names for independent scenarios. A private
fixture/restore may supply an invalid persisted reference for fail-closed
tests, not a new production force-delete/retire API.

### Concrete requests and responses

These examples fix the specified paths, fields, statuses and header semantics;
other generated revision/promotion alias paths and request envelopes must be
discovered from the pinned public revision contract/served
OpenAPI, not guessed or treated as an unsupported-feature skip.

| Action | Request | Required response and state |
| --- | --- | --- |
| First named create | `PUT /bootassignments/by-name/group-default`, `If-None-Match: *`, JSON body below | 201 and strong `ETag`; capture assignment UID and ETag. Competing create using `If-None-Match: *`: 412, exactly one record. |
| Named read | `GET /bootassignments/by-name/group-default` | 200, current resource and strong ETag; requires `read:boot`. |
| Same-spec replay | Same named PUT with `If-Match: <captured-current-strong-ETag>` and same body | 200, same UID and ETag. The angle-bracket expression is client substitution, not a literal header. |
| Change or delete | Named PUT or DELETE with exact current strong `If-Match` | Successful change is reflected immediately; missing, stale or `*` update/delete precondition gives 412 without mutation. |
| UID APIs | PUT/PATCH/DELETE the ordinary UID resource with current strong `If-Match` | Same identity, index, reference, auth and precondition checks as named operations; missing/stale/wildcard 412, rename 409. Same-ETag concurrent writes: exactly one winner. |
| Collection create | `POST /bootassignments` with the body below | 201 for a new name; duplicate name 409 and no second record. |
| Create A | `POST /bootconfigs?revisionName=release-a` creates series `acceptance-os` and its first revision with `configs.json` A spec | 201; capture A revision UID and digest from the response; both default and latest resolve to A. |
| Ensure B | `PUT /bootconfigs/acceptance-os/revisions/release-b` with B spec, then identical ensure again in the existing series | 201 then 200; same B UID/digest; latest B, default still A. Same B name with different canonical content: 409, no change. |
| Promote / roll back | Promote default to B, then A, with exact current pointer strong ETag in `If-Match` | Success yields current pointer/ETag; missing or stale `If-Match` gives 412 with no pointer or artifact change. One winner in a same-ETag race. |
| Bad references | Wrong-series, missing or retired reference on assignment mutation | 422 and no state change. Multiple discriminators and malformed JSON are negative inputs; require a 4xx denial and unchanged state. |
| Retire | Every new BootConfig revision retirement alias | 409 with `boot_config_retirement_disabled`; UID, revision and all pins remain intact. Series deletion and immutable UID spec mutation: 405. |

Example body for both named PUT and collection POST:

```json
{
  "metadata": {"name": "group-default"},
  "spec": {
    "selector": {"groups": ["acceptance-red", "acceptance-blue"]},
    "priority": 100,
    "configRef": {"name": "acceptance-os", "selector": "default"}
  }
}
```

`configRef: {"name":"acceptance-os"}` follows default; explicit `default`
does likewise. `latest`, `revisionName: "release-a"` and the captured A `uid`
are distinct reference modes. Ensure/promotion and **all** assignment
create/update/delete entry points, including ordinary UID and generated
aliases, share one synchronization boundary in the single-writer deployment.
Simultaneous writes with one ETag have exactly one winner. Do not infer
multi-process safety from these in-process races.

## Selection truth table

For **new assignments only**, each listed value and each selector family is an
OR alternative. The one highest matched family supplies the score; scores do
not add. MAC case and conventional separators normalize; xnames/groups match
exactly, not as glob or regex. The table assumes requested profile qualifies.

| Matched families | Score | Result |
| --- | ---: | --- |
| MAC, including normalized equivalent | 100 | MAC wins even over NID/xname/multiple groups. |
| NID, no MAC | 75 | NID wins over xname/groups. |
| xname, no MAC/NID | 50 | xname wins over groups. |
| One or more groups only | 25 | Two matching groups still score 25, not 50. |
| No selector fields | 1 | Catch-all. |
| Selector present, no value matches | — | Assignment does not match. |

Compare scores first, then higher integer priority, then ascending assignment
name; reverse insertion order must not change ties. New profile empty and
`default` are equivalent; an explicitly requested profile filters new
assignments by exact name. If none matches node **and** requested profile,
delegate to the existing legacy path, including its current HTTP profile
behavior and additive ranking/tie rules; never apply the new truth table to
legacy BootConfigurations. Select a winner before resolving its reference: a
broken winning reference returns 503 and a non-booting `#!ipxe` refusal with
nonsecret assignment/reference identity, never another assignment, default,
latest or legacy rescue. Unknown-node behavior remains legacy behavior.

## Literal rollout oracle

Seed SMD components, EthernetInterfaces and groups for the lifecycle scratch
nodes; the production-provider scenario must not POST a boot-service Node.
Install `group-default` and `pinned-a` after A exists; ensure B twice, then
install `mac-canary`. The canary node belongs to *both* fixture groups but its
MAC score 100 beats group score 25 despite group priority 100 versus canary
priority 0. The pinned node's xname wins its group match. `expected.json`
records the stages, including A on the pinned node at **every** stage. Read
the pointers and retrieve actual `/bootscript` output after each successful
operation. Assert the following exact tuple values, not a tuple computed by the
service or its model; reject any other revision's kernel, initrd or arguments.

| Revision | Kernel | Initrd | Exact parameter tokens |
| --- | --- | --- | --- |
| A | `https://images.example.invalid/boot-images/acceptance-a-kernel` | `https://images.example.invalid/boot-images/acceptance-a-initrd` | `console=ttyS0`, `root=live:s3://boot-images/acceptance-a-rootfs.squashfs`, `release=A`, `ip=dhcp` |
| B | `https://images.example.invalid/boot-images/acceptance-b-kernel` | `https://images.example.invalid/boot-images/acceptance-b-initrd` | `console=ttyS1`, `root=live:s3://boot-images/acceptance-b-rootfs.squashfs`, `release=B`, `ip=static` |

Stages: created A → ensure B twice (default still A, latest B) → canary B,
default A → promote B (default B) → roll back A (default A). Canary remains B
and pinned consumer A throughout their applicable stages. Compare artifact
tokens semantically without depending on unrelated volatile script comments.
The existing seed does not apply `fixtures/boot-configs.json` as a boot
configuration. The packet's legacy fixture changes only its kernel/initrd
locations to HTTPS; its parameters remain unchanged. The unmatched legacy node
continues the legacy fixture behavior; compare its
baseline and candidate responses semantically, including legacy profile rules.

## Security, freshness and recovery assertions

New management requires working verification even when legacy auth is off:
unavailable verification returns 503 without mutation; legacy startup and
public boot stay unchanged. Verify signature/JWKS, allowed algorithm, expiry,
issuer equal to configured TokenSmithURL, and audience containing
`boot-service`: wrong/expired/invalid tokens return 401; insufficient scope
returns 403. Reads require `read:boot`, proposal `propose:boot`, assignment
mutations and default promotion `promote:boot`. Exercise every direct alias,
status, trailing-slash and UID route; status cannot change pointers. No raw
JWTs, secrets or credentials in fixtures or evidence.

A proposer can create B while all consumers follow default or pins. If any
assignment follows latest, an operation that **advances** latest additionally
requires `promote:boot`, including unnamed revision POST and named ensure of
new content. Identical ensure replay does not advance latest. The latest
follower/create race shares the mutation gate; use an administrator holding all
three scopes for authorized latest advancement and verify delivery. Negative
auth cases must assert unchanged pointers, assignment identity and scripts.

Prove an actual new-path cache hit with stable diagnostics/metrics, not merely
two equal responses, then exercise pointer/assignment writes and require new
requests after success to see the new snapshot immediately. Cache identity
includes resolved UID/digest, assignment UID/version, profile and all render
inputs: xname, MAC, NID, hostname, groups and template identity. Concurrent
reads may see coherent A **or** B, never a mixed tuple. Healthy SMD identity and
group changes are visible within five seconds with membership TTL at most one
second; once refresh cannot meet that bound, fail closed for new delivery.
Legacy provider/cache behavior remains covered without global redesign.

Adoption is explicit: `client boot-adopt --source-uid UID --series NAME
--assignment NAME` defaults to dry-run; `--dry-run` is explicit and mutually
exclusive with `--execute`. Copy content into `legacy-<sourceUID>` when that
is a valid name, otherwise use a deterministic valid-name digest; preserve
supported selector/profile/priority semantics. Preflight and reject non-lossless
multi-match additive legacy selections before **any** write. Replays and
network-partial retries use idempotent ensure/apply, not destructive rollback;
legacy data is never deleted. Restart and force-recreation against the same
durable directory must retain revision and assignment UIDs, pointers and
rendered artifacts. Backup/restore uses matching binary/data versions; there
is no downgrade guarantee. Only one service process owns a file data directory.

## Contract-to-scenario coverage

`scenarios.json` is the machine-readable manifest; its `clauses` arrays map
each scenario to the frozen plan clauses. Clauses 1–13 must all appear and
each scenario's `expect` is a mandatory assertion, not an optional label.
Together the lifecycle, safety, SMD/cache and persistence cases freeze the
#68–73 acceptance oracle. The baseline must fail for unsupported new behavior,
not skip; later candidate runs must use the unmodified oracle. No container or
live service result is claimed by this documentation/data-only task.
