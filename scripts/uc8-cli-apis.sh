#!/usr/bin/env bash
set -euo pipefail
# Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
# SPDX-License-Identifier: MIT
cd "$(dirname "$0")/.."
log_dir="logs/$(date -u +%Y%m%dT%H%M%SZ)/uc8-cli-apis"
mkdir -p "$log_dir"
exec > >(tee "$log_dir/uc8-cli-apis.log") 2>&1
source scripts/load-images.sh
caller_bss="${SBX_BSS_IMAGE:-}"
caller_cloud_init="${SBX_CLOUD_INIT_IMAGE:-}"
source images/pr162/services.env
export SBX_BSS_IMAGE="${caller_bss:-$SBX_BSS_IMAGE}"
export SBX_CLOUD_INIT_IMAGE="${caller_cloud_init:-$SBX_CLOUD_INIT_IMAGE}"
compose=(docker compose -f compose/infra.yaml -f compose/bmc-sim.yaml -f compose/core.yaml -f compose/cli-apis.yaml)
owned=false
finish() {
  rc=$?
  if (( rc != 0 )); then
    "${compose[@]}" ps > "$log_dir/compose-ps.log" 2>&1 || true
    docker ps -a > "$log_dir/docker-ps.log" 2>&1 || true
    "${compose[@]}" logs --tail=200 bss cloud-init > "$log_dir/services.log" 2>&1 || true
    printf 'Legacy CLI APIs failed; inspect %s. Core stack preserved.\n' "$log_dir" > BLOCKED.md
  fi
  if [[ "$owned" == true ]]; then
    "${compose[@]}" rm -sf bss cloud-init || rc=1
  fi
  bash scripts/heartbeat.sh "cli-apis-$rc" "Legacy API profile exit $rc; $log_dir"
  exit "$rc"
}
trap finish EXIT
# Never adopt existing legacy services or take their ports.
for service in bss cloud-init; do
  [[ -z "$("${compose[@]}" ps -aq "$service")" ]] || { printf 'Refusing existing %s\n' "$service"; exit 1; }
done
command -v nc >/dev/null
for port in 27777 27778; do
  if [[ -n "$(docker ps -q --filter publish="$port")" ]] || nc -z 127.0.0.1 "$port"; then
    printf 'Refusing occupied port %s\n' "$port"; exit 1
  fi
done
bash scripts/wait-for-stack.sh core
owned=true
"${compose[@]}" up -d --no-deps bss cloud-init
for uri in http://127.0.0.1:27778/boot/v1/service/status http://127.0.0.1:27777/version; do
  ready=false
  for _attempt in {1..30}; do
    if curl -fsS --max-time 2 "$uri" >/dev/null; then ready=true; break; fi
    sleep 1
  done
  [[ "$ready" == true ]] || { printf 'API readiness failed: %s\n' "$uri"; exit 1; }
done
"${compose[@]}" images bss cloud-init > "$log_dir/images.txt"
export SBX_CLI_APIS_ENABLED=1
if [[ "${1:-}" == pair ]]; then
  bash scripts/uc8-pr162.sh
else
  cd tests
  go test -tags integration -count=1 -v -timeout 10m -run '^TestUC8_LiveLegacyAPIs$' ./integration/...
fi
