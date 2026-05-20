#!/usr/bin/env bash
# Bring up the sandbox: infra → bmc-sim → core, with health waits between layers.
# Sources the active image manifest so docker compose substitution sees the right tags.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

# shellcheck disable=SC1091
source "$ROOT/scripts/load-images.sh"

bash scripts/heartbeat.sh up-starting "compose up: infra"
docker compose -f compose/infra.yaml up -d
WAIT_TIMEOUT_S=60 bash scripts/wait-for-stack.sh || true

bash scripts/heartbeat.sh up-starting "compose up: bmc-sim"
docker compose -f compose/bmc-sim.yaml up -d

bash scripts/heartbeat.sh up-starting "compose up: core"
docker compose -f compose/infra.yaml -f compose/bmc-sim.yaml -f compose/core.yaml up -d

bash scripts/heartbeat.sh up-waiting "polling /health on every service"
bash scripts/wait-for-stack.sh

bash scripts/heartbeat.sh up "stack is healthy"
