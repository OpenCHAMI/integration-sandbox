#!/usr/bin/env bash
# Poll well-known healthcheck endpoints until they all respond OK or we timeout.
# Each entry is "name|url" — name is for logging, url is what we curl.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

TIMEOUT_S="${WAIT_TIMEOUT_S:-180}"
INTERVAL_S="${WAIT_INTERVAL_S:-3}"

ENDPOINTS=(
  "vault|http://127.0.0.1:8200/v1/sys/health"
  "localstack|http://127.0.0.1:4566/_localstack/health"
  "smd|http://127.0.0.1:27779/hsm/v2/service/ready"
  "tokensmith|http://127.0.0.1:27780/health"
  "boot-service|http://127.0.0.1:27791/health"
  "metadata-service|http://127.0.0.1:27792/health"
  "fru-tracker|http://127.0.0.1:27793/health"
  "power-control|http://127.0.0.1:28007/health"
  "redfish-bmc-0|https://127.0.0.1:5000/redfish/v1"
)

deadline=$(( $(date +%s) + TIMEOUT_S ))
fail=0

for entry in "${ENDPOINTS[@]}"; do
  name="${entry%%|*}"
  url="${entry##*|}"
  printf '%-22s' "[wait] $name"
  ok=0
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
    printf ' TIMEOUT\n'
    fail=1
  fi
done

if (( fail )); then
  bash "$ROOT/scripts/log-bundle.sh" wait-timeout >/dev/null
  exit 1
fi
