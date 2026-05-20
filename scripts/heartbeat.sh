#!/usr/bin/env bash
# Append a one-line milestone to PROGRESS.log and STATUS.
# Usage: scripts/heartbeat.sh <short-status> <free-form message>
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
status="${1:-?}"
shift || true
msg="${*:-}"

ts="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
printf '[%s] %s — %s\n' "$ts" "$status" "$msg" >> "$ROOT/PROGRESS.log"
printf '%s\n' "$status" > "$ROOT/STATUS"
