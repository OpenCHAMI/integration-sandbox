#!/usr/bin/env bash
# Pull (or build) every image referenced by the active manifest.
# Pull-first policy. Build fallback for power-control (not published) and ipmi-sim
# (always built locally). Caller env vars beat manifest entries.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
WORKSPACE="$(cd "$ROOT/.." && pwd)"
cd "$ROOT"

# shellcheck disable=SC1091
source "$ROOT/scripts/load-images.sh"

bash scripts/heartbeat.sh build-starting "pulling images for IMAGES=${IMAGES:-default}"

# Public images we expect to pull.
PULL_LIST=(
  "$SBX_VAULT_IMAGE"
  "$SBX_LOCALSTACK_IMAGE"
  "$SBX_POSTGRES_IMAGE"
  "$SBX_SUSHY_IMAGE"
  "$SBX_SMD_IMAGE"
  "$SBX_TOKENSMITH_IMAGE"
  "$SBX_BOOT_IMAGE"
  "$SBX_METADATA_IMAGE"
  "$SBX_FRU_IMAGE"
  "$SBX_MAGELLAN_IMAGE"
  "$SBX_POWER_IMAGE"
)

declare -a MISSING=()
for img in "${PULL_LIST[@]}"; do
  printf '[pull] %s\n' "$img"
  if ! timeout 120 docker pull --quiet "$img" >/dev/null 2>&1; then
    printf '[pull] WARN: %s did not pull\n' "$img"
    MISSING+=("$img")
  fi
done

# IPMI sim is built from sibling repo (no public image expected).
# Skip if SKIP_SIM is set to true.
if [[ "${SKIP_SIM:-false}" == "true" ]]; then
  printf '[build] ⚡ Skipping ipmi_sim (SKIP_SIM=true)\n'
elif [ -d "$WORKSPACE/remote-console/ipmi_sim" ]; then
  if ! docker image inspect "$SBX_IPMI_SIM_IMAGE" >/dev/null 2>&1; then
    printf '[build] %s\n' "$SBX_IPMI_SIM_IMAGE"
    docker build -q -t "$SBX_IPMI_SIM_IMAGE" "$WORKSPACE/remote-console/ipmi_sim" >/dev/null
  else
    printf '[build] %s already present\n' "$SBX_IPMI_SIM_IMAGE"
  fi
fi

# power-control is not (yet) published. If the pull failed and a sibling repo
# exists, build from source and tag with whatever name the manifest expects.
# Skip if SKIP_SIM is set to true (power-control depends on remote-console).
if [[ "${SKIP_SIM:-false}" == "true" ]]; then
  printf '[build] ⚡ Skipping power-control (SKIP_SIM=true)\n'
elif printf '%s\n' "${MISSING[@]}" | grep -qx "$SBX_POWER_IMAGE"; then
  if [ -d "$WORKSPACE/power-control" ]; then
    printf '[build] %s (from power-control/Dockerfile.build)\n' "$SBX_POWER_IMAGE"
    docker build -q \
      -f "$WORKSPACE/power-control/Dockerfile.build" \
      -t "$SBX_POWER_IMAGE" \
      "$WORKSPACE/power-control" >/dev/null
  else
    printf '[build] WARN: power-control missing both image and source\n'
  fi
fi

bash scripts/heartbeat.sh build "images ready for IMAGES=${IMAGES:-default}"
