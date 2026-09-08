#!/usr/bin/env bash
# Generate a JWT via tokensmith for API access

SOURCE_DIR="$(dirname "$BASH_SOURCE")"

docker compose  \
	-f "$SOURCE_DIR"/../compose/infra.yaml  \
	-f "$SOURCE_DIR"/../compose/bmc-sim.yaml  \
	-f "$SOURCE_DIR"/../compose/core.yaml  \
	exec -it tokensmith  \
	tokensmith user-token create  \
	--subject testing  \
	--scopes admin,audit  \
	--enable-local-user-mint  \
	--key-file /tokensmith/keys/private.pem  \
	--audience smd  \
	--issuer ${SBX_TOKENSMITH_URL:-http://127.0.0.1:27780}