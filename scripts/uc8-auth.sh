#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
log_dir="logs/$(date -u +%Y%m%dT%H%M%SZ)/uc8-auth"
mkdir -p "$log_dir"
exec > >(tee "$log_dir/uc8-auth.log") 2>&1
source scripts/load-images.sh
export UC8_AUTH_PROJECT="uc8-auth-$$"
export UC8_AUTH_ENABLED=1 SBX_SKIP_PREFLIGHT=1
export UC8_AUTH_SERVICE="${UC8_AUTH_SERVICE:-boot}"
port=27791
target=boot-service
route=/bootconfigurations
if [[ "$UC8_AUTH_SERVICE" == bss ]]; then
  port=27778; target=bss; route=/boot/v1/bootparameters
elif [[ "$UC8_AUTH_SERVICE" != boot ]]; then
  printf 'Unsupported UC8_AUTH_SERVICE\n' >&2; exit 1
fi
compose=(docker compose -p "$UC8_AUTH_PROJECT" -f compose/uc8-auth.yaml)
# Reuse the documented boot port; refuse to disturb an existing sandbox.
published="$(docker ps -q --filter publish="$port")"
command -v nc >/dev/null
if [[ -n "$published" ]] || nc -z 127.0.0.1 "$port"; then
  printf 'UC8 auth refuses occupied port %s.\n' "$port"
  printf '%s\n' "UC8 auth blocked: port $port occupied; existing stack unchanged." >> PROGRESS.log
  printf 'UC8 auth requires a separate runner or free port %s.\n' "$port" > STATUS
  printf 'UC8 auth blocked: port %s occupied; existing stack preserved.\n' "$port" > BLOCKED.md
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
    printf 'UC8 %s authentication failed. Inspect %s for readiness/contract cause. Boot v0.3.2 lacks inbound enforcement; never classify failed fixture construction as service rejection.\n' "$UC8_AUTH_SERVICE" "$log_dir" > BLOCKED.md
  fi
  printf 'UC8 auth completed with status %s; logs %s\n' "$result" "$log_dir" >> PROGRESS.log
  printf 'UC8 auth status %s\n' "$result" >> STATUS
  exit "$result"
}
trap finish EXIT
# The unique project owns every service created below, including partial up.
existing="$("${compose[@]}" ps -aq)"
if [[ -n "$existing" ]]; then
  printf 'Refusing to adopt existing project %s\n' "$UC8_AUTH_PROJECT"
  exit 1
fi
owned=true
timeout 180 "${compose[@]}" up -d --wait --wait-timeout 90 tokensmith "$target"
ready=false
for _attempt in {1..30}; do
  status="$("${compose[@]}" exec -T tokensmith curl -s --max-time 2 -o /dev/null -w '%{http_code}' "http://$target:$port$route" || true)"
  case "$status" in 200|401|403) ready=true; break ;; esac
  sleep 1
done
if [[ "$ready" != true ]]; then
  printf 'Authentication target %s did not become reachable.\n' "$target"
  exit 1
fi
cd tests
go test -tags integration -count=1 -v -timeout 5m -run '^TestUC8Auth_' ./integration/...
