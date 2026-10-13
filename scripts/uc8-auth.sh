#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
log_dir="logs/$(date -u +%Y%m%dT%H%M%SZ)/uc8-auth"
mkdir -p "$log_dir"
exec > >(tee "$log_dir/uc8-auth.log") 2>&1
source scripts/load-images.sh
export UC8_AUTH_PROJECT="uc8-auth-$$"
export UC8_AUTH_ENABLED=1 SBX_WAIT_TIMEOUT=1ns
compose=(docker compose -p "$UC8_AUTH_PROJECT" -f compose/uc8-auth.yaml)
# Reuse the documented boot port; refuse to disturb an existing sandbox.
published="$(docker ps -q --filter publish=27791)"
command -v nc >/dev/null
if [[ -n "$published" ]] || nc -z 127.0.0.1 27791; then
  printf '%s\n' 'UC8 auth refuses occupied boot port 27791.'
  printf '%s\n' 'UC8 auth blocked: boot port 27791 is occupied; existing stack unchanged.' >> PROGRESS.log
  printf '%s\n' 'UC8 auth requires a separate runner or free boot port.' >> STATUS
  exit 1
fi
owned=false
finish() {
  result=$?
  if [[ "$owned" == true ]]; then
    if ! "${compose[@]}" ps > "$log_dir/compose-ps.log" 2>&1; then result=1; fi
    if ! docker ps -a > "$log_dir/docker-ps.log" 2>&1; then result=1; fi
    if ! "${compose[@]}" logs --tail=200 > "$log_dir/services.log" 2>&1; then result=1; fi
    if ! "${compose[@]}" down --remove-orphans; then result=1; fi
  fi
  if [[ "$result" != 0 ]]; then
    printf 'UC8 auth failed. Inspect %s. Boot v0.3.2 configures JWKS but does not wire inbound authentication middleware; readiness failures may also prevent execution.\n' "$log_dir" > BLOCKED.md
  fi
  printf 'UC8 auth completed with status %s; logs %s\n' "$result" "$log_dir" >> PROGRESS.log
  printf 'UC8 auth status %s\n' "$result" >> STATUS
  exit "$result"
}
trap finish EXIT
# The unique project owns every service created below, including partial up.
if [[ -n "$("${compose[@]}" ps -aq)" ]]; then
  printf 'Refusing to adopt existing project %s\n' "$UC8_AUTH_PROJECT"
  exit 1
fi
owned=true
"${compose[@]}" up -d --wait --wait-timeout 90
ready=false
for attempt in {1..30}; do
  status="$("${compose[@]}" exec -T tokensmith curl -s --max-time 2 -o /dev/null -w '%{http_code}' http://boot-service:27791/bootconfigurations || true)"
  case "$status" in 200|401|403) ready=true; break ;; esac
  sleep 1
done
if [[ "$ready" != true ]]; then
  printf '%s\n' 'Boot authentication stack did not become reachable.'
  exit 1
fi
cd tests
go test -tags integration -count=1 -v -timeout 5m -run '^TestUC8Auth_' ./integration/...
