.DEFAULT_GOAL := help

ROOT := $(shell pwd)
# Default to the `release` manifest — each OpenCHAMI service is pinned to its
# latest GitHub Release tag (regenerate with `make refresh-releases`). Use
# IMAGES=default for the floating :latest tags, IMAGES=edge for :main builds.
IMAGES ?= release
COMPOSE := docker compose -f compose/infra.yaml -f compose/bmc-sim.yaml -f compose/core.yaml

# Export so scripts/load-images.sh sees it.
export IMAGES

.PHONY: help build-images up down seed test test-integration test-bats ci tail clean reset show-images refresh-releases

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk -F':.*## ' '{printf "  %-20s %s\n", $$1, $$2}'
	@echo ""
	@echo "Image selection (default: release — latest GitHub Release tag of each service):"
	@echo "  make ci                                                  # images/release.env (default)"
	@echo "  make ci IMAGES=default                                   # images/default.env (floating :latest)"
	@echo "  make ci IMAGES=edge                                      # images/edge.env (:main builds)"
	@echo "  make ci IMAGES=release-v1.0                              # images/release-v1.0.env (pinned snapshot)"
	@echo "  make ci SBX_TOKENSMITH_IMAGE=ghcr.io/openchami/tokensmith:pr-23"
	@echo "  make ci IMAGES=edge SBX_BOOT_IMAGE=ghcr.io/openchami/boot-service:pr-7"
	@echo ""
	@echo "  make refresh-releases                                    # re-resolve latest releases (network)"

refresh-releases: ## Re-query GitHub for each service's latest release; rewrites images/release.env (network required)
	@bash scripts/resolve-latest-releases.sh

show-images: ## Print the resolved image set for the current IMAGES + overrides
	@bash -c 'set -e; source scripts/load-images.sh >/dev/null; \
	  for v in SBX_VAULT_IMAGE SBX_LOCALSTACK_IMAGE SBX_POSTGRES_IMAGE \
	           SBX_SUSHY_IMAGE SBX_IPMI_SIM_IMAGE \
	           SBX_SMD_IMAGE SBX_TOKENSMITH_IMAGE SBX_BOOT_IMAGE \
	           SBX_METADATA_IMAGE SBX_FRU_IMAGE SBX_POWER_IMAGE SBX_MAGELLAN_IMAGE; do \
	    printf "  %-22s %s\n" "$$v" "$${!v}"; \
	  done'

build-images: ## Pull (or build) every image referenced by the manifest
	@bash scripts/build-images.sh

up: build-images ## Bring up the entire stack (infra + bmc-sim + core), wait for /health
	@bash scripts/up.sh

down: ## Tear down all stacks and remove volumes
	@bash scripts/down.sh

seed: ## Seed Vault, S3, and SMD with fixtures (idempotent)
	@bash fixtures/vault-seed.sh
	@bash fixtures/s3-buckets.sh
	@bash fixtures/seed-smd.sh

test-integration: ## Run the Go integration suite against the running stack
	@cd tests && go test -tags integration -count=1 -v -timeout 10m ./integration/...

uc1: ## UC1 — populate SMD with nodes, verify visibility in boot-service + metadata-service
	@cd tests && go test -tags integration -count=1 -v -timeout 5m -run '^TestUC1_' ./integration/...

uc2: ## UC2 — two clusters with disjoint nodes; move one node between them
	@cd tests && go test -tags integration -count=1 -v -timeout 5m -run '^TestUC2_' ./integration/...

uc3: ## UC3 — restart each container; confirm node register/read still works
	@cd tests && go test -tags integration -count=1 -v -timeout 15m -run '^TestUC3_' ./integration/...

uc-all: uc1 uc2 uc3 ## Run all use-case suites in sequence

test-bats: ## Run the bats CLI smoke suite against the running stack
	@bats tests/bats/

test: test-bats test-integration ## Run both test suites
# bats runs first because UC3 restarts vault (dev mode = in-memory) and would
# wipe the seeded secrets that bats asserts on. Integration tests are
# self-sufficient and don't depend on bats results.

ci: ## End-to-end automation: up → seed → test → down (with log bundle on failure)
	@bash scripts/heartbeat.sh ci-starting "make ci begin (IMAGES=$(IMAGES))"
	@set -e; \
	  trap 'rc=$$?; if [ $$rc -ne 0 ]; then bash scripts/log-bundle.sh ci-failure >/dev/null; bash scripts/heartbeat.sh ci-failed "exit $$rc"; fi; bash scripts/down.sh >/dev/null 2>&1 || true; exit $$rc' EXIT; \
	  $(MAKE) build-images; \
	  $(MAKE) up; \
	  $(MAKE) seed; \
	  $(MAKE) test
	@bash scripts/heartbeat.sh ci-passed "make ci green (IMAGES=$(IMAGES))"

tail: ## Stream PROGRESS.log + STATUS for phone-side visibility
	@printf '== STATUS ==\n'; cat STATUS 2>/dev/null; printf '\n== PROGRESS ==\n'; tail -F PROGRESS.log

clean: down ## Alias for down + remove logs/
	@rm -rf logs/* 2>/dev/null || true

reset: clean build-images ## Hard reset: down, clear logs, re-pull images
	@:
