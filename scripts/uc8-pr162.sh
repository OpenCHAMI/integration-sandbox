#!/usr/bin/env bash
set -euo pipefail
# Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
# SPDX-License-Identifier: MIT
cd "$(dirname "$0")/.."
log_dir="logs/$(date -u +%Y%m%dT%H%M%SZ)/uc8-pr162"
mkdir -p "$log_dir"
exec > >(tee "$log_dir/uc8-pr162.log") 2>&1
# This target adopts no stack and never starts/stops/reseeds services.
if [[ "${SBX_DEDICATED_RUNNER:-}" != 1 ]]; then
  printf 'Set SBX_DEDICATED_RUNNER=1 on the dedicated validation runner. Existing stacks are never adopted implicitly.\n' >&2
  exit 1
fi
source scripts/load-images.sh
source images/pr162/commits.env
compose=(docker compose -f compose/infra.yaml -f compose/bmc-sim.yaml -f compose/core.yaml)
finish() {
  rc=$?
  if (( rc != 0 )); then
    "${compose[@]}" ps > "$log_dir/compose-ps.log" 2>&1 || true
    docker ps -a > "$log_dir/docker-ps.log" 2>&1 || true
    "${compose[@]}" logs --tail=200 > "$log_dir/services.log" 2>&1 || true
    printf 'PR162 comparison failed; inspect %s. Existing stack preserved.\n' "$log_dir" > BLOCKED.md
  fi
  bash scripts/heartbeat.sh "pr162-comparison-$rc" "Comparison exit $rc; $log_dir"
  exit "$rc"
}
trap finish EXIT
bash scripts/heartbeat.sh pr162-comparison-starting 'Checking healthy dedicated stack; no reseeding'
bash scripts/wait-for-stack.sh all
docker exec sandbox-postgres pg_isready -U openchami -d smd
"${compose[@]}" images > "$log_dir/service-images.txt"
# Record immutable runtime identities, not only selected tags. No env/key dumps.
check_service_inputs() {
  for service in postgres smd tokensmith boot metadata power fru; do
    case "$service" in
      postgres) selected="$SBX_POSTGRES_IMAGE" ;; smd) selected="$SBX_SMD_IMAGE" ;;
      tokensmith) selected="$SBX_TOKENSMITH_IMAGE" ;; boot) selected="$SBX_BOOT_IMAGE" ;;
      metadata) selected="$SBX_METADATA_IMAGE" ;; power) selected="$SBX_POWER_IMAGE" ;; fru) selected="$SBX_FRU_IMAGE" ;;
    esac
    desired="$(docker image inspect "$selected" --format '{{.Id}}')"
    actual="$(docker inspect "sandbox-$service" --format '{{.Image}}')"
    [[ "$desired" == "$actual" ]] || { printf 'Running %s differs from selected %s; refusing drift.\n' "$service" "$selected"; return 1; }
  done
}
check_service_inputs
for service in postgres smd tokensmith boot metadata power fru; do
  docker inspect "sandbox-$service" --format '{{.Name}} {{.Image}} {{.Config.Image}}' >> "$log_dir/service-identities.txt"
done
result=0
for variant in baseline migrated; do
  if [[ "$variant" == baseline ]]; then commit="$SBX_OCHAMI_BASELINE_COMMIT"; else commit="$SBX_OCHAMI_MIGRATED_COMMIT"; fi
  export SBX_OCHAMI_IMAGE="openchami/ochami-sandbox:$commit"
  if [[ "$variant" == baseline ]]; then export SBX_CLI_JWX_MAJOR=3; else export SBX_CLI_JWX_MAJOR=4; fi
  [[ "$(docker image inspect "$SBX_OCHAMI_IMAGE" --format '{{index .Config.Labels "org.opencontainers.image.revision"}}')" == "$commit" ]]
  docker image inspect "$SBX_OCHAMI_IMAGE" --format '{{.Id}} {{json .RepoDigests}}' > "$log_dir/$variant-image.txt"
  for attempt in 1 2; do
    check_service_inputs
    bash scripts/wait-for-stack.sh all
    # Failures remain nonzero but do not suppress the other revision's evidence.
    if (cd tests && go test -tags integration -count=1 -v -timeout 25m -run '^Test(UC4_|UC8_)' ./integration/...) 2>&1 | tee "$log_dir/$variant-run$attempt.log"; then
      printf '%s run%s PASS\n' "$variant" "$attempt" >> "$log_dir/outcomes.txt"
    else
      printf '%s run%s FAIL\n' "$variant" "$attempt" >> "$log_dir/outcomes.txt"
      result=1
    fi
  done
done
exit "$result"
