#!/usr/bin/env bash
# Query GitHub Releases for each OpenCHAMI service repo, pick the latest
# semver release tag, and write images/release.env. The committed file is the
# source of truth at `make ci` time — this script only refreshes it on
# demand (`make refresh-releases`). Network is required only when running
# this script.
#
# Auth: uses $GITHUB_TOKEN if set (raises rate limit from 60/hr to 5000/hr).
# Falls back to anonymous calls otherwise; warns if rate-limited.
#
# Usage:
#   scripts/resolve-latest-releases.sh                # write images/release.env
#   scripts/resolve-latest-releases.sh --print        # print to stdout, don't write
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OUT="$ROOT/images/release.env"
PRINT_ONLY=0
[[ "${1:-}" == "--print" ]] && PRINT_ONLY=1

# (env-var name, ghcr.io image, github repo)
# Note: SBX_SUSHY_IMAGE (the Redfish BMC emulator) is intentionally not
# resolved here — it ships from quay.io/metal3-io, not OpenCHAMI's
# GitHub releases, so it doesn't follow the same release cadence.
# Pinned to `:latest` (or whatever images/<set>.env declares).
declare -a SERVICES=(
  "SBX_SMD_IMAGE|ghcr.io/openchami/smd|OpenCHAMI/smd"
  "SBX_TOKENSMITH_IMAGE|ghcr.io/openchami/tokensmith|OpenCHAMI/tokensmith"
  "SBX_BOOT_IMAGE|ghcr.io/openchami/boot-service|OpenCHAMI/boot-service"
  "SBX_METADATA_IMAGE|ghcr.io/openchami/metadata-service|OpenCHAMI/metadata-service"
  "SBX_FRU_IMAGE|ghcr.io/openchami/fru-tracker|OpenCHAMI/fru-tracker"
  "SBX_POWER_IMAGE|ghcr.io/openchami/power-control|OpenCHAMI/power-control"
  "SBX_MAGELLAN_IMAGE|ghcr.io/openchami/magellan|OpenCHAMI/magellan"
  "SBX_OCHAMI_IMAGE|ghcr.io/openchami/ochami|OpenCHAMI/ochami"
)

# Third-party + sandbox-only images we don't track via GitHub Releases.
# These keep the same pins as default.env.
declare -a STATIC=(
  "SBX_VAULT_IMAGE=hashicorp/vault:1.21"
  "SBX_LOCALSTACK_IMAGE=localstack/localstack:3"
  "SBX_POSTGRES_IMAGE=postgres:16-alpine"
  "SBX_IPMI_SIM_IMAGE=openchami/ipmi-sim:dev"
)

curl_args=(-fsSL -H 'Accept: application/vnd.github+json' -H 'X-GitHub-Api-Version: 2022-11-28')
if [[ -n "${GITHUB_TOKEN:-}" ]]; then
  curl_args+=(-H "Authorization: Bearer $GITHUB_TOKEN")
fi

# Resolve a single repo to its latest release tag. Strips leading 'v' only if
# the corresponding ghcr.io image conventionally drops it (e.g. SMD uses
# `2.17.7`, csm-rie uses `v1.6.7`). We preserve whatever GitHub returns —
# both conventions work as ghcr.io tags as long as the registry is consistent.
fetch_latest() {
  local repo="$1"
  local resp tag
  if ! resp=$(curl "${curl_args[@]}" "https://api.github.com/repos/${repo}/releases/latest" 2>/dev/null); then
    printf 'resolve-latest: %s — no /releases/latest (no published release?), trying /tags\n' "$repo" >&2
    if ! resp=$(curl "${curl_args[@]}" "https://api.github.com/repos/${repo}/tags?per_page=20" 2>/dev/null); then
      printf 'resolve-latest: %s — failed to fetch tags\n' "$repo" >&2
      return 1
    fi
    # First semver-shaped tag from /tags
    tag=$(printf '%s' "$resp" | grep -oE '"name": *"v?[0-9]+\.[0-9]+\.[0-9]+[^"]*"' \
      | head -1 | sed -E 's/.*"name": *"([^"]+)".*/\1/')
  else
    tag=$(printf '%s' "$resp" | grep -oE '"tag_name": *"[^"]+"' | head -1 \
      | sed -E 's/.*"tag_name": *"([^"]+)".*/\1/')
  fi
  if [[ -z "$tag" ]]; then
    printf 'resolve-latest: %s — could not parse tag from response\n' "$repo" >&2
    return 1
  fi
  printf '%s' "$tag"
}

resolved=()
fallback_latest=()
network_failed=()
for entry in "${SERVICES[@]}"; do
  IFS='|' read -r var image repo <<<"$entry"
  printf 'resolve-latest: querying %s ...\n' "$repo" >&2
  if tag=$(fetch_latest "$repo" 2>/tmp/.resolve-err); then
    resolved+=("${var}=${image}:${tag}")
    printf '  -> %s:%s\n' "$image" "$tag" >&2
  else
    err=$(cat /tmp/.resolve-err 2>/dev/null || true)
    # Distinguish "no release published" from "network failure". A successful
    # API call that returns no semver-shaped tag is the former; everything
    # else (curl error, rate limit, 5xx) is the latter and should fail loud.
    if grep -q 'could not parse tag\|no /releases/latest' <<<"$err"; then
      # Inline comments would be parsed into the env value by load-images.sh,
      # so flag the fallback on its own preceding comment line instead.
      resolved+=("# fallback (no GitHub release tagged yet)")
      resolved+=("${var}=${image}:latest")
      fallback_latest+=("$repo")
      printf '  -> %s:latest (fallback — no release tagged)\n' "$image" >&2
    else
      network_failed+=("$var ($repo): $err")
    fi
  fi
done
rm -f /tmp/.resolve-err

if (( ${#network_failed[@]} > 0 )); then
  printf 'resolve-latest: network/API failures:\n' >&2
  printf '  - %s\n' "${network_failed[@]}" >&2
  printf 'resolve-latest: refusing to write a partial manifest. Set GITHUB_TOKEN to raise rate limit.\n' >&2
  exit 1
fi

generated_at=$(date -u +'%Y-%m-%dT%H:%M:%SZ')
{
  cat <<EOF
# integration-sandbox image manifest — release (latest published)
#
# Auto-generated by scripts/resolve-latest-releases.sh on ${generated_at}.
# Each OpenCHAMI service is pinned to the most recent GitHub Release tag.
# Services without a published release fall back to :latest (and are flagged).
# Override individual services as needed (SBX_<NAME>_IMAGE=...).
#
# Refresh with:  make refresh-releases

EOF
  if (( ${#fallback_latest[@]} > 0 )); then
    printf '# WARN: %d service(s) have no GitHub release yet and use :latest:\n' "${#fallback_latest[@]}"
    for r in "${fallback_latest[@]}"; do printf '#   - %s\n' "$r"; done
    printf '\n'
  fi
  printf '# Infra (third-party, version-pinned)\n'
  for s in "${STATIC[@]:0:3}"; do printf '%s\n' "$s"; done
  printf '\n# BMC sims\n'
  printf '%s\n' "${resolved[0]}"
  for s in "${STATIC[@]:3}"; do printf '%s\n' "$s"; done
  printf '\n# OpenCHAMI services (pinned to latest GitHub Release)\n'
  for r in "${resolved[@]:1}"; do printf '%s\n' "$r"; done
} > "$OUT.tmp"

if (( PRINT_ONLY == 1 )); then
  cat "$OUT.tmp"
  rm -f "$OUT.tmp"
else
  mv "$OUT.tmp" "$OUT"
  printf 'resolve-latest: wrote %s\n' "$OUT" >&2
fi
