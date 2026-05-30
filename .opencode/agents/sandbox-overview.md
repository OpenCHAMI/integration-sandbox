---
description: >-
  Gives a concise, high-level overview of the OpenCHAMI integration sandbox:
  its purpose, major components, and the architectural layout of the Docker-Compose
  stack.
mode: subagent
---

# OpenCHAMI Integration Sandbox – Overview

**What it is**  
A Docker-Compose-driven, BMC-less end-to-end test harness for the OpenCHAMI
micro-services. It provides a single-command (`make ci`) that spins up a full
service stack, seeds it with fixtures, runs a suite of integration tests, and
tears everything down. No Kubernetes or operator code is involved.

**Why it exists**  
To let any OpenCHAMI service repository validate its PR build against the rest
of the fleet without needing real hardware. It exercises real service images
(pulled from `ghcr.io/openchami/*`) and simulated infrastructure (Vault dev,
LocalStack S3, PostgreSQL, Redfish BMC emulators, IPMI simulator).

**Key Docker-Compose layers**

| Compose file                | Services it starts                                   |
|----------------------------|------------------------------------------------------|
| `compose/infra.yaml`       | Vault dev, LocalStack S3, PostgreSQL                 |
| `compose/bmc-sim.yaml`     | 8 `csm-rie` Redfish emulators (`x0c0s0b0`…`x0c0s7b0`) + 1 `ipmi_sim` |
| `compose/core.yaml`        | SMD, tokensmith, boot-service, metadata-service, fru-tracker, power-control, magellan-runner |

**Test suite** (`tests/integration/`) – a Go module (build tag `integration`) that runs seven named use-cases (UC1-UC7). Each UC validates cross-service flows such as node visibility, token exchange, Redfish discovery, power state transitions, and FRU reconciliation.

**Supporting assets**

* **Fixtures** – JSON and shell scripts in `fixtures/` (Vault seeds, S3 bucket setup, SMD component data, boot configs, inventory snapshots, Redfish endpoint mocks).  
* **Scripts** – `scripts/` contains helpers (`up.sh`, `down.sh`, `wait-for-stack.sh`, `log-bundle.sh`, `heartbeat.sh`, `build-images.sh`, etc.) that obey the idempotency and logging conventions described in `.claude/CLAUDE.md`.  
* **BATS smoke tests** – `tests/bats/cli-smoke.bats` validates the CLI entry-points of the services.

**Conventions (do-not-re-invent)**  

* 8 fake nodes: `x0c0s0b0` … `x0c0s7b0`.  
* BMC credentials: `root` / `root_password`.  
* Vault root token: `dev-root-token`.  
* LocalStack S3 bucket names: `boot-images`, `openchami-logs`, `parquet`.  
* Image-tag policy: `make ci` defaults to `IMAGES=release` (pinned to the latest GitHub Release tag). Override per-service with `SBX_<NAME>_IMAGE=…`.

**How to use**  

```bash
make ci          # full end-to-end run (build, up, seed, test, down)
make tail        # phone-friendly tail of PROGRESS.log & STATUS
make show-images # list image manifests that will be used
```

The agent can answer any "what", "where", or "how" question about the sandbox
(e.g., "What does UC5 test?", "Where are the Redfish fixtures stored?",
"Which compose file defines the BMC simulators?").
