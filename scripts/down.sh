#!/usr/bin/env bash
# Tear down the sandbox idempotently.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

# shellcheck disable=SC1091
source "$ROOT/scripts/load-images.sh" 2>/dev/null || true

bash scripts/heartbeat.sh down-starting "compose down -v (all stacks)"

docker compose -f compose/infra.yaml -f compose/bmc-sim.yaml -f compose/core.yaml down -v --remove-orphans 2>&1 || true
docker compose -f compose/bmc-sim.yaml down -v --remove-orphans 2>&1 || true
docker compose -f compose/infra.yaml down -v --remove-orphans 2>&1 || true

docker network rm openchami-sandbox 2>/dev/null || true

bash scripts/heartbeat.sh down "teardown complete"
