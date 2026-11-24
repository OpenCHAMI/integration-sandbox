#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
log_dir="$root/logs/$(date -u +%Y%m%dT%H%M%SZ)-classification-$$"
mkdir -p "$log_dir"
exec 2> >(tee -a "$log_dir/errors.log" >&2)
if [[ $# != 4 ]]; then
  printf 'usage: %s GO-JSONL PROVENANCE OPENAPI BOOT-ACCESS-LOG\n' "$0" >&2
  exit 2
fi

jq -n --slurpfile events "$1" --slurpfile provenance "$2" --slurpfile spec "$3" --rawfile access "$4" '
  def instant:
    try (
      capture("^(?<seconds>[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2})(\\.(?<fraction>[0-9]{1,9}))?(?<zone>Z|[+-][0-9]{2}:[0-9]{2})$") as $t |
      ($t.seconds | strptime("%Y-%m-%dT%H:%M:%S") | mktime) as $seconds |
      (if $t.zone == "Z" then 0 else
        (($t.zone[1:3] | tonumber) * 3600 + ($t.zone[4:6] | tonumber) * 60) * (if $t.zone[0:1] == "+" then 1 else -1 end)
      end) as $offset |
      [($seconds - $offset), ((($t.fraction // "") + "000000000")[:9] | tonumber)]
    ) catch null;
  def scenario: (.Test // "" | test("^TestDeclarative(Lifecycle|Safety)/[^/]+$"));
  [$events[] | select(scenario and .Action == "run") | .Test as $name |
    {name:$name,start:(.Time | instant),ends:[$events[] | select(.Test == $name and (.Action == "pass" or .Action == "fail" or .Action == "skip")) | .Time | instant]} |
    . + {end:(if (.ends | length) == 1 then .ends[0] else null end)}
  ] as $windows |
  [$access | split("\n")[] |
    (try capture("^(?<service>[^| ]+)\\s+\\|\\s+(?<time>[^ ]+)\\s+.*\"(?<method>[A-Z]+) (?<url>[^ ]+) HTTP/[^\"]+\"(?:.* - )?\\s*(?<status>[0-9]{3})(\\s|$)") catch empty) |
    . + {moment:(.time | instant),path:(.url | sub("^https?://[^/]+"; "") | split("?")[0]),status:(.status | tonumber)} |
    select(.moment != null)
  ] as $requests |
  (if ($provenance | length) == 1 and ($provenance[0].clockStart | instant) != null and ($provenance[0].clockEnd | instant) != null and
      ($provenance[0].clockProbe | type) == "string" and ($provenance[0].clockProbe | length) > 0 and $provenance[0].clockStatus == 200 then
     [$requests[] | select((.service == "boot-1" or .service == "boot") and .method == "GET" and .status == 200 and
       .url == ("http://" + $provenance[0].bootOrigin + "/health?classification_probe=" + $provenance[0].clockProbe) and
       .moment >= ($provenance[0].clockStart | instant) and .moment <= ($provenance[0].clockEnd | instant))] | length == 1
   else false end) as $clockAligned |
  (($provenance | length) == 1 and ($spec | length) == 1 and $clockAligned and
    $provenance[0].imageID == "sha256:df546b2e5a88805b86c3a0bb6cd9823da9c55a96714b35fb37e48c0103bb269b" and
    $provenance[0].imageRevision == "fbe7a63b7e1a23433a296172f65bb692b8636b0e" and
    $provenance[0].sourceHead == "fbe7a63b7e1a23433a296172f65bb692b8636b0e" and
    $provenance[0].sourceClean == true and
    $provenance[0].openapiStatus == 200 and ($spec[0].openapi | type) == "string" and ($spec[0].paths | type) == "object" and
    ($spec[0].paths | has("/bootconfigs") | not) and ($spec[0].paths | has("/bootassignments") | not)) as $baseline |
  def served($path):
    ($path | split("/")) as $actual |
    any(($spec[0].paths | keys[]); (split("/")) as $pattern |
      ($pattern | length) == ($actual | length) and
      all(range(0; ($pattern | length)); . as $i | $pattern[$i] == $actual[$i] or ($pattern[$i] | test("^\\{[^/]+\\}$"))));
  {baselineEvidence:$baseline, scenarios:[
    $events[] | select(scenario and (.Action == "pass" or .Action == "fail" or .Action == "skip")) | . as $end |
    [$windows[] | select(.name == $end.Test)] as $matches |
    [$events[] | select(.Test == $end.Test and .Action == "output") |
      (try (.Output | capture("(?i)HTTP status (?<value>404|405)\\b").value | tonumber) catch empty)] | unique as $statuses |
    (if ($matches | length) == 1 and $matches[0].start != null and $matches[0].end != null and ($statuses | length) == 1 then
      [$requests[] | select(.status == $statuses[0] and .moment >= $matches[0].start and .moment <= $matches[0].end)] | sort_by(.moment)
     else [] end) as $requestsInWindow |
    (if ($matches | length) == 1 and $matches[0].start != null and $matches[0].end != null then
      [$requests[] | select(.status >= 300 and .moment >= $matches[0].start and .moment <= $matches[0].end)]
     else [] end) as $nonSuccessRequests |
    (if ($requestsInWindow | length) == 1 then
      [$windows[] | select(.start != null and .start <= $requestsInWindow[0].moment and (.end == null or .end >= $requestsInWindow[0].moment))] | length
     else 0 end) as $owners |
    [$events[] | select(.Test == $end.Test and .Action == "output" and (.Output // "" | test("^\\s+[^:\\n]+\\.go:[0-9]+:"))) |
      {text:.Output,time:.Time,moment:(.Time | instant)}] | sort_by(.moment) as $failureLines |
    (if $end.Action == "fail" and $baseline and
        (["TestDeclarativeSafety/legacy-adoption-dry-run","TestDeclarativeSafety/legacy-adoption-replay","TestDeclarativeSafety/legacy-adoption-unsafe-preflight"] | index($end.Test)) != null and
        ($statuses == [405]) and ($failureLines | length) == 2 and
        ($failureLines[0].text | test("^\\s+safety_adoption_cases_test\\.go:[0-9]+: HTTP status 405; want 201\\s*$")) and
        ($failureLines[1].text | test("^\\s+safety_adoption_support_test\\.go:[0-9]+: adoption cleanup HTTP 405 or invalid ETag\\s*$")) and
        $failureLines[0].moment != null and $failureLines[1].moment != null and ($requestsInWindow | length) == 2 and ($nonSuccessRequests | length) == 2 then
      $requestsInWindow as $r |
      (all($r[]; . as $request |
        ([ $windows[] | select(.start != null and .start <= $request.moment and (.end == null or .end >= $request.moment)) ] | length) == 1 and
        (.service == "boot-1" or .service == "boot") and (.url | startswith("http://" + $provenance[0].bootOrigin + "/")) and
        (served(.path) | not)) and
       $r[0].method == "POST" and $r[0].path == "/bootconfigs" and $r[0].moment <= $failureLines[0].moment and
       $r[1].method == "GET" and ($r[1].path | test("^/bootassignments/by-name/[^/]+$")) and
       $r[1].moment > $failureLines[0].moment and $r[1].moment <= $failureLines[1].moment)
     else false end) as $sameCauseCleanup |
    (if $end.Action == "fail" and $baseline and ($requestsInWindow | length) == 1 and $owners == 1 and
        ($failureLines | length) == 1 and $failureLines[0].moment != null and
        ($failureLines[0].text | test("^\\s+[^:\\n]+\\.go:[0-9]+:\\s+(unexpected )?HTTP status " + ($statuses[0] | tostring) + "\\b")) then
      $requestsInWindow[0] as $primary |
      (($primary.service == "boot-1" or $primary.service == "boot") and
       ($primary.url | startswith("http://" + $provenance[0].bootOrigin + "/")) and
       $primary.method == "POST" and ($primary.path == "/bootconfigs" or $primary.path == "/bootassignments") and
       (served($primary.path) | not) and $primary.moment < $failureLines[0].moment and
       all($nonSuccessRequests[];
         . == $primary or
         ((["TestDeclarativeLifecycle/routes-additive-legacy", "TestDeclarativeLifecycle/default-latest-pinned-rollout", "TestDeclarativeLifecycle/all-reference-forms", "TestDeclarativeLifecycle/unknown-and-no-assignment", "TestDeclarativeLifecycle/selector-strongest-family", "TestDeclarativeLifecycle/selector-priority-name-order", "TestDeclarativeLifecycle/profile-and-legacy-selection", "TestDeclarativeLifecycle/cache-hit-and-write-freshness", "TestDeclarativeLifecycle/cache-input-fingerprint", "TestDeclarativeLifecycle/concurrent-render-coherence", "TestDeclarativeLifecycle/smd-membership-freshness", "TestDeclarativeLifecycle/smd-refresh-fail-closed"] | index($end.Test)) != null and (.service == "smd-1" or .service == "smd") and .method == "GET" and .status == 404 and
          (.path | test("^/hsm/v2/(State/Components|Inventory/EthernetInterfaces|groups)/[^/]+$"))) or
         ($end.Test == "TestDeclarativeLifecycle/routes-additive-legacy" and (.service == "boot-1" or .service == "boot") and
          (.url | startswith("http://" + $provenance[0].bootOrigin + "/")) and .method == "GET" and .status == 301 and
          (.path == "/bootparameters/" or .path == "/boot/v1/bootparameters/"))))
     else false end) as $direct |
    {test:$end.Test,result:$end.Action,classification:
      (if $sameCauseCleanup then "unsupported-new-api-405-cleanup-unsupported"
       elif $direct
       then "unsupported-new-api-" + ($statuses[0] | tostring) else "unclassified" end)} +
    (if $sameCauseCleanup then {evidence:[$requestsInWindow[] | {time,method,path,status}]}
     elif $direct then {evidence:[$requestsInWindow[] | {time,method,path,status}],firstFailureAt:$failureLines[0].time} else {} end)
  ]} | . + {provenanceEvidence:.baselineEvidence,
    baselineEvidence:(.baselineEvidence and all(.scenarios[]; .result != "fail" or .classification != "unclassified"))}
' | tee -a "$log_dir/classification.json"
