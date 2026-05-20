#!/usr/bin/env bash
# Source the active image manifest, but never clobber values the caller already set.
# Caller already exported (SBX_*_IMAGE in env) wins; manifest fills in the rest.
#
# Usage (from another bash script):
#   source scripts/load-images.sh           # uses $IMAGES (default: default)
#
# Honors:
#   IMAGES=<name>      -> images/<name>.env
#   IMAGES=/path.env   -> absolute or relative path
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
IMAGES="${IMAGES:-default}"

if [[ "$IMAGES" == */* || "$IMAGES" == *.env ]]; then
  manifest="$IMAGES"
else
  manifest="$ROOT/images/${IMAGES}.env"
fi

if [[ ! -f "$manifest" ]]; then
  printf 'load-images: manifest not found: %s\n' "$manifest" >&2
  return 1 2>/dev/null || exit 1
fi

# Read each VAR=VALUE line; skip if already set in the caller's env.
while IFS= read -r line || [[ -n "$line" ]]; do
  # strip leading whitespace + comments + blank lines
  trimmed="${line#"${line%%[![:space:]]*}"}"
  [[ -z "$trimmed" || "$trimmed" == \#* ]] && continue
  key="${trimmed%%=*}"
  val="${trimmed#*=}"
  if [[ -z "${!key:-}" ]]; then
    export "$key"="$val"
  fi
done < "$manifest"

printf 'load-images: manifest=%s\n' "$manifest" >&2
