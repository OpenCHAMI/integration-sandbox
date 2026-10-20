#!/usr/bin/env bash
set -euo pipefail
# Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
# SPDX-License-Identifier: MIT
cd "$(dirname "$0")/.."
log_dir="logs/$(date -u +%Y%m%dT%H%M%SZ)/pr162-ci"
mkdir -p "$log_dir"
exec > >(tee "$log_dir/pr162-ci.log") 2>&1
[[ "${SBX_DEDICATED_RUNNER:-}" == 1 ]] || { printf 'Dedicated runner required.\n'; exit 1; }
source scripts/load-images.sh
source images/pr162/commits.env
[[ -z "$(docker ps -aq --filter label=com.docker.compose.project=openchami-sandbox)" ]] || { printf 'Refusing existing sandbox project.\n'; exit 1; }
for port in 8200 4566 27779 27780 27791 27792 27793 28007 5000; do
  if [[ -n "$(docker ps -q --filter publish="$port")" ]] || nc -z 127.0.0.1 "$port"; then printf 'Refusing occupied port %s\n' "$port"; exit 1; fi
done
bash scripts/build-cli-pair.sh
export SBX_OCHAMI_IMAGE="openchami/ochami-sandbox:$SBX_OCHAMI_MIGRATED_COMMIT"
export SKIP_SIM=true
run_core() (
  set -e
  owned=false
  cleanup() {
    rc=$?
    if (( rc != 0 )); then bash scripts/log-bundle.sh pr162-ci-failure || true; fi
    if [[ "$owned" == true ]]; then
      docker compose -f compose/infra.yaml -f compose/bmc-sim.yaml -f compose/core.yaml down -v --remove-orphans || rc=1
    fi
    exit "$rc"
  }
  trap cleanup EXIT
  bash scripts/build-images.sh
  owned=true
  bash scripts/up.sh
  bash fixtures/vault-seed.sh
  bash fixtures/s3-buckets.sh
  bash fixtures/seed-smd.sh
  printf 'ready\n' > "$log_dir/core-ready"
  core_result=0
  bash scripts/uc8-cli-apis.sh pair || core_result=1
  (cd tests && go test -tags integration -count=1 -v -timeout 20m -run '^TestUC[56]_' ./integration/...) || core_result=1
  exit "$core_result"
)
result=0
set +e
run_core
core_rc=$?
set -e
if [[ ! -f "$log_dir/core-ready" ]]; then exit "$core_rc"; fi
if (( core_rc == 0 )); then
  printf 'Core comparison and interactions PASS\n' > "$log_dir/outcomes.txt"
else
  result=1
  printf 'Core comparison or readiness FAIL\n' > "$log_dir/outcomes.txt"
fi
export SBX_CLI_JWX_MAJOR=4
if UC8_AUTH_SERVICE=bss bash scripts/uc8-auth.sh; then
  printf 'BSS enforcement PASS\n' >> "$log_dir/outcomes.txt"
else
  result=1; printf 'BSS enforcement FAIL\n' >> "$log_dir/outcomes.txt"
fi
if [[ -n "${SBX_ENFORCING_BOOT_IMAGE:-}" ]]; then
  [[ "$SBX_ENFORCING_BOOT_IMAGE" == *@sha256:* ]] || { printf 'Enforcing boot image must be immutable.\n'; exit 1; }
  if SBX_BOOT_IMAGE="$SBX_ENFORCING_BOOT_IMAGE" UC8_AUTH_SERVICE=boot bash scripts/uc8-auth.sh; then
    printf 'Boot enforcement PASS\n' >> "$log_dir/outcomes.txt"
  else
    result=1; printf 'Boot enforcement FAIL\n' >> "$log_dir/outcomes.txt"
  fi
else
  printf 'Boot enforcement BLOCKED: immutable enforcing image required.\n' >> "$log_dir/outcomes.txt"
  result=1
fi
bash scripts/heartbeat.sh "pr162-ci-$result" "PR162 validation exit $result; $log_dir"
exit "$result"
