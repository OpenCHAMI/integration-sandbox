# Fixture reference

Every file under `fixtures/`, what it does, and when it's applied.

## Auto-applied (by `make seed`)

### `pg-init.sql`
Bash-mounted into the postgres container at `/docker-entrypoint-initdb.d/00-init.sql`. Runs **once** on first boot of the postgres container. Creates per-service databases:
```sql
CREATE DATABASE smd;
CREATE DATABASE boot;
CREATE DATABASE metadata;
CREATE DATABASE tokensmith;
CREATE DATABASE power;
CREATE DATABASE fru;
```
Not idempotent on its own — but `CREATE DATABASE` is gated by postgres's first-boot script execution. Re-applying after the first boot is a no-op (the script doesn't run).

### `vault-seed.sh`
Idempotent. On every `make seed`:
1. Enables KV-v2 at path `openchami/` (no-op if already enabled).
2. Enables KV-v1 at path `hms-creds/` (no-op if already enabled).
3. Writes cluster-wide secrets at `openchami/sandbox/db/credentials`, `openchami/sandbox/s3/versitygw`, `openchami/sandbox/s3/logs`, `openchami/sandbox/oidc/tokensmith-client`.
4. Writes per-xname BMC creds at `hms-creds/<xname>` for all 8 xnames (`Username=root`, `Password=root_password`).
5. Writes a console SSH key entry at `hms-creds/bmc-console-keys`.

Override env: `VAULT_ADDR`, `VAULT_TOKEN`, `CLUSTER`. Defaults match the compose file.

### `s3-buckets.sh`
Idempotent. Runs `awslocal` *inside* the localstack container (no host awscli needed).
1. Creates buckets `boot-images`, `openchami-logs`, `parquet` (skips if present).
2. Uploads sentinel objects to `s3://boot-images/`:
   - `sandbox.ipxe` — minimal iPXE script.
   - `cloud-init.yaml` — minimal cloud-config template.

Override env: `LOCALSTACK_CONTAINER` (default `sandbox-localstack`).

### `seed-smd.sh`
Idempotent (SMD's bulk endpoints are upsert).
1. POST `smd-components.json` → `/hsm/v2/State/Components`. Accepts 200/201/204.
2. POST `redfish-endpoints.json` → `/hsm/v2/Inventory/RedfishEndpoints`. Accepts 200/201/204.

Override env: `SMD_URL` (default `http://127.0.0.1:27779`).

## Reference data (consumed by tests, not auto-applied)

### `smd-components.json`
The 8 fake nodes. Pushed to SMD by `seed-smd.sh`.
```json
{ "Components": [
  { "ID":"x0c0s0b0n0", "Type":"Node", "State":"On",  "NID":1000, ... },
  …
]}
```

### `redfish-endpoints.json`
The 8 fake BMCs. Pushed to SMD by `seed-smd.sh`.
```json
{ "RedfishEndpoints": [
  { "ID":"x0c0s0b0", "FQDN":"x0c0s0b0", "User":"root", "Password":"root_password", ... },
  …
]}
```

### `boot-configs.json`
A default boot config plus two per-node overrides. Reference data — not currently applied by `make seed`. Use it from a future test that exercises the boot-service `/bootconfigurations` endpoint.

### `inventory-snapshot.json`
A synthetic Redfish discovery snapshot for fru-tracker reconciler tests. Reference data — not auto-applied. Future tests can POST it to `/discoverysnapshots`.

### `tokensmith-config.json`
A reference tokensmith config file. Not currently used because the sandbox bypasses the tokensmith entrypoint (which insists on this file). Kept here so an alternative compose layout that uses the published entrypoint can mount it.

## Adding a fixture

See [extending.md](extending.md) — the process is "drop a file, add to `make seed` if applicable, document here."

## Things you should NOT put in fixtures

- **Secrets that aren't sandbox-only.** Every credential here is published in the docs; if it has to stay private, it doesn't belong in this directory.
- **Large binaries.** Anything over a few KB belongs in `s3-buckets.sh` (uploaded to localstack) or in the relevant service's own test data.
- **Live SQL data.** Use a script that POSTs through the service's API. The harness avoids reaching directly into postgres on principle.
