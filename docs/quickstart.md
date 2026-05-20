# Quickstart

Five minutes from clone to green.

## Prerequisites
You need (versions tested in parens):
- Docker 29.x with the compose plugin v2.
- Go 1.24.6 (Go 1.23 should work; 1.22 won't).
- `make`, `bash`, `bats`, `jq`, `curl`. All standard on Ubuntu 24.04.
- The user running `make` must be in the `docker` group.

You do **not** need: kind, kubectl, helm, awscli on the host. The integration sandbox doesn't use Kubernetes; awscli is invoked inside the localstack container.

## First run

```bash
cd integration-sandbox
make ci
```

`make ci` does build/pull → up → seed → test → down in one shot. First run pulls ~5 GB of images (vault, localstack, postgres, csm-rie, smd, tokensmith, boot-service, metadata-service, fru-tracker, power-control, magellan) and builds two locally (ipmi-sim, power-control). Subsequent runs hit the cache and finish in ~2 minutes.

## What "green" looks like

```
ok  github.com/openchami/integration-sandbox/tests/integration
1..6
ok 1 vault status reports unsealed
ok 2 vault has openchami/sandbox/db/credentials
ok 3 vault has hms-creds/x0c0s0b0
ok 4 localstack has the boot-images bucket
ok 5 localstack boot-images contains sandbox.ipxe
ok 6 smd /service/ready returns 200
```

`make ci` exits 0. `cat STATUS` reads `ci-passed`.

## Iteration loop

Don't tear down between edits. Use the layered targets:

```bash
make up         # bring stack up (idempotent)
make seed       # populate vault/s3/smd
make test       # run all tests against the running stack
# edit, repeat: just `make test`
make down       # when finished
```

For a single use case:
```bash
make uc1        # only UC1
make uc2        # only UC2
make uc3        # only UC3 (restarts containers; takes ~12s)
```

## Phone-friendly observation

```bash
make tail
```
Streams `STATUS` + `PROGRESS.log`. Append-only, monotonic timestamps. Useful when running unattended.

## When something breaks

`make ci` traps failures and writes a forensic bundle to `logs/<UTC>-ci-failure/`:
- `state.txt` — `docker ps`, `df -h`, `free -h`, `docker stats`.
- `log-<container>.txt` — last 500 lines from each running container.

Open the bundle, grep for the failing assertion, and check the matching service log. See [troubleshooting](troubleshooting.md) for the failure modes we've already hit.
