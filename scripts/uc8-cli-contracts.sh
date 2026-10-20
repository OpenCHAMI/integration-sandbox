#!/usr/bin/env bash
set -euo pipefail
# Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
# SPDX-License-Identifier: MIT
cd "$(dirname "$0")/.."
log_dir="logs/$(date -u +%Y%m%dT%H%M%SZ)/uc8-cli-contracts"
mkdir -p "$log_dir"
exec > >(tee "$log_dir/uc8-cli-contracts.log") 2>&1
source images/pr162/commits.env
export SBX_SKIP_PREFLIGHT=1
finish() {
  rc=$?
  bash scripts/heartbeat.sh "cli-contracts-$rc" "Real CLI fixture contracts exit $rc; $log_dir"
  exit "$rc"
}
trap finish EXIT
result=0
for major in 3 4; do
  export SBX_CLI_JWX_MAJOR="$major"
  if [[ "$major" == 3 ]]; then commit="$SBX_OCHAMI_BASELINE_COMMIT"; else commit="$SBX_OCHAMI_MIGRATED_COMMIT"; fi
  export SBX_OCHAMI_IMAGE="openchami/ochami-sandbox:$commit"
  docker image inspect "$SBX_OCHAMI_IMAGE" --format '{{.Id}} {{json .RepoDigests}}' > "$log_dir/v$major-image.txt"
  if (cd tests && go test -tags integration -race -count=1 -v -timeout 5m -run '^TestUC8_(TokenLocalValidation|TokenFixtureRegression|ConfigurationContracts|FailureContracts|RunnerResult|InputFiles|CLITimeout|InFlightCancellation|LegacyCloudInitReads|MetadataCleanupOwnership|DiagnosticRedaction|Cleanup.*)$' ./integration/...) 2>&1 | tee "$log_dir/v$major-tests.log"; then
    printf 'v%s PASS\n' "$major" >> "$log_dir/outcomes.txt"
  else
    result=1; printf 'v%s FAIL\n' "$major" >> "$log_dir/outcomes.txt"
  fi
done
exit "$result"
