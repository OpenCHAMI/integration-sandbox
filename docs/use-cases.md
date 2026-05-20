# Use cases

Three named scenarios. Each runnable in isolation. xname ranges are disjoint so they don't collide on shared state.

| Suite | xname range | runtime | Make target |
|---|---|---|---|
| UC1 | `x0c0s0b0n0`–`x0c0s3b0n0` | <100 ms | `make uc1` |
| UC2 | `x0c0s4b0n0`–`x0c0s7b0n0` | <100 ms | `make uc2` |
| UC3 | `x9c0s0b0n0`–`x9c0s7b0n0` | ~12 s | `make uc3` |
| All three back-to-back | — | ~12 s | `make uc-all` |

## UC1 — Populate SMD, verify visibility downstream

**Goal:** assert that nodes registered in SMD become visible through both boot-service and metadata-service surfaces.

**File:** `tests/integration/uc1_node_visibility_test.go`

**Reality check before reading the test:** as of the May 2026 surface, boot-service `/nodes` and metadata-service `/groups` are **independent stores** — they do not auto-sync from SMD. The use case therefore validates "manual fan-out works end-to-end and each service exposes the right shape," not "SMD writes auto-propagate." The cloud-init lookup *does* cross the SMD/metadata boundary at request time and is exercised end-to-end.

**Steps:**
1. SMD: bulk-POST `Components` for 4 xnames, then per-NIC POST `EthernetInterfaces` (so `X-Forwarded-For` lookup will work later).
2. SMD: GET `/State/Components?type=Node`, assert all 4 xnames present.
3. boot-service: hermetic reset + create one Node per xname (`bootResetByName` + `bootCreateNode`).
4. boot-service: GET `/nodes`, assert all 4 xnames present.
5. metadata-service: POST a sandbox Group with template `#cloud-config\nhostname: {{ name }}`.
6. metadata-service: GET `/groups`, assert the group appears in the list.
7. metadata-service: GET `/user-data` with `X-Forwarded-For: <node IP>`. Service queries SMD for the matching xname, then renders the group template. Assert response is `#cloud-config…`.

**Cleanup:** skipped by default. `SBX_UC_CLEANUP=1 make uc1` to delete fixtures.

## UC2 — Two clusters, move a node

**Goal:** model two OpenCHAMI clusters with disjoint node sets, then move one node across the boundary, and verify the membership change propagated.

**File:** `tests/integration/uc2_multi_cluster_test.go`

**Cluster model:** the operator's "cluster" is a separate namespace + DB. The integration sandbox represents clusters via Node `spec.groups` plus metadata-service `Group` resources. To exercise true SMD isolation (two SMD instances) you can layer in `compose/multi-smd.yaml` (planned, not yet shipped) and run with `SBX_MULTI_SMD=1`.

**Steps:**
1. metadata-service: create groups `uc2-alpha` and `uc2-beta`.
2. boot-service: hermetic reset + create 4 Nodes — `x0c0s4`/`x0c0s5` in alpha, `x0c0s6`/`x0c0s7` in beta.
3. Verify initial membership: GET `/nodes`, group each by xname, assert each node is in its declared cluster.
4. Move `x0c0s6b0n0` from beta to alpha via `bootSetGroups` (resolves uid → PUT `/nodes/{uid}` with updated `spec.groups`).
5. Re-verify: alpha=3, beta=1.

**Why uid resolution matters:** boot-service `/nodes/{uid}` is keyed by `uid` (e.g. `node-08a0c74f`), not by `metadata.name`. `bootSetGroups` does the lookup transparently. See [troubleshooting](troubleshooting.md) for the trap if you bypass it.

## UC3 — Restart resilience

**Goal:** simulate a Kubernetes deployment rollout (every container restarts) and confirm the registration round-trip still works after each restart.

**File:** `tests/integration/uc3_restart_resilience_test.go`

**Iteration:** for each service in `restartTargets`:
```
smd → tokensmith → boot-service → metadata-service →
fru-tracker → power-control → vault → postgres
```
the test:
1. `docker compose restart <service>`.
2. Wait for the service's health endpoint to recover (60 s deadline).
3. Re-wait for SMD specifically (everything depends on it).
4. Register a fresh xname in SMD.
5. Register the same xname in boot-service (hermetic reset first).
6. Verify SMD by xname, boot-service by uid, metadata-service group by list-and-search.

**Why bats has to run before integration:** UC3 restarts vault. Vault is in dev mode (in-memory), so the seeded secrets vanish. The bats suite asserts on those secrets and would fail if it ran after UC3. `make ci` runs bats first, then integration; don't reorder.

**Why postgres has no health URL:** Postgres has no HTTP, so the test waits for SMD to come back as the proxy "DB is up" signal. Postgres is the last service in the iteration so its restart effectively re-tests every dependent service in one go.

## Adding a new use case

1. New file `tests/integration/uc<N>_<name>_test.go` with the `//go:build integration` header.
2. Reserve a fresh xname range so it doesn't collide with UC1/UC2/UC3.
3. Use `bootResetByName` + `bootCreateNode` instead of bare POST. boot-service does not enforce name uniqueness; without reset, re-runs accumulate duplicates.
4. Add a Make target:
   ```make
   uc<N>: ## brief description
   	@cd tests && go test -tags integration -count=1 -v -timeout 5m -run '^TestUC<N>_' ./integration/...
   ```
5. Append to `uc-all`.
6. Update this file with steps and rationale.
7. Run `make uc-all` twice to confirm idempotency.
