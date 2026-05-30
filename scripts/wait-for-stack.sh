#!/usr/bin/env bash
# Poll well-known healthcheck endpoints until they all respond OK or we
# time out. Each entry is "name|url" — name is for logging, url is what
# we curl.
#
# Each endpoint gets its OWN deadline. The previous global-deadline
# design (pre-2026-05-30) made the last endpoints in the list look like
# they timed out whenever an earlier endpoint was slow — even if the
# late endpoints were already up. Per-endpoint deadlines mean slow
# services no longer starve later polls of their budget.
#
# Usage:
#   wait-for-stack.sh [infra|core|bmc|all]
#
# - infra: vault + localstack (postgres has no HTTP /health)
# - core:  smd, tokensmith, boot-service, metadata-service, fru-tracker,
#          power-control
# - bmc:   redfish-bmc-0 (the only port-mapped BMC sim)
# - all:   everything (default)
#
# Env:
#   WAIT_PER_TIMEOUT_S — seconds per endpoint (default 120)
#   WAIT_TIMEOUT_S     — legacy name; kept as a fallback for callers
#                        that already set it
#   WAIT_INTERVAL_S    — seconds between polls (default 3)
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

PER_TIMEOUT_S="${WAIT_PER_TIMEOUT_S:-${WAIT_TIMEOUT_S:-120}}"
INTERVAL_S="${WAIT_INTERVAL_S:-3}"
SUBSET="${1:-all}"

INFRA_ENDPOINTS=(
  "vault|http://127.0.0.1:8200/v1/sys/health"
  "localstack|http://127.0.0.1:4566/_localstack/health"
)

CORE_ENDPOINTS=(
  "smd|http://127.0.0.1:27779/hsm/v2/service/ready"
  "tokensmith|http://127.0.0.1:27780/health"
  "boot-service|http://127.0.0.1:27791/health"
  "metadata-service|http://127.0.0.1:27792/health"
  "fru-tracker|http://127.0.0.1:27793/health"
  "power-control|http://127.0.0.1:28007/health"
)

BMC_ENDPOINTS=(
  "redfish-bmc-0|https://127.0.0.1:5000/redfish/v1"
)

case "$SUBSET" in
  infra) ENDPOINTS=("${INFRA_ENDPOINTS[@]}") ;;
  core)  ENDPOINTS=("${CORE_ENDPOINTS[@]}") ;;
  bmc)   ENDPOINTS=("${BMC_ENDPOINTS[@]}") ;;
  all)   ENDPOINTS=("${INFRA_ENDPOINTS[@]}" "${CORE_ENDPOINTS[@]}" "${BMC_ENDPOINTS[@]}") ;;
  *)
    echo "wait-for-stack: unknown subset '$SUBSET' (expected: infra|core|bmc|all)" >&2
    exit 2
    ;;
esac

printf '[wait] subset=%s per-endpoint-timeout=%ds interval=%ds\n' \
  "$SUBSET" "$PER_TIMEOUT_S" "$INTERVAL_S"

fail=0
for entry in "${ENDPOINTS[@]}"; do
  name="${entry%%|*}"
  url="${entry##*|}"
  printf '%-22s' "[wait] $name"
  ok=0
  deadline=$(( $(date +%s) + PER_TIMEOUT_S ))
  while (( $(date +%s) < deadline )); do
    code=$(curl -ks -o /dev/null -w '%{http_code}' --max-time 2 "$url" || echo 000)
    if [[ "$code" =~ ^(200|204|301|302|307|308|401)$ ]]; then
      printf ' OK  (%s)\n' "$code"
      ok=1
      break
    fi
    sleep "$INTERVAL_S"
  done
  if (( ok == 0 )); then
    printf ' TIMEOUT (after %ds)\n' "$PER_TIMEOUT_S"
    fail=1
  fi
done

if (( fail )); then
  bash "$ROOT/scripts/log-bundle.sh" wait-timeout >/dev/null
  exit 1
fi
