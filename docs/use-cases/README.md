# Use Cases

This directory contains detailed documentation for each use case validated by the integration test suite.

## Overview

The OpenCHAMI integration sandbox validates seven use cases that exercise cross-service workflows:

| UC | Title | Services | Status |
|----|-------|----------|--------|
| [UC1](uc1-node-visibility.md) | Node Visibility | SMD, boot-service, metadata-service | ![UC1](https://github.com/OpenCHAMI/integration-sandbox/actions/workflows/ci.yml/badge.svg?event=push) |
| [UC2](uc2-multi-cluster.md) | Multi-Cluster | boot-service, metadata-service | ![UC2](https://github.com/OpenCHAMI/integration-sandbox/actions/workflows/ci.yml/badge.svg?event=push) |
| [UC3](uc3-restart-resilience.md) | Restart Resilience | All 9 services | ![UC3](https://github.com/OpenCHAMI/integration-sandbox/actions/workflows/ci.yml/badge.svg?event=push) |
| [UC4](uc4-tokensmith-smd.md) | Tokensmith + SMD | tokensmith, SMD | ![UC4](https://github.com/OpenCHAMI/integration-sandbox/actions/workflows/ci.yml/badge.svg?event=push) |
| [UC5](uc5-magellan-scan.md) | Magellan Scan | magellan, csm-rie, SMD | ![UC5](https://github.com/OpenCHAMI/integration-sandbox/actions/workflows/ci.yml/badge.svg?event=push) |
| [UC6](uc6-power-redfish.md) | Power Control | power-control, SMD, csm-rie | ![UC6](https://github.com/OpenCHAMI/integration-sandbox/actions/workflows/ci.yml/badge.svg?event=push) |
| [UC7](uc7-fru-discovery.md) | FRU Discovery | fru-tracker | ![UC7](https://github.com/OpenCHAMI/integration-sandbox/actions/workflows/ci.yml/badge.svg?event=push) |

## Quick reference

### UC1: Node Visibility
Populate SMD with nodes; node visibility chain through boot/metadata, with cloud-init lookup.

### UC2: Multi-Cluster
Two clusters with disjoint node sets; move a node between them; membership reflects on both services.

### UC3: Restart Resilience
Restart each container in turn (k8s-style); re-confirm node visibility and cross-service health.

### UC4: Tokensmith + SMD
RFC 8693 bootstrap-token mint + token exchange; JWKS signature verification; authenticated SMD write round-trips through postgres.

### UC5: Magellan Scan
Run the documented `magellan scan → collect → send` pipeline against the 8 BMC sims; verify SMD's `/Inventory/RedfishEndpoints` reflects the discovered fleet.

### UC6: Power Control
POST a `force-off`/`on` transition; observe `PowerState` mutation on the BMC sim via an independent Redfish read; reverse and re-verify.

### UC7: FRU Discovery
POST a 32-device discovery snapshot (8 nodes × CPU + 2 DIMMs); poll until reconciler completes; verify parent/child UID linkage in the device tree.

## Running use cases

### All use cases

```bash
make ci
```

### Individual use case

```bash
make up
make seed
make uc1  # or uc2, uc3, etc.
```

### Via GitHub Actions

Use cases run automatically on:
- Every push to `main`
- Every pull request
- Daily at 06:00 UTC (drift detection)
- Manual workflow dispatch

You can trigger a manual run from the [Actions tab](../../actions/workflows/ci.yml) and override:
- Image manifest (`IMAGES=release|default|edge`)
- Individual service images (`SBX_*_IMAGE`)
- Simulator skip flag (`SKIP_SIM=true`)

## Adding a new use case

1. Create a new test file: `tests/integration/uc<N>_<slug>_test.go`
2. Add the test function: `func TestUC<N>_<Description>(t *testing.T)`
3. Update this README and the main [use-cases.md](../use-cases.md)
4. Add a new job to `.github/workflows/ci.yml`
5. Run `scripts/gen-usecase-docs.sh` to generate the markdown
6. Update the main README badges

## Related documentation

- [Use cases overview](../use-cases.md) — narrative description of all cases
- [Architecture](../architecture.md) — stack components and layers
- [Troubleshooting](../troubleshooting.md) — common failure modes
- [CI Integration](../ci-integration.md) — using the sandbox from service repos
