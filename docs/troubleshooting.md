# Troubleshooting

Failure modes we've already hit, with the actual fix. Newest at top.

## bats: `vault has openchami/sandbox/db/credentials` fails

**Symptom:** integration tests passed, then bats fails on `vault kv get …`.
**Cause:** UC3 restarts vault. Vault is in dev mode (in-memory), so seeded secrets vanish. If bats runs *after* UC3, the seed assertions fail.
**Fix:** `make ci` runs bats first. If you've reordered or added a new bats test, keep that ordering or call `bash fixtures/vault-seed.sh` between integration and bats.

## boot-service test gets duplicate entries / stale groups

**Symptom:** `verify_initial_membership` fails with `[]string{"beta"} does not contain "uc2-alpha"` even though the test just POSTed `[uc2-alpha]`.
**Cause:** boot-service does not enforce `metadata.name` uniqueness. A re-run without cleanup leaves duplicates; lookups by name return the first match.
**Fix:** always `bootResetByName(t, bootURL, xname)` before `bootCreateNode`. The helpers are in `tests/integration/clientutil_test.go`.

## boot-service / metadata-service PUT returns 404

**Symptom:** `PUT /nodes/x0c0s4b0n0` → 404 even though a Node with that name exists.
**Cause:** the path parameter is `uid` (e.g. `node-08a0c74f`), not `metadata.name`/`xname`.
**Fix:** look up uid first (`bootNodeUIDsByName`) then PUT to `/nodes/<uid>`. `bootSetGroups` does this for groups updates. Same trap applies to `metadata-service /groups/{uid}`.

## csm-rie healthcheck fails — `Recv failure: Connection reset by peer`

**Symptom:** `curl http://127.0.0.1:5000/redfish/v1` connects but the request resets.
**Cause:** csm-rie runs Flask on **HTTPS** with a self-signed cert, not HTTP. The container log says `* Use HTTPS`.
**Fix:**
- healthcheck: `curl -fsk https://127.0.0.1:5000/redfish/v1`
- tests: HTTPS URL plus `tls.Config{InsecureSkipVerify: true}` on the http client (see `suite_test.go`).

## SMD: `Missing DB port number`

**Symptom:** SMD container loops with that error.
**Cause:** SMD reads CLI flags only — env vars are ignored. With only `-db-dsn` set, SMD still demands `-dbport`.
**Fix:** pass per-flag config in `compose/core.yaml`:
```yaml
command:
  - "/smd"
  - "-dbhost=postgres"
  - "-dbport=5432"
  - "-dbuser=openchami"
  - "-dbname=smd"
  - "-dbtype=postgres"
  - "-dbopts=password=openchami sslmode=disable"
  - "-migrate"
```
SMD has no `-dbpass` flag; password lives in `-dbopts` (libpq keyword form).

## SMD: `Failed to open /etc/cert.pem for writing: permission denied`

**Symptom:** SMD logs that, then `Warning: TLS cert or key file missing, falling back to http`.
**Cause:** SMD tries to auto-generate a self-signed cert into `/etc/cert.pem`. The container user isn't root, so it can't.
**Status:** harmless — SMD falls back to HTTP, which is what we want anyway. Healthcheck and tests use HTTP `http://localhost:27779`.

## SMD: `relation "system" does not exist`

**Symptom:** SMD loops with `pq: relation "system" does not exist` after upgrading or recreating postgres.
**Cause:** SMD didn't run its migrations.
**Fix:** the compose file passes `-migrate`. If you're running SMD outside the sandbox, do the same.

## tokensmith: `failed to read config file: /tokensmith/config.json: no such file or directory`

**Symptom:** tokensmith refuses to start.
**Cause:** the published image's entrypoint always shells out with `--config="$TOKENSMITH_CONFIG"`. Default value points at a file that doesn't exist.
**Fix:** bypass the entrypoint and run the binary directly. See `compose/core.yaml`'s tokensmith block:
```yaml
entrypoint: ["/sbin/tini", "--"]
command: ["tokensmith", "serve", "--port=27780", …]
environment:
  TOKENSMITH_CONFIG: ""
```

## tokensmith: `permission denied` on `/tokensmith/keys/private.pem`

**Symptom:** tokensmith fails immediately on first boot.
**Cause:** the Dockerfile creates `/tokensmith/keys` as root, but the runtime user is UID 65534.
**Fix:** mount `tmpfs` over the directories that need to be writable:
```yaml
tmpfs:
  - /tokensmith/keys:mode=1777
  - /tokensmith/data:mode=1777
```

## fru-tracker: `unable to open database file: no such file or directory`

**Symptom:** fru-tracker keeps restarting.
**Cause:** the image has no `/data` directory and the runtime user can't create it. SQLite's `file:/data/fru.db?…` URL fails.
**Fix:** `tmpfs: ["/data:mode=1777"]` in `compose/core.yaml`. Dev-mode only — production should use a real volume with proper ownership.

## boot-service: `tokensmith bootstrap token is required when both hsm-url and tokensmith_url are set`

**Symptom:** boot-service exits during validation.
**Cause:** boot-service treats this as a hard error even when `BOOT_SERVICE_ENABLE_AUTH=false`.
**Fix:** drop `BOOT_SERVICE_TOKENSMITH_URL` when auth is disabled. The sandbox's `compose/core.yaml` does not set it.

## power-control: `Unable to connect to Vault, err: open /var/run/secrets/kubernetes.io/serviceaccount/namespace: no such file or directory`

**Symptom:** power-control loops indefinitely with that message.
**Cause:** power-control's Vault credstore tries Kubernetes ServiceAccount auth before falling back to direct token. Outside Kubernetes the namespace file doesn't exist.
**Fix:** for non-K8s deployments, set `VAULT_ENABLED=false`. Power-control falls through to in-process state. The sandbox does this by default.

## awslocal on host fails — `[Errno 2] No such file or directory: b'/home/alt/.local/bin/aws'`

**Symptom:** `make seed` errors out at the S3 step.
**Cause:** `awslocal` is a wrapper around `aws`, which isn't installed on the host.
**Fix:** the seed scripts run `awslocal` *inside* the localstack container (`docker exec sandbox-localstack awslocal …`), so the host doesn't need awscli. If you regress this, the symptom returns. Don't.

## docker compose: `external network openchami-sandbox not found`

**Symptom:** bringing up `compose/core.yaml` standalone fails.
**Cause:** core and bmc-sim declare the network as `external: true`. They expect infra (which creates it) to come up first.
**Fix:** `make up` does this in order. If you must bring layers up by hand: infra first, then bmc-sim, then core. Or just create the network manually: `docker network create openchami-sandbox`.

## "Healthcheck never reports healthy" on a distroless image

**Symptom:** `docker compose up` reports a service as `(unhealthy)` indefinitely.
**Cause:** distroless images (boot-service, metadata-service) ship no shell, no curl, no wget. There's no way to write a `HEALTHCHECK` directive that runs inside the container.
**Fix:** the sandbox does NOT define `healthcheck:` in `compose/core.yaml` for those services. `wait-for-stack.sh` polls them from the host instead. Don't add a `HEALTHCHECK` directive — it'll fail immediately.

## bwrap: `Failed RTM_NEWADDR: Operation not permitted`

**Symptom:** any bash command fails with that error after enabling `/sandbox`.
**Cause:** the auto-allow bash sandbox uses bubblewrap, which can't set up its loopback namespace on this VM (kernel hardening / unprivileged userns disabled).
**Fix:** turn off `/sandbox` and use `/permissions` to allow specific bash patterns instead. See the project `.claude/settings.local.json` for the working allowlist.

## Image override doesn't take effect

**Symptom:** `make ci SBX_TOKENSMITH_IMAGE=...` runs against the manifest tag, not your override.
**Cause:** typo in the variable name, or you set it inside a sub-shell that doesn't propagate to make.
**Fix:** verify with `make show-images SBX_TOKENSMITH_IMAGE=...` first. If `show-images` shows the right tag, the run will use it. If not, check the variable name (must start `SBX_`) and that it's exported / on the make command line.

## "Tests run twice as long the second time" or "make ci hangs at down"

**Symptom:** `make ci` runtime drifts upward across runs, or the down step never finishes.
**Cause:** containers from a prior run still around (often from `Ctrl-C` mid-test).
**Fix:** `make reset`. Or aggressive: `docker rm -f $(docker ps -aq --filter "name=sandbox-")`.
