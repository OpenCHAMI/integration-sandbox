#!/usr/bin/env bash
set -euo pipefail

: "${SBX_DECLARATIVE_RUN_DIR:?required}"
: "${SBX_DECLARATIVE_CONTROL_BIN:?required}"
: "${SBX_DECLARATIVE_CONTROL_RECEIPTS:?required}"
: "${SBX_DECLARATIVE_COMPOSE_PROJECT:?required}"
: "${SBX_DECLARATIVE_COMPOSE_FILE:?required}"
: "${SBX_DECLARATIVE_BOOT_IMAGE_ID:?required}"
exec > >(tee -a "$SBX_DECLARATIVE_RUN_DIR/control-check.log") 2>&1
compose=(docker compose -p "$SBX_DECLARATIVE_COMPOSE_PROJECT" -f "$SBX_DECLARATIVE_COMPOSE_FILE")
empty_digest="$(printf '' | shasum -a 256 | cut -d ' ' -f 1)"
for instance in main auth-off; do
  if [[ "$instance" == main ]]; then service=boot; data=boot; else service=boot-auth-off; data=auth-off; fi
  for dir in "$SBX_DECLARATIVE_RUN_DIR/data/$data/bootassignments" "$SBX_DECLARATIVE_RUN_DIR/data/$data/revisions/bootconfig"; do
    if [[ -d "$dir" && -n "$(find "$dir" -type f -print -quit)" ]]; then
      printf 'unexpected pre-existing declarative store in %s\n' "$instance" >&2
      exit 1
    fi
  done
  before_container="$("${compose[@]}" ps -q "$service")"
  before_started="$(docker inspect --format '{{.State.StartedAt}}' "$before_container")"
  before_mount="$(docker inspect --format '{{range .Mounts}}{{if eq .Destination "/data"}}{{.Source}}{{end}}{{end}}' "$before_container")"
  before_image="$(docker inspect --format '{{.Image}}' "$before_container")"
  [[ "$before_mount" == "$SBX_DECLARATIVE_RUN_DIR/data/$data" && "$before_image" == "$SBX_DECLARATIVE_BOOT_IMAGE_ID" ]] || { printf 'unexpected %s store mount or image\n' "$instance" >&2; exit 1; }
  before="$(wc -l < "$SBX_DECLARATIVE_CONTROL_RECEIPTS" | tr -d ' ')"
  response="$("$SBX_DECLARATIVE_CONTROL_BIN" snapshot-store "{\"instance\":\"$instance\"}")"
  digest="$(printf '%s' "$response" | jq -er '.configurationDigest | sub("^sha256:"; "")')"
  [[ "$digest" == "$empty_digest" ]] || { printf 'snapshot digest does not match independently observed empty %s store\n' "$instance" >&2; exit 1; }
  after="$(wc -l < "$SBX_DECLARATIVE_CONTROL_RECEIPTS" | tr -d ' ')"
  [[ $after -eq $((before + 1)) ]] || { printf 'snapshot did not append exactly one receipt\n' >&2; exit 1; }
  receipt="$(jq -sc 'last' "$SBX_DECLARATIVE_CONTROL_RECEIPTS")"
  container="$("${compose[@]}" ps -q "$service")"
  started="$(docker inspect --format '{{.State.StartedAt}}' "$container")"
  mount="$(docker inspect --format '{{range .Mounts}}{{if eq .Destination "/data"}}{{.Source}}{{end}}{{end}}' "$container")"
  image="$(docker inspect --format '{{.Image}}' "$container")"
  [[ "$mount" == "$before_mount" && "$image" == "$before_image" ]] || { printf 'snapshot changed %s store mount or image\n' "$instance" >&2; exit 1; }
  printf '%s' "$receipt" | jq -e --arg project "$SBX_DECLARATIVE_COMPOSE_PROJECT" --arg instance "$instance" --arg service "$service" --arg container "$container" --arg before_container "$before_container" --arg before_started "$before_started" --arg started "$started" --arg mount "$mount" --arg image "$SBX_DECLARATIVE_BOOT_IMAGE_ID" '
    def timestamp:
      capture("^(?<sec>[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2})(\\.(?<fraction>[0-9]{1,9}))?Z$") as $t |
      [($t.sec | strptime("%Y-%m-%dT%H:%M:%S") | mktime), ((($t.fraction // "") + "000000000")[:9] | tonumber)];
    .action == "snapshot-store" and .project == $project and .instance == $instance and .service == $service and .success == true and
    .beforeContainerID == $before_container and .afterContainerID == $container and $before_container == $container and
    .beforeStartedAt == $before_started and .afterStartedAt == $started and (($started | timestamp) > ($before_started | timestamp)) and
    .sourceVolumeID == $mount and .targetVolumeID == $mount and .sourceImageID == $image and .targetImageID == $image
  ' > /dev/null
  printf 'verified %s snapshot against empty owned store and docker inspection\n' "$instance"
done
