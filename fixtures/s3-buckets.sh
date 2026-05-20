#!/usr/bin/env bash
# Create localstack S3 buckets and seed minimal artifacts. Idempotent.
#
# We run awslocal *inside* the localstack container (which already has aws-cli
# installed) instead of relying on a host-side awscli, so the sandbox stays
# self-contained.
set -euo pipefail

CONTAINER="${LOCALSTACK_CONTAINER:-sandbox-localstack}"
BUCKETS=(boot-images openchami-logs parquet)

aw() { docker exec -i "$CONTAINER" awslocal "$@"; }

for b in "${BUCKETS[@]}"; do
  if ! aw s3api head-bucket --bucket "$b" 2>/dev/null; then
    aw s3 mb "s3://${b}" >/dev/null
    printf '[s3] created bucket %s\n' "$b"
  else
    printf '[s3] bucket %s exists\n' "$b"
  fi
done

# Seed sentinel objects via stdin so we don't need to touch container fs.
printf '#!ipxe\necho sandbox boot image\nboot\n' | aw s3 cp - s3://boot-images/sandbox.ipxe >/dev/null
cat <<'YAML' | aw s3 cp - s3://boot-images/cloud-init.yaml >/dev/null
#cloud-config
hostname: {{ name }}
users:
  - name: openchami
    sudo: ALL=(ALL) NOPASSWD:ALL
    ssh_authorized_keys:
      - ssh-ed25519 AAAA-sandbox
YAML

printf '[s3] seeded sentinel objects\n'
