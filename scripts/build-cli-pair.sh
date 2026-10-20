#!/usr/bin/env bash
set -euo pipefail
# Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
# SPDX-License-Identifier: MIT
cd "$(dirname "$0")/.."
log_dir="logs/$(date -u +%Y%m%dT%H%M%SZ)/build-cli-pair"
mkdir -p "$log_dir" .cache/ochami
exec > >(tee "$log_dir/build-cli-pair.log") 2>&1
source images/pr162/commits.env
source_repo="${SBX_OCHAMI_SOURCE:-https://github.com/OpenCHAMI/ochami.git}"
finish() {
  rc=$?
  if (( rc != 0 )); then
    docker compose -f compose/infra.yaml -f compose/bmc-sim.yaml -f compose/core.yaml ps > "$log_dir/compose-ps.log" 2>&1 || true
    docker ps -a > "$log_dir/docker-ps.log" 2>&1 || true
    docker compose -f compose/infra.yaml -f compose/bmc-sim.yaml -f compose/core.yaml logs --tail=200 > "$log_dir/services.log" 2>&1 || true
    printf 'Pinned CLI build failed; inspect %s; service tests not started.\n' "$log_dir" > BLOCKED.md
  fi
  bash scripts/heartbeat.sh "cli-build-$rc" "Pinned CLI pair build exit $rc; $log_dir"
  exit "$rc"
}
trap finish EXIT
bash scripts/heartbeat.sh cli-build-starting 'Building pinned v3/v4 CLI inputs'
for variant in baseline migrated; do
  if [[ "$variant" == baseline ]]; then commit="$SBX_OCHAMI_BASELINE_COMMIT"; else commit="$SBX_OCHAMI_MIGRATED_COMMIT"; fi
  context=".cache/ochami/$commit"
  image="openchami/ochami-sandbox:$commit"
  mkdir -p "$context"
  if [[ ! -d "$context/.git" ]]; then git init -q "$context"; fi
  if ! git -C "$context" cat-file -e "$commit^{commit}" 2>/dev/null; then
    timeout 120 git -C "$context" fetch --no-tags "$source_repo" "$commit"
  fi
  [[ "$(git -C "$context" rev-parse "$commit^{commit}")" == "$commit" ]]
  # Refuse dirty cached inputs rather than resetting anybody's work.
  [[ -z "$(git -C "$context" status --porcelain)" ]]
  git -C "$context" checkout --detach "$commit"
  if docker image inspect "$image" >/dev/null 2>&1; then
    [[ "$(docker image inspect "$image" --format '{{index .Config.Labels "org.opencontainers.image.revision"}}')" == "$commit" ]]
  else
    if [[ "${SBX_OCHAMI_BUILD_MODE:-docker}" == host ]]; then
      # Optional offline-friendly cross-build. Use one explicit toolchain for
      # both revisions and record it; never silently substitute a version.
      toolchain="${SBX_OCHAMI_GO_TOOLCHAIN:?host mode requires an explicit Go toolchain}"
      architecture="$(docker version --format '{{.Server.Arch}}')"
      env GOTOOLCHAIN="$toolchain" go version | tee "$log_dir/$variant-toolchain.txt"
      (cd "$context" && timeout 600 env GOTOOLCHAIN="$toolchain" GOOS=linux GOARCH="$architecture" CGO_ENABLED=0 make VERSION=pr162 TAG=pr162 BRANCH=detached BUILD="$commit" GITSTATE=clean)
      timeout 120 docker build -f compose/Dockerfile.ochami-binary --label "org.opencontainers.image.revision=$commit" -t "$image" "$context"
    else
      timeout 1200 docker build -f compose/Dockerfile.ochami-pr162 --build-arg "SOURCE_COMMIT=$commit" --label "org.opencontainers.image.revision=$commit" -t "$image" "$context"
    fi
  fi
  docker image inspect "$image" --format '{{.Id}} {{json .RepoDigests}}' | tee "$log_dir/$variant-image.txt"
  docker run --rm --entrypoint /bin/ochami "$image" version | tee "$log_dir/$variant-version.txt"
  # Reject unknown/incorrect runtime provenance even when the OCI label matches.
  grep -F "$commit" "$log_dir/$variant-version.txt"
  actual_go="$(awk '$1 == "Go:" { print $2 }' "$log_dir/$variant-version.txt")"
  [[ -n "$actual_go" ]]
  pair_go="${pair_go:-$actual_go}"
  [[ "$actual_go" == "$pair_go" ]] || { printf 'CLI pair uses different Go toolchains.\n'; exit 1; }
  printf 'SBX_OCHAMI_IMAGE=%s\n' "$image" > ".cache/ochami/$variant.env"
done
