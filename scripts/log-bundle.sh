#!/usr/bin/env bash
# Capture forensic info to logs/<UTC>/.
# Usage: scripts/log-bundle.sh [tag]
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
tag="${1:-bundle}"
ts="$(date -u +%Y%m%dT%H%M%SZ)"
out="$ROOT/logs/${ts}-${tag}"
mkdir -p "$out"
exec > >(tee "$out/log-bundle.log") 2>&1

cd "$ROOT"

{
  echo "=== docker ps -a ==="
  docker ps -a 2>&1 || true
  echo
  echo "=== docker compose ps (all stacks) ==="
  docker compose -f compose/infra.yaml -f compose/bmc-sim.yaml -f compose/core.yaml ps 2>&1 || true
  echo
  echo "=== df -h ==="
  df -h 2>&1 || true
  echo
  echo "=== free -h ==="
  free -h 2>&1 || true
  echo
  echo "=== docker stats (one shot) ==="
  docker stats --no-stream 2>&1 || true
} > "$out/state.txt"

# Per-container logs
docker compose -f compose/infra.yaml -f compose/bmc-sim.yaml -f compose/core.yaml logs --tail=200 > "$out/compose-services.txt" 2>&1 || true

for c in $(docker ps -a --filter label=com.docker.compose.project=openchami-sandbox --format '{{.Names}}'); do
  docker logs --tail=500 "$c" > "$out/log-${c}.txt" 2>&1 || true
done

printf '%s\n' "$out"
