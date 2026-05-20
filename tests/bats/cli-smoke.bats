#!/usr/bin/env bats

# CLI smoke tests against the running sandbox. Each test is self-contained;
# none mutate state. If anything fails here, the suite is unhappy.

setup() {
  : "${SBX_VAULT_URL:=http://127.0.0.1:8200}"
  : "${SBX_SMD_URL:=http://127.0.0.1:27779}"
  : "${SBX_LOCALSTACK_URL:=http://127.0.0.1:4566}"
  export VAULT_ADDR="$SBX_VAULT_URL"
  export VAULT_TOKEN="${VAULT_TOKEN:-dev-root-token}"
  export AWS_ACCESS_KEY_ID=test
  export AWS_SECRET_ACCESS_KEY=test
  export AWS_DEFAULT_REGION=us-east-1
  export AWS_ENDPOINT_URL="$SBX_LOCALSTACK_URL"
}

@test "vault status reports unsealed" {
  run vault status -format=json
  [ "$status" -eq 0 ]
  echo "$output" | jq -e '.sealed == false' >/dev/null
}

@test "vault has openchami/sandbox/db/credentials" {
  run vault kv get -format=json openchami/sandbox/db/credentials
  [ "$status" -eq 0 ]
}

@test "vault has hms-creds/x0c0s0b0" {
  run vault read -format=json hms-creds/x0c0s0b0
  [ "$status" -eq 0 ]
  echo "$output" | jq -e '.data.Username == "root"' >/dev/null
}

@test "localstack has the boot-images bucket" {
  run docker exec sandbox-localstack awslocal s3 ls s3://boot-images/
  [ "$status" -eq 0 ]
}

@test "localstack boot-images contains sandbox.ipxe" {
  run docker exec sandbox-localstack awslocal s3 ls s3://boot-images/sandbox.ipxe
  [ "$status" -eq 0 ]
}

@test "smd /service/ready returns 200" {
  run curl -s -o /dev/null -w '%{http_code}' "${SBX_SMD_URL}/hsm/v2/service/ready"
  [ "$status" -eq 0 ]
  [ "$output" = "200" ]
}
