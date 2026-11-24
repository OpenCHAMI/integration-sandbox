#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
log_dir="$root/logs/$(date -u +%Y%m%dT%H%M%SZ)-manifest-$$"
mkdir -p "$log_dir"
exec > >(tee -a "$log_dir/test-declarative-boot-manifest.log") 2>&1

if [[ $# != 1 || ! -f "$1" ]]; then
  printf 'usage: %s GO-TEST-JSONL\n' "$0" >&2
  exit 2
fi

jq -n --slurpfile manifest "$root/fixtures/declarative-boot/scenarios.json" --slurpfile events "$1" '
  def lifecycle: [
    "named-revision-replay-conflict", "config-content-validation", "routes-additive-legacy",
    "default-latest-pinned-rollout", "all-reference-forms", "unknown-and-no-assignment",
    "selector-strongest-family", "selector-priority-name-order", "profile-and-legacy-selection",
    "cache-hit-and-write-freshness", "cache-input-fingerprint", "concurrent-render-coherence",
    "smd-membership-freshness", "smd-refresh-fail-closed"
  ];
  def safety: [
    "assignment-shape-validation", "malformed-input-rejection", "promotion-preconditions-race",
    "invalid-references", "named-create-race", "named-update-delete-preconditions",
    "named-update-race", "uid-update-delete-race", "duplicate-and-rename",
    "status-and-alias-boundary", "auth-role-matrix", "auth-invalid-claims",
    "auth-unavailable-legacy-public", "latest-proposer-guard", "latest-follower-create-race",
    "retirement-all-aliases", "broken-winning-reference", "legacy-adoption-dry-run",
    "legacy-adoption-replay", "legacy-adoption-unsafe-preflight", "durable-restart-recreation",
    "matching-version-backup-restore"
  ];
  ([(lifecycle[] | {id: ., test: ("TestDeclarativeLifecycle/" + .)}),
    (safety[] | {id: ., test: ("TestDeclarativeSafety/" + .)})]) as $expected |
  ($manifest[0].scenarios | map(.id)) as $ids |
  [$events[] | select(.Test? != null and (.Action == "run" or .Action == "pass" or .Action == "fail" or .Action == "skip"))] as $testEvents |
  [$testEvents[] | select(.Test | test("^TestDeclarative(Lifecycle|Safety)/[^/]+$"))] as $scenarioEvents |
  [ $expected[] as $item |
    {id: $item.id, test: $item.test,
     starts: ([$scenarioEvents[] | select(.Test == $item.test and .Action == "run")] | length),
     results: [$scenarioEvents[] | select(.Test == $item.test and (.Action == "pass" or .Action == "fail" or .Action == "skip")) | .Action]} |
    . + {result: (if .starts == 1 and (.results | length) == 1 then .results[0] else "invalid" end)}
  ] as $results |
  [$scenarioEvents[] | .Test | select(. as $name | [$expected[].test] | index($name) | not)] | unique as $unexpected |
  [$testEvents[] | select(.Action == "skip") | .Test] | unique as $skipped |
  [$testEvents[] | select(.Action == "fail") | .Test] | unique as $failed |
  {manifestValid: (($manifest | length) == 1 and ($ids | length) == 36 and ($ids | unique | length) == 36 and ($ids | sort) == ([$expected[].id] | sort)),
   scenarios: $results, unexpected: $unexpected, skipped: $skipped, failedTests: $failed} |
  . + {success: (.manifestValid and (.unexpected | length) == 0 and (.skipped | length) == 0 and (.failedTests | length) == 0 and ([.scenarios[] | select(.result != "pass")] | length) == 0)} |
  ., (if .success then empty else error("scenario accounting failed") end)
'
