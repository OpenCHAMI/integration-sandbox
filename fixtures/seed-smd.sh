#!/usr/bin/env bash
# Push smd-components.json and redfish-endpoints.json into SMD.
# Idempotent: components POST upserts; redfish endpoints POST is all-or-nothing
# and 409s on any pre-existing ID, so we PUT each endpoint individually
# (also upsert) instead.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SMD_URL="${SMD_URL:-http://127.0.0.1:27779}"

# Components
http_code=$(curl -s -o /tmp/smd-comp.out -w '%{http_code}' \
  -H "Content-Type: application/json" \
  --data @"$ROOT/fixtures/smd-components.json" \
  -X POST "${SMD_URL}/hsm/v2/State/Components")
case "$http_code" in
  200|201|204) printf '[smd] components seeded (HTTP %s)\n' "$http_code" ;;
  *)           printf '[smd] components POST -> HTTP %s; body: %s\n' "$http_code" "$(cat /tmp/smd-comp.out)"; exit 1 ;;
esac

# Redfish endpoints — POST per-ID, accepting 409 (already exists) as success.
# SMD has no upsert primitive: bulk POST 409s on any pre-existing ID, PATCH/PUT
# require the entry to exist. So we try POST and treat conflict as "already
# seeded by a prior run or by a UC that left endpoints in place".
created=0
existed=0
ids=$(python3 -c 'import json,sys;print("\n".join(e["ID"] for e in json.load(open(sys.argv[1]))["RedfishEndpoints"]))' "$ROOT/fixtures/redfish-endpoints.json")
for id in $ids; do
  body=$(python3 -c '
import json, sys
data = json.load(open(sys.argv[1]))
for e in data["RedfishEndpoints"]:
    if e["ID"] == sys.argv[2]:
        print(json.dumps(e))
        break
' "$ROOT/fixtures/redfish-endpoints.json" "$id")
  http_code=$(curl -s -o /tmp/smd-rf.out -w '%{http_code}' \
    -H "Content-Type: application/json" \
    --data "$body" \
    -X POST "${SMD_URL}/hsm/v2/Inventory/RedfishEndpoints")
  case "$http_code" in
    200|201|204) created=$((created+1)) ;;
    409) existed=$((existed+1)) ;;
    *) printf '[smd] redfish POST %s -> HTTP %s; body: %s\n' "$id" "$http_code" "$(cat /tmp/smd-rf.out)"; exit 1 ;;
  esac
done
printf '[smd] redfish endpoints seeded (%d created, %d already present)\n' "$created" "$existed"
