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

# Wait for infra to stabilise before stacking more containers on the
# runner — gives vault/localstack their CPU window before the 8 sushy-
# tools containers + 6 core services come up and start contending.
WAIT_PER_TIMEOUT_S=60 bash scripts/wait-for-stack.sh infra

# Gate the ipmi_sim service (compose/bmc-sim.yaml::ipmi-bmc-0 has
# `profiles: ["ipmi"]`) on SKIP_SIM and image availability. If the image
# is missing and cannot be pulled, skip ipmi instead of hard-failing `up`.
if [[ -z "${COMPOSE_PROFILES:-}" ]]; then
  if [[ "${SKIP_SIM:-false}" != "true" ]]; then
    if docker image inspect "$SBX_IPMI_SIM_IMAGE" >/dev/null 2>&1; then
      export COMPOSE_PROFILES="ipmi"
    elif timeout 30 docker pull --quiet "$SBX_IPMI_SIM_IMAGE" >/dev/null 2>&1; then
      export COMPOSE_PROFILES="ipmi"
    else
      bash scripts/heartbeat.sh up-warning "ipmi image unavailable; continuing with SKIP_SIM=true"
      echo "up: WARN: IPMI image unavailable ($SBX_IPMI_SIM_IMAGE); continuing without ipmi profile" >&2
    fi
  fi
fi

bash scripts/heartbeat.sh up-starting "compose up: bmc-sim (profiles=${COMPOSE_PROFILES:-<none>})"
docker compose -f compose/bmc-sim.yaml up -d

bash scripts/heartbeat.sh up-starting "compose up: core (profiles=${COMPOSE_PROFILES:-<none>})"
docker compose -f compose/infra.yaml -f compose/bmc-sim.yaml -f compose/core.yaml up -d

bash scripts/heartbeat.sh up-waiting "polling /health on every service (per-endpoint deadline)"
bash scripts/wait-for-stack.sh all

bash scripts/heartbeat.sh up "stack is healthy"
