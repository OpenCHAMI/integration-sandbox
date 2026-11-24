#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
umask 077
export GOTOOLCHAIN=go1.26.10
run_id="$(date -u +%Y%m%d%H%M%S)-$(od -An -N6 -tx1 /dev/urandom | tr -d ' \n')"
export SBX_DECLARATIVE_COMPOSE_PROJECT="declarative-${run_id}"
export SBX_DECLARATIVE_RUN_DIR="$root/logs/issue50/run-$run_id"
export SBX_DECLARATIVE_COMPOSE_FILE="$root/compose/declarative-boot.yaml"
export SBX_DECLARATIVE_COMPOSE_PATH="$SBX_DECLARATIVE_COMPOSE_FILE"
export SBX_DECLARATIVE_FIXTURE_IMAGE="openchami-declarative-fixture:$run_id"
export SBX_DECLARATIVE_CONTROL_RECEIPTS="$SBX_DECLARATIVE_RUN_DIR/receipts.jsonl"
export SBX_DECLARATIVE_BOOT_CONFIG_DIR="$SBX_DECLARATIVE_RUN_DIR/config/boot"
export SBX_DECLARATIVE_HSM_BOOTSTRAP_REDACTIONS="$SBX_DECLARATIVE_RUN_DIR/credentials.redactions"
export SBX_DECLARATIVE_QUIESCENCE_LOCK="$SBX_DECLARATIVE_RUN_DIR/quiescence.lock"
unset SBX_DECLARATIVE_QUIESCENCE_INODE
export SBX_DECLARATIVE_ASSET_MANIFEST="$SBX_DECLARATIVE_RUN_DIR/assets.jsonl"
export SBX_DECLARATIVE_ISSUER="http://tokensmith:27780"
export SBX_DECLARATIVE_AUTH_OFF_SERVICE="boot-auth-off"
unset SBX_DECLARATIVE_BOOT_HOST_PORT SBX_DECLARATIVE_AUTH_OFF_HOST_PORT SBX_DECLARATIVE_SMD_HOST_PORT SBX_DECLARATIVE_TOKENSMITH_HOST_PORT
unset SBX_DECLARATIVE_BOOT_URL SBX_DECLARATIVE_AUTH_OFF_URL BOOT_URL SBX_DECLARATIVE_HSM_BOOTSTRAP TOKENSMITH_BOOTSTRAP_TOKEN BOOT_SERVICE_TOKENSMITH_BOOTSTRAP_TOKEN
export RUN_DIR="$SBX_DECLARATIVE_RUN_DIR" COMPOSE_FILE="$SBX_DECLARATIVE_COMPOSE_FILE" COMPOSE_PROJECT="$SBX_DECLARATIVE_COMPOSE_PROJECT" PROJECT="$SBX_DECLARATIVE_COMPOSE_PROJECT"
export CONTROL_RECEIPTS="$SBX_DECLARATIVE_CONTROL_RECEIPTS" FIXTURE_IMAGE="$SBX_DECLARATIVE_FIXTURE_IMAGE"
mkdir -p "$RUN_DIR/data/boot" "$RUN_DIR/data/auth-off" "$RUN_DIR/build" "$SBX_DECLARATIVE_BOOT_CONFIG_DIR"
chmod 700 "$RUN_DIR"
chmod 755 "$SBX_DECLARATIVE_BOOT_CONFIG_DIR"
chmod 777 "$RUN_DIR/data/boot" "$RUN_DIR/data/auth-off"
touch "$SBX_DECLARATIVE_CONTROL_RECEIPTS" "$SBX_DECLARATIVE_ASSET_MANIFEST"
exec > >(tee -a "$RUN_DIR/test-declarative-boot.log") 2>&1
compose=(docker compose -p "$COMPOSE_PROJECT" -f "$COMPOSE_FILE")
owned_up=0
phase=setup
# Called by the owning EXIT trap.
# shellcheck disable=SC2329
cleanup() {
  local rc=$? id label project kind name path pid resolved remaining attempt capture_safe=1 safe=1
  trap - EXIT
  set +e
  if [[ $owned_up == 1 ]]; then
    jq -e -s --arg project "$COMPOSE_PROJECT" 'all(.[]; .project == $project and (.kind == "container" or .kind == "process" or .kind == "bind-directory"))' "$SBX_DECLARATIVE_ASSET_MANIFEST" > /dev/null || { capture_safe=0; rc=1; }
    "${compose[@]}" ps -a > "$RUN_DIR/compose-ps.txt" 2>&1
    docker ps -a > "$RUN_DIR/docker-ps.txt" 2>&1
    while IFS= read -r pid; do
      [[ "$pid" =~ ^[0-9]+$ ]] || { capture_safe=0; rc=1; continue; }
      if resolved="$(ps -p "$pid" -o command= 2>&1)"; then
        if [[ "$resolved" == *"$RUN_DIR"* ]]; then
          kill "$pid" || { capture_safe=0; rc=1; }
          for ((attempt=0; attempt<20; attempt++)); do
            if resolved="$(ps -p "$pid" -o command= 2>&1)"; then :
            elif [[ -z "$resolved" ]]; then break
            else capture_safe=0; rc=1; break
            fi
            sleep 0.1
          done
          if ps -p "$pid" -o command= > /dev/null 2>&1; then capture_safe=0; fi
        else
          capture_safe=0
          rc=1
        fi
      elif [[ -n "$resolved" ]]; then
        capture_safe=0
        rc=1
      fi
    done < <(jq -r --arg project "$COMPOSE_PROJECT" 'select(.project == $project and .kind == "process") | .pid' "$SBX_DECLARATIVE_ASSET_MANIFEST")
    if [[ $rc != 0 ]]; then
      if [[ "$capture_safe" != 1 || -e "$RUN_DIR/identity-redaction-incomplete" || -L "$RUN_DIR/identity-redaction-incomplete" ]]; then
        printf 'credential-safe log capture unavailable\n' > "$RUN_DIR/compose-failure.log"
      elif ! "${compose[@]}" logs --tail=200 2>&1 | bash "$root/scripts/test-declarative-boot-redact.sh" > "$RUN_DIR/compose-failure.log"; then
        printf 'credential-safe log capture unavailable\n' > "$RUN_DIR/compose-failure.log"
        rc=1
      fi
      if [[ "$phase" == setup ]]; then
        printf 'BLOCKED: setup failed; inspect owned service diagnostics before retrying\n' > "$RUN_DIR/BLOCKED.md"
      fi
    fi
    while IFS=$'\t' read -r kind name path pid; do
      case "$kind" in
        container)
          id="$(docker inspect -f '{{.Id}}' "$name" 2>/dev/null)" || continue
          label="$(docker inspect -f '{{index .Config.Labels "openchami.acceptance.run"}}' "$id" 2>/dev/null)"
          project="$(docker inspect -f '{{index .Config.Labels "com.docker.compose.project"}}' "$id" 2>/dev/null)"
          [[ "$label" == "$COMPOSE_PROJECT" && "$project" == "$COMPOSE_PROJECT" ]] && docker rm -f "$id" >/dev/null || rc=1
          ;;
        process) : ;;
        bind-directory) : ;;
        *) rc=1 ;;
      esac
    done < <(jq -r --arg project "$COMPOSE_PROJECT" 'select(.project == $project) | [.kind, .name // "", .path // "", (.pid // "" | tostring)] | @tsv' "$SBX_DECLARATIVE_ASSET_MANIFEST")
    while IFS= read -r id; do
      [[ -z "$id" ]] || docker rm -f "$id" >/dev/null || rc=1
    done < <(docker ps -aq --filter "label=com.docker.compose.project=$COMPOSE_PROJECT" --filter "label=openchami.acceptance.run=$COMPOSE_PROJECT")
    "${compose[@]}" down --remove-orphans -v >> "$RUN_DIR/cleanup.txt" 2>&1 || rc=1
    while IFS= read -r path; do
      [[ -d "$path" && ! -L "$path" ]] || continue
      resolved="$(cd "$path" && pwd -P)" || { rc=1; continue; }
      case "$resolved" in "$RUN_DIR"/*) rm -rf -- "$resolved" || rc=1 ;; *) rc=1 ;; esac
    done < <(jq -r --arg project "$COMPOSE_PROJECT" 'select(.project == $project and .kind == "bind-directory") | .path' "$SBX_DECLARATIVE_ASSET_MANIFEST")
    remaining="$(docker ps -aq --filter "label=com.docker.compose.project=$COMPOSE_PROJECT" --filter "label=openchami.acceptance.run=$COMPOSE_PROJECT")" || safe=0
    [[ -z "$remaining" ]] || safe=0
    while IFS= read -r pid; do
      [[ "$pid" =~ ^[0-9]+$ ]] || { safe=0; continue; }
      if resolved="$(ps -p "$pid" -o command= 2>&1)"; then
        case "$resolved" in *"$RUN_DIR"*) safe=0 ;; esac
      elif [[ -n "$resolved" ]]; then
        printf 'BLOCKED: cannot inspect owned watcher %s\n' "$pid" >> "$RUN_DIR/cleanup.txt"
        safe=0
      fi
    done < <(jq -r --arg project "$COMPOSE_PROJECT" 'select(.project == $project and .kind == "process") | .pid' "$SBX_DECLARATIVE_ASSET_MANIFEST")
  fi
  rm -rf -- "$RUN_DIR/data" "$RUN_DIR/build"
  if [[ "$safe" == 1 ]]; then
    rm -rf -- "$RUN_DIR/config" || rc=1
    rm -f -- "$SBX_DECLARATIVE_HSM_BOOTSTRAP_REDACTIONS" || rc=1
  else
    printf 'BLOCKED: owned processes or containers remain; private credentials retained locally\n' >> "$RUN_DIR/cleanup.txt"
    rc=1
  fi
  docker image rm "$FIXTURE_IMAGE" >> "$RUN_DIR/cleanup.txt" 2>&1 || true
  printf 'project=%s exit=%s\n' "$COMPOSE_PROJECT" "$rc" >> "$RUN_DIR/cleanup.txt"
  exit "$rc"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

bash "$root/scripts/test-declarative-boot-quiescence.sh" init
SBX_DECLARATIVE_QUIESCENCE_INODE="$(ls -di "$SBX_DECLARATIVE_QUIESCENCE_LOCK")"
export SBX_DECLARATIVE_QUIESCENCE_INODE
: "${SBX_DECLARATIVE_BOOT_IMAGE:?set an explicit candidate or baseline image}"
: "${SBX_DECLARATIVE_BOOT_CLIENT_BIN:?set the absolute candidate host client}"
[[ "$SBX_DECLARATIVE_BOOT_CLIENT_BIN" = /* && -x "$SBX_DECLARATIVE_BOOT_CLIENT_BIN" ]] || { printf 'invalid host client\n' >&2; exit 1; }
[[ -d "$root/tests/cmd/declarative-control" ]] || { printf 'BLOCKED: control helper not ready\n' >&2; exit 1; }
export SBX_DECLARATIVE_BOOT_IMAGE_ID
SBX_DECLARATIVE_BOOT_IMAGE_ID="$(docker image inspect "$SBX_DECLARATIVE_BOOT_IMAGE" --format '{{.Id}}')"
image_revision="$(docker image inspect "$SBX_DECLARATIVE_BOOT_IMAGE" --format '{{index .Config.Labels "org.opencontainers.image.revision"}}')"
export BOOT_IMAGE_ID="$SBX_DECLARATIVE_BOOT_IMAGE_ID"
printf 'boot image ID: %s\n' "$BOOT_IMAGE_ID"
if [[ "$SBX_DECLARATIVE_BOOT_IMAGE" == openchami-sandbox/boot-service:pr75-fbe7a63 ]]; then
  [[ "$BOOT_IMAGE_ID" == sha256:df546b2e5a88805b86c3a0bb6cd9823da9c55a96714b35fb37e48c0103bb269b ]]
  [[ "$image_revision" == fbe7a63b7e1a23433a296172f65bb692b8636b0e ]]
fi
go -C "$root/tests" build -o "$RUN_DIR/declarative-control" ./cmd/declarative-control
go -C "$root/tests" build -o "$RUN_DIR/declarative-fixture-host" ./cmd/declarative-fixture
go -C "$root/tests" build -o "$RUN_DIR/declarative-ports" ./cmd/declarative-ports
go -C "$root/tests" build -tags 'integration declarative_boot' -o "$RUN_DIR/quiescence-http" ./internal/quiescence/cmd/boot-http
selected_ports="$("$RUN_DIR/declarative-ports")"
printf '%s' "$selected_ports" | jq -e '[.boot,.authOff,.smd,.tokensmith] | length == 4 and all(.[]; type == "number" and floor == . and . > 0 and . <= 65535) and (unique | length == 4)' > /dev/null
SBX_DECLARATIVE_BOOT_HOST_PORT="$(jq -er '.boot' <<< "$selected_ports")"
SBX_DECLARATIVE_AUTH_OFF_HOST_PORT="$(jq -er '.authOff' <<< "$selected_ports")"
SBX_DECLARATIVE_SMD_HOST_PORT="$(jq -er '.smd' <<< "$selected_ports")"
SBX_DECLARATIVE_TOKENSMITH_HOST_PORT="$(jq -er '.tokensmith' <<< "$selected_ports")"
export SBX_DECLARATIVE_BOOT_HOST_PORT SBX_DECLARATIVE_AUTH_OFF_HOST_PORT SBX_DECLARATIVE_SMD_HOST_PORT SBX_DECLARATIVE_TOKENSMITH_HOST_PORT
image_arch="$(docker image inspect "$SBX_DECLARATIVE_BOOT_IMAGE" --format '{{.Architecture}}')"
GOOS=linux GOARCH="$image_arch" CGO_ENABLED=0 go -C "$root/tests" build -o "$RUN_DIR/build/declarative-fixture" ./cmd/declarative-fixture
docker build --platform "linux/$image_arch" -f "$root/compose/declarative-fixture.Dockerfile" -t "$FIXTURE_IMAGE" "$RUN_DIR/build"
export SBX_DECLARATIVE_CONTROL_BIN="$RUN_DIR/declarative-control"
owned_up=1
"${compose[@]}" up -d --wait postgres smd tokensmith-backend tokensmith
export SBX_DECLARATIVE_TOKENSMITH_CONTAINER
SBX_DECLARATIVE_TOKENSMITH_CONTAINER="$("${compose[@]}" ps -q tokensmith-backend)"
export SBX_DECLARATIVE_TOKENSMITH_STORE="/tokensmith/data/bootstrap-tokens"
check_port() {
  local service="$1" target="$2" requested="$3" id configured mapped
  id="$("${compose[@]}" ps -q "$service")"
  configured="$(docker inspect "$id" | jq -er --arg key "$target/tcp" '.[0].HostConfig.PortBindings[$key] | select(length == 1 and .[0].HostIp == "127.0.0.1") | .[0].HostPort')"
  mapped="$("${compose[@]}" port "$service" "$target")"
  [[ "$configured" == "$requested" && "$mapped" == "127.0.0.1:$requested" ]] || { printf 'BLOCKED: %s host binding mismatch\n' "$service" >&2; return 1; }
}
check_port smd 27779 "$SBX_DECLARATIVE_SMD_HOST_PORT"
check_port tokensmith 27780 "$SBX_DECLARATIVE_TOKENSMITH_HOST_PORT"
SBX_DECLARATIVE_SMD_URL="http://127.0.0.1:$SBX_DECLARATIVE_SMD_HOST_PORT"
SBX_DECLARATIVE_TOKENSMITH_URL="http://127.0.0.1:$SBX_DECLARATIVE_TOKENSMITH_HOST_PORT"
export SBX_DECLARATIVE_SMD_URL SBX_DECLARATIVE_TOKENSMITH_URL
export SBX_DECLARATIVE_FIXTURE_URL="$SBX_DECLARATIVE_TOKENSMITH_URL" FIXTURE_URL="$SBX_DECLARATIVE_TOKENSMITH_URL"
curl -fsS --retry 30 --retry-connrefused --retry-delay 1 "$SBX_DECLARATIVE_TOKENSMITH_URL/.well-known/jwks.json" > /dev/null
bash "$root/scripts/test-declarative-boot-verify.sh"
"$SBX_DECLARATIVE_CONTROL_BIN" prepare-boot-identity '{}' | jq -se 'length == 1 and .[0] == {}' > /dev/null
jq -c '{Components:[.nodes[] | {ID:.xname,Type:"Node",State:"On",Flag:"OK",Enabled:true,Role:"Compute",NID:.nid}]}' "$root/fixtures/declarative-boot/nodes.json" | curl -fsS -o /dev/null -H 'Content-Type: application/json' --data-binary @- "$SBX_DECLARATIVE_SMD_URL/hsm/v2/State/Components"
while IFS= read -r interface; do
  printf '%s' "$interface" | curl -fsS -o /dev/null -H 'Content-Type: application/json' --data-binary @- "$SBX_DECLARATIVE_SMD_URL/hsm/v2/Inventory/EthernetInterfaces"
done < <(jq -c '.nodes[] | {ID:(.mac | gsub(":";"")),MACAddress:.mac,ComponentID:.xname,Type:"Node"}' "$root/fixtures/declarative-boot/nodes.json")
for witness in x0c0s2b0n0 x0c0s3b0n0; do
  curl -fsS "$SBX_DECLARATIVE_SMD_URL/hsm/v2/State/Components/$witness" | jq -e --arg id "$witness" '.ID == $id or any(.Components[]?; .ID == $id)' > /dev/null
done
"${compose[@]}" up -d boot boot-auth-off
check_port boot 27790 "$SBX_DECLARATIVE_BOOT_HOST_PORT"
check_port boot-auth-off 27790 "$SBX_DECLARATIVE_AUTH_OFF_HOST_PORT"
SBX_DECLARATIVE_BOOT_URL="http://127.0.0.1:$SBX_DECLARATIVE_BOOT_HOST_PORT"
SBX_DECLARATIVE_AUTH_OFF_URL="http://127.0.0.1:$SBX_DECLARATIVE_AUTH_OFF_HOST_PORT"
export SBX_DECLARATIVE_BOOT_URL SBX_DECLARATIVE_AUTH_OFF_URL
export BOOT_URL="$SBX_DECLARATIVE_BOOT_URL"
for endpoint in "$SBX_DECLARATIVE_BOOT_URL" "$SBX_DECLARATIVE_AUTH_OFF_URL"; do
  ready=0
  for ((attempt=0; attempt<60; attempt++)); do
    if "$RUN_DIR/quiescence-http" "$endpoint/health" > /dev/null; then ready=1; break; fi
    bash "$root/scripts/test-declarative-boot-quiescence.sh" check
    sleep 1
  done
  [[ "$ready" == 1 ]] || { printf 'boot health check failed\n' >&2; exit 1; }
done
baseline_source="${SBX_DECLARATIVE_BASELINE_SOURCE:-$(dirname "$root")/boot-service-pr75-review}"
source_head=''
source_clean=false
if [[ -e "$baseline_source/.git" ]]; then
  if source_head="$(git -C "$baseline_source" rev-parse HEAD 2>/dev/null)" && source_status="$(git -C "$baseline_source" status --porcelain)" && [[ -z "$source_status" ]]; then
    source_clean=true
  fi
fi
openapi_status=0
if status="$(curl -sS --max-time 10 --max-filesize 4194304 -o "$RUN_DIR/openapi.json" -w '%{http_code}' "$SBX_DECLARATIVE_BOOT_URL/openapi.json")" && [[ "$status" =~ ^[0-9]{3}$ ]]; then
  openapi_status="$((10#$status))"
fi
if [[ "$openapi_status" == 200 ]] && jq -e '(.openapi | type) == "string" and (.paths | type) == "object"' "$RUN_DIR/openapi.json" >/dev/null; then
  jq . "$RUN_DIR/openapi.json" > "$RUN_DIR/openapi-classifier.json"
else
  printf '{}\n' > "$RUN_DIR/openapi-classifier.json"
  printf 'OpenAPI evidence unavailable; feature classifications will be unclassified\n' >&2
fi
clock_start="$(perl -MTime::HiRes=gettimeofday -MPOSIX=strftime -e 'my ($s,$us)=gettimeofday; print strftime("%Y-%m-%dT%H:%M:%S", gmtime($s)), sprintf(".%06dZ",$us)')"
clock_status=0
if status="$(curl -sS --max-time 5 -o /dev/null -w '%{http_code}' "$SBX_DECLARATIVE_BOOT_URL/health?classification_probe=$run_id")" && [[ "$status" =~ ^[0-9]{3}$ ]]; then
  clock_status="$((10#$status))"
fi
clock_end="$(perl -MTime::HiRes=gettimeofday -MPOSIX=strftime -e 'my ($s,$us)=gettimeofday; print strftime("%Y-%m-%dT%H:%M:%S", gmtime($s)), sprintf(".%06dZ",$us)')"
jq -nc --arg image "$SBX_DECLARATIVE_BOOT_IMAGE_ID" --arg revision "$image_revision" --arg head "$source_head" --argjson clean "$source_clean" --argjson status "$openapi_status" --arg origin "127.0.0.1:$SBX_DECLARATIVE_BOOT_HOST_PORT" --arg probe "$run_id" --arg start "$clock_start" --arg end "$clock_end" --argjson clock_status "$clock_status" '{imageID:$image,imageRevision:$revision,sourceHead:$head,sourceClean:$clean,openapiStatus:$status,bootOrigin:$origin,clockProbe:$probe,clockStart:$start,clockEnd:$end,clockStatus:$clock_status}' > "$RUN_DIR/classification-provenance.json"
seed_rc=0
jq -c '{metadata:{name:"acceptance-legacy"},spec:{hosts:[.node],kernel:.default.Kernel,initrd:.default.Initrd,params:.default.Params}}' "$root/fixtures/declarative-boot/legacy.json" | "$RUN_DIR/quiescence-http" -method=POST -status "$SBX_DECLARATIVE_AUTH_OFF_URL/bootconfigurations" | bash "$root/scripts/test-declarative-boot-redact.sh" > "$RUN_DIR/legacy-seed-response.json" || seed_rc=$?
seed_status="$(perl -ne '$status = $1 if /^__SBX_HTTP_STATUS__(\d{3})$/; END { print $status // "" }' "$RUN_DIR/legacy-seed-response.json")"
perl -0777 -i -pe 's/\n__SBX_HTTP_STATUS__[0-9]{3}\n\z//' "$RUN_DIR/legacy-seed-response.json"
[[ "$seed_status" =~ ^2[0-9][0-9]$ ]] || { [[ "$seed_rc" != 0 ]] || seed_rc=22; }
if [[ "$seed_rc" != 0 ]]; then
  printf 'legacy seed failed (HTTP %s, exit %s); redacted response: %s\n' "${seed_status:-unknown}" "$seed_rc" "$RUN_DIR/legacy-seed-response.json" >&2
  exit "$seed_rc"
fi
rm -f "$RUN_DIR/legacy-seed-response.json"
"$RUN_DIR/quiescence-http" "$SBX_DECLARATIVE_AUTH_OFF_URL/bootconfigurations" > /dev/null
"$RUN_DIR/quiescence-http" "$SBX_DECLARATIVE_AUTH_OFF_URL/bootscript?host=x0c0s3b0n0" > "$RUN_DIR/legacy-boot.txt"
jq -r '.default.Kernel, .default.Initrd, (.default.Params | split(" ")[])' "$root/fixtures/declarative-boot/legacy.json" | while IFS= read -r value; do grep -Fq -- "$value" "$RUN_DIR/legacy-boot.txt"; done
bash "$root/scripts/test-declarative-boot-quiescence.sh" check
bash "$root/scripts/test-declarative-boot-control-check.sh"
bash "$root/scripts/test-declarative-boot-quiescence.sh" check
check_port boot 27790 "$SBX_DECLARATIVE_BOOT_HOST_PORT"
check_port boot-auth-off 27790 "$SBX_DECLARATIVE_AUTH_OFF_HOST_PORT"
check_port smd 27779 "$SBX_DECLARATIVE_SMD_HOST_PORT"
check_port tokensmith 27780 "$SBX_DECLARATIVE_TOKENSMITH_HOST_PORT"
printf 'Running full tagged acceptance suite; unsupported features are failures.\n'
phase=suite
set +e
go -C "$root/tests" test -tags 'integration declarative_boot' -race -count=1 -json -timeout 15m ./declarativeboot/... 2>&1 | bash "$root/scripts/test-declarative-boot-redact.sh" | tee "$RUN_DIR/go.jsonl"
pipeline_status=("${PIPESTATUS[@]}")
go_rc=${pipeline_status[0]}
if [[ ${pipeline_status[1]} != 0 || ${pipeline_status[2]} != 0 ]]; then go_rc=1; fi
set -e
if ! bash "$root/scripts/test-declarative-boot-quiescence.sh" check; then
  printf 'BLOCKED: quiescence lost; discard the owned run\n' > "$RUN_DIR/BLOCKED.md"
  [[ "$go_rc" != 0 ]] || go_rc=1
fi
request_capture_safe=1
jq -e -s --arg project "$COMPOSE_PROJECT" 'all(.[]; .project == $project and (.kind == "container" or .kind == "process" or .kind == "bind-directory"))' "$SBX_DECLARATIVE_ASSET_MANIFEST" > /dev/null || request_capture_safe=0
while IFS= read -r pid; do
  [[ "$pid" =~ ^[0-9]+$ ]] || { request_capture_safe=0; continue; }
  if process_command="$(ps -p "$pid" -o command= 2>&1)"; then
    [[ "$process_command" != *"$RUN_DIR"* ]] || request_capture_safe=0
  elif [[ -n "$process_command" ]]; then
    request_capture_safe=0
  fi
done < <(jq -r --arg project "$COMPOSE_PROJECT" 'select(.project == $project and .kind == "process") | .pid' "$SBX_DECLARATIVE_ASSET_MANIFEST")
if [[ "$request_capture_safe" != 1 || -e "$RUN_DIR/identity-redaction-incomplete" || -L "$RUN_DIR/identity-redaction-incomplete" ]] || ! "${compose[@]}" logs --timestamps --no-color boot smd tokensmith tokensmith-backend 2>&1 | bash "$root/scripts/test-declarative-boot-redact.sh" > "$RUN_DIR/boot-access.log"; then
  : > "$RUN_DIR/boot-access.log"
  printf 'boot request evidence unavailable; feature classifications will be unclassified\n' >&2
fi
if ! bash "$root/scripts/test-declarative-boot-classify.sh" "$RUN_DIR/go.jsonl" "$RUN_DIR/classification-provenance.json" "$RUN_DIR/openapi-classifier.json" "$RUN_DIR/boot-access.log" > "$RUN_DIR/failure-classification.json"; then
  [[ "$go_rc" != 0 ]] || go_rc=1
fi
manifest_rc=0
bash "$root/scripts/test-declarative-boot-manifest.sh" "$RUN_DIR/go.jsonl" || manifest_rc=$?
if [[ "$go_rc" != 0 ]]; then exit "$go_rc"; fi
exit "$manifest_rc"
