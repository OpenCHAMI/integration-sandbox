#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
run="$root/logs/issue50/classifier-test-$(date -u +%Y%m%dT%H%M%SZ)-$$"
mkdir -p "$run"
exec > >(tee -a "$run/classifier-test.log") 2>&1
image=sha256:df546b2e5a88805b86c3a0bb6cd9823da9c55a96714b35fb37e48c0103bb269b
head=fbe7a63b7e1a23433a296172f65bb692b8636b0e
for case_name in 404 405 assignments smd400 404-other wrong-image wrong-head wrong-revision dirty-source wrong-method wrong-path present-path present-assignments bad-openapi ambiguous other-service overlap missing-trace bad-clock direct-old-error direct-smd-error direct-smd-http direct-other-error direct-bad-order direct-old-http direct-smd-absence direct-unexpected-smd404 direct-safety-smd404 direct-expected-redirect adoption-dry adoption-replay adoption-unsafe adoption-old-conflict adoption-old-access adoption-extra-405 adoption-template adoption-wrong-image adoption-reversed; do
  status=405 method=POST path=/bootconfigs service=boot-1 selected_image="$image" selected_head="$head" selected_revision="$head" clean=true openapi_status=200
  test_name=TestDeclarativeSafety/named-create-race
  case "$case_name" in
    404) status=404 ;;
    assignments) path=/bootassignments ;;
    smd400) status=400; service=smd-1; path=/hsm/v2/State/Components ;;
    404-other) status=404; path=/other ;;
    wrong-image) selected_image=sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa ;;
    wrong-head) selected_head=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa ;;
    wrong-revision) selected_revision=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa ;;
    dirty-source) clean=false ;;
    bad-openapi) openapi_status=503 ;;
    wrong-method) method=GET ;;
    wrong-path) path=/bootconfigurations ;;
    direct-old-error|direct-smd-error|direct-smd-http|direct-other-error|direct-bad-order|direct-old-http|direct-unexpected-smd404) test_name=TestDeclarativeLifecycle/named-revision-replay-conflict ;;
    direct-smd-absence) test_name=TestDeclarativeLifecycle/default-latest-pinned-rollout ;;
    direct-expected-redirect) test_name=TestDeclarativeLifecycle/routes-additive-legacy ;;
    adoption-dry|adoption-old-conflict|adoption-old-access|adoption-extra-405|adoption-template|adoption-wrong-image|adoption-reversed) test_name=TestDeclarativeSafety/legacy-adoption-dry-run ;;
    adoption-replay) test_name=TestDeclarativeSafety/legacy-adoption-replay ;;
    adoption-unsafe) test_name=TestDeclarativeSafety/legacy-adoption-unsafe-preflight ;;
  esac
  if [[ "$case_name" == adoption-wrong-image ]]; then selected_image=sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa; fi
  jq -nc --arg image "$selected_image" --arg head "$selected_head" --arg revision "$selected_revision" --argjson clean "$clean" --argjson status "$openapi_status" '{imageID:$image,imageRevision:$revision,sourceHead:$head,sourceClean:$clean,openapiStatus:$status,bootOrigin:"127.0.0.1:42421",clockProbe:"clock-id",clockStart:"2026-11-12T20:19:51.100000Z",clockEnd:"2026-11-12T20:19:51.400000Z",clockStatus:200}' > "$run/provenance.json"
  if [[ "$case_name" == present-path ]]; then
    jq -nc '{openapi:"3.0.0",paths:{"/bootconfigs":{},"/bootconfigurations":{}}}' > "$run/openapi.json"
  elif [[ "$case_name" == present-assignments ]]; then
    jq -nc '{openapi:"3.0.0",paths:{"/bootassignments":{},"/bootconfigurations":{}}}' > "$run/openapi.json"
  elif [[ "$case_name" == adoption-template ]]; then
    jq -nc '{openapi:"3.0.0",paths:{"/bootassignments/by-name/{name}":{},"/bootconfigurations":{}}}' > "$run/openapi.json"
  else
    jq -nc '{openapi:"3.0.0",paths:{"/bootconfigurations":{}}}' > "$run/openapi.json"
  fi
  if [[ "$case_name" == smd400 ]]; then
    message='    lifecycle_smd_fixture_test.go:54: create SMD component: unexpected HTTP status 400 (expected [200 201 204])'
  else
    message="    lifecycle_revisions_test.go:35: HTTP status $status; want 201"
  fi
  if [[ "$case_name" == adoption-* ]]; then
    jq -nc --arg test "$test_name" '
      {Time:"2026-11-12T15:19:52.100000000-05:00",Action:"run",Test:$test},
      {Time:"2026-11-12T15:19:52.300000000-05:00",Action:"output",Test:$test,Output:"    safety_adoption_cases_test.go:15: HTTP status 405; want 201\n"},
      {Time:"2026-11-12T15:19:52.350000000-05:00",Action:"output",Test:$test,Output:"    safety_adoption_support_test.go:97: adoption cleanup HTTP 405 or invalid ETag\n"},
      {Time:"2026-11-12T15:19:52.400000000-05:00",Action:"fail",Test:$test}' > "$run/go.jsonl"
  else
    jq -nc --arg message "$message" --arg test "$test_name" '
      {Time:"2026-11-12T15:19:52.100000000-05:00",Action:"run",Test:$test},
      {Time:"2026-11-12T15:19:52.300000000-05:00",Action:"output",Test:$test,Output:($message + "\n")},
      {Time:"2026-11-12T15:19:52.400000000-05:00",Action:"fail",Test:$test}' > "$run/go.jsonl"
  fi
  printf 'boot-1 | 2026-11-12T20:19:51.200000000Z server "GET http://127.0.0.1:42421/health?classification_probe=clock-id HTTP/1.1" from 127.0.0.1 - 200 0B\n' > "$run/boot-access.log"
  request_time=2026-11-12T20:19:52.200000000Z
  if [[ "$case_name" == direct-bad-order ]]; then request_time=2026-11-12T20:19:52.350000000Z; fi
  printf '%s | %s server "%s http://127.0.0.1:42421%s?revisionName=a HTTP/1.1" from 127.0.0.1 - %s 0B\n' "$service" "$request_time" "$method" "$path" "$status" >> "$run/boot-access.log"
  case "$case_name" in
    direct-old-error) extra='    legacy_test.go:42: unexpected HTTP status 301 (expected [200])' ;;
    direct-smd-error) extra='    lifecycle_smd_fixture_test.go:54: create SMD component: unexpected HTTP status 400 (expected [201])' ;;
    direct-other-error) extra='    other_test.go:23: unexpected setup error' ;;
    *) extra='' ;;
  esac
  if [[ -n "$extra" ]]; then
    jq -nc --arg test "$test_name" --arg message "$extra" '{Time:"2026-11-12T15:19:52.250000000-05:00",Action:"output",Test:$test,Output:($message + "\n")}' >> "$run/go.jsonl"
  fi
  if [[ "$case_name" == direct-old-http || "$case_name" == direct-expected-redirect ]]; then
    printf 'boot-1 | 2026-11-12T20:19:52.150000000Z server "GET http://127.0.0.1:42421/bootparameters/ HTTP/1.1" from 127.0.0.1 - 301 0B\n' >> "$run/boot-access.log"
  fi
  if [[ "$case_name" == direct-smd-absence || "$case_name" == direct-unexpected-smd404 || "$case_name" == direct-safety-smd404 ]]; then
    printf 'smd-1 | 2026-11-12T20:19:52.150000000Z server "GET http://127.0.0.1:42423/hsm/v2/State/Components/x1c0s0b0n0 HTTP/1.1" from 127.0.0.1 - 404 0B\n' >> "$run/boot-access.log"
  fi
  if [[ "$case_name" == direct-smd-error || "$case_name" == direct-smd-http ]]; then
    printf 'smd-1 | 2026-11-12T20:19:52.150000000Z server "POST http://127.0.0.1:42423/hsm/v2/State/Components HTTP/1.1" from 127.0.0.1 - 400 0B\n' >> "$run/boot-access.log"
  fi
  if [[ "$case_name" == adoption-* ]]; then
    cleanup_time=2026-11-12T20:19:52.320000000Z
    if [[ "$case_name" == adoption-reversed ]]; then cleanup_time=2026-11-12T20:19:52.220000000Z; fi
    printf 'boot-1 | %s server "GET http://127.0.0.1:42421/bootassignments/by-name/test HTTP/1.1" from 127.0.0.1 - 405 0B\n' "$cleanup_time" >> "$run/boot-access.log"
  fi
  if [[ "$case_name" == adoption-old-conflict ]]; then
    jq -nc --arg test "$test_name" '{Time:"2026-11-12T15:19:52.330000000-05:00",Action:"output",Test:$test,Output:"    legacy_test.go:12: unexpected HTTP status 301 (expected [200])\n"}' >> "$run/go.jsonl"
  fi
  if [[ "$case_name" == adoption-extra-405 ]]; then
    printf 'boot-1 | 2026-11-12T20:19:52.330000000Z server "GET http://127.0.0.1:42421/legacy HTTP/1.1" from 127.0.0.1 - 405 0B\n' >> "$run/boot-access.log"
  fi
  if [[ "$case_name" == adoption-old-access ]]; then
    printf 'boot-1 | 2026-11-12T20:19:52.330000000Z server "GET http://127.0.0.1:42421/bootparameters/ HTTP/1.1" from 127.0.0.1 - 301 0B\n' >> "$run/boot-access.log"
  fi
  if [[ "$case_name" == ambiguous ]]; then
    printf 'boot-1 | 2026-11-12T20:19:52.250000000Z server "GET http://127.0.0.1:42421/other HTTP/1.1" from 127.0.0.1 - 405 0B\n' >> "$run/boot-access.log"
  fi
  if [[ "$case_name" == other-service ]]; then
    printf 'smd-1 | 2026-11-12T20:19:52.250000000Z 127.0.0.1 - - "POST /hsm/v2/State/Components HTTP/1.1" 405 0B\n' >> "$run/boot-access.log"
  fi
  if [[ "$case_name" == overlap ]]; then
    jq -nc '{Time:"2026-11-12T15:19:52.150000000-05:00",Action:"run",Test:"TestDeclarativeSafety/named-update-race"}, {Time:"2026-11-12T15:19:52.350000000-05:00",Action:"fail",Test:"TestDeclarativeSafety/named-update-race"}' >> "$run/go.jsonl"
  fi
  if [[ "$case_name" == missing-trace ]]; then : > "$run/boot-access.log"; fi
  if [[ "$case_name" == bad-clock ]]; then
    jq '.clockStart = "2026-11-12T20:19:50Z" | .clockEnd = "2026-11-12T20:19:51Z"' "$run/provenance.json" > "$run/bad-clock.json"
    mv "$run/bad-clock.json" "$run/provenance.json"
  fi
  bash "$root/scripts/test-declarative-boot-classify.sh" "$run/go.jsonl" "$run/provenance.json" "$run/openapi.json" "$run/boot-access.log" > "$run/result.json"
  actual="$(jq -er '.scenarios[0].classification' "$run/result.json")"
  case "$case_name:$actual" in
    404:unsupported-new-api-404|405:unsupported-new-api-405|assignments:unsupported-new-api-405|direct-smd-absence:unsupported-new-api-405|direct-expected-redirect:unsupported-new-api-405|adoption-dry:unsupported-new-api-405-cleanup-unsupported|adoption-replay:unsupported-new-api-405-cleanup-unsupported|adoption-unsafe:unsupported-new-api-405-cleanup-unsupported|smd400:unclassified|404-other:unclassified|wrong-image:unclassified|wrong-head:unclassified|wrong-revision:unclassified|dirty-source:unclassified|wrong-method:unclassified|wrong-path:unclassified|present-path:unclassified|present-assignments:unclassified|bad-openapi:unclassified|ambiguous:unclassified|other-service:unclassified|overlap:unclassified|missing-trace:unclassified|bad-clock:unclassified|direct-old-error:unclassified|direct-smd-error:unclassified|direct-smd-http:unclassified|direct-unexpected-smd404:unclassified|direct-safety-smd404:unclassified|direct-other-error:unclassified|direct-bad-order:unclassified|direct-old-http:unclassified|adoption-old-conflict:unclassified|adoption-old-access:unclassified|adoption-extra-405:unclassified|adoption-template:unclassified|adoption-wrong-image:unclassified|adoption-reversed:unclassified) ;;
    *) printf 'unexpected classification for %s: %s\n' "$case_name" "$actual" >&2; exit 1 ;;
  esac
  if [[ "$actual" == unclassified ]]; then
    jq -e '.baselineEvidence == false' "$run/result.json" > /dev/null
  fi
  if [[ "$actual" == unsupported-new-api-405-cleanup-unsupported ]]; then
    jq -e '.scenarios[0].evidence | length == 2 and .[0].method == "POST" and .[0].path == "/bootconfigs" and .[1].method == "GET" and .[1].path == "/bootassignments/by-name/test"' "$run/result.json" > /dev/null
  fi
  printf '%s: %s\n' "$case_name" "$actual"
done

baseline="$root/logs/issue50/run-20261113131920-0c4abaa1fb48"
if [[ -f "$baseline/go.jsonl" && -f "$baseline/boot-access.log" && -f "$baseline/openapi-classifier.json" && -f "$baseline/classification-provenance.json" ]]; then
  jq -c . "$baseline/go.jsonl" > "$run/verifier-counterexample.jsonl"
  jq -nc '{Time:"2026-11-13T08:19:40.292000-05:00",Action:"output",Test:"TestDeclarativeLifecycle/named-revision-replay-conflict",Output:"    legacy_test.go:42: unexpected HTTP status 301 (expected [200])\n"}' >> "$run/verifier-counterexample.jsonl"
  bash "$root/scripts/test-declarative-boot-classify.sh" "$run/verifier-counterexample.jsonl" "$baseline/classification-provenance.json" "$baseline/openapi-classifier.json" "$baseline/boot-access.log" > "$run/verifier-counterexample-result.json"
  jq -e '.baselineEvidence == false and ([.scenarios[] | select(.test == "TestDeclarativeLifecycle/named-revision-replay-conflict" and .classification == "unclassified")] | length == 1)' "$run/verifier-counterexample-result.json" > /dev/null
  printf 'recorded-run verifier counterexample: unclassified and baselineEvidence false\n'
fi
