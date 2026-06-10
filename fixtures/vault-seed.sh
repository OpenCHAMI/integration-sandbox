#!/usr/bin/env bash
# Seed Vault dev with sandbox-wide secrets:
#   - openchami/sandbox/* — KV-v2 (matches operator's seed pattern)
#   - hms-creds/<xname> — KV-v1 secrets used by power-control / remote-console
# Idempotent.
set -euo pipefail

VAULT_ADDR="${VAULT_ADDR:-http://127.0.0.1:8200}"
VAULT_TOKEN="${VAULT_TOKEN:-dev-root-token}"
CLUSTER="${CLUSTER:-sandbox}"
CONTAINER="${VAULT_CONTAINER:-sandbox-vault}"

export VAULT_ADDR VAULT_TOKEN

XNAMES=(x0c0s0b0 x0c0s1b0 x0c0s2b0 x0c0s3b0 x0c0s4b0 x0c0s5b0 x0c0s6b0 x0c0s7b0)

vault() {
  docker exec -i \
    -e VAULT_ADDR="$VAULT_ADDR" \
    -e VAULT_TOKEN="$VAULT_TOKEN" \
    "$CONTAINER" vault "$@"
}

# --- KV-v2 namespace for cluster-wide secrets (mirrors openchami-operator) ---
vault secrets enable -path=openchami kv-v2 2>/dev/null || true

PREFIX="openchami/${CLUSTER}"
vault kv put "${PREFIX}/db/credentials" \
  SMD_DB_PASSWORD="openchami" \
  BOOT_SERVICE_DB_PASSWORD="openchami" >/dev/null

vault kv put "${PREFIX}/s3/versitygw" \
  access_key="test" \
  secret_key="test" >/dev/null

vault kv put "${PREFIX}/s3/logs" \
  access_key="test" \
  secret_key="test" >/dev/null

vault kv put "${PREFIX}/oidc/tokensmith-client" \
  client_secret="sandbox-oidc-secret" >/dev/null

# --- KV-v1 mount used by HMS clients (power-control reads VAULT_KEYPATH/<xname>) ---
vault secrets enable -path=hms-creds -version=1 kv 2>/dev/null || true

for x in "${XNAMES[@]}"; do
  vault write "hms-creds/${x}" \
    Username=root \
    Password=root_password >/dev/null
done

# console SSH key entry — shape matches remote-console's expectation
vault write "hms-creds/bmc-console-keys" \
  PublicKey="ssh-ed25519 AAAA-sandbox" \
  PrivateKey="-----BEGIN OPENSSH PRIVATE KEY-----\\nsandbox\\n-----END OPENSSH PRIVATE KEY-----" >/dev/null

printf 'vault seeded for cluster=%s with %d xnames\n' "$CLUSTER" "${#XNAMES[@]}"
