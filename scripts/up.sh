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

# infra.yaml is the one place the openchami-sandbox network is auto-created;
# bmc-sim.yaml and core.yaml mark it external: true. If it didn't come up,
# the cryptic "network ... declared as external" error downstream would
# bury the real problem.
if ! docker network inspect openchami-sandbox >/dev/null 2>&1; then
  echo "up: openchami-sandbox network was not created by compose/infra.yaml" >&2
  docker compose -f compose/infra.yaml ps >&2 || true
  exit 1
fi

WAIT_TIMEOUT_S=60 bash scripts/wait-for-stack.sh || true

# Gate the ipmi_sim service (compose/bmc-sim.yaml::ipmi-bmc-0 has
# `profiles: ["ipmi"]`) on SKIP_SIM. With SKIP_SIM=true the build is
# skipped and the image won't pull; the profile keeps compose from trying.
if [[ -z "${COMPOSE_PROFILES:-}" ]]; then
  if [[ "${SKIP_SIM:-false}" != "true" ]]; then
    export COMPOSE_PROFILES="ipmi"
  fi
fi

bash scripts/heartbeat.sh up-starting "compose up: bmc-sim (profiles=${COMPOSE_PROFILES:-<none>})"
docker compose -f compose/bmc-sim.yaml up -d

bash scripts/heartbeat.sh up-starting "compose up: core (profiles=${COMPOSE_PROFILES:-<none>})"
docker compose -f compose/infra.yaml -f compose/bmc-sim.yaml -f compose/core.yaml up -d

bash scripts/heartbeat.sh up-waiting "polling /health on every service"
bash scripts/wait-for-stack.sh

bash scripts/heartbeat.sh up "stack is healthy"
