# Use Case 1: Node Visibility

## Overview

**Services exercised:** SMD, boot-service, metadata-service

**What it asserts:** Populate SMD with nodes; node visibility chain through boot/metadata, with cloud-init lookup.

## Description

No additional description available.

## Running the test

### Standalone (requires stack to be up)

```bash
make up
make seed
make uc1
```

### As part of full CI

```bash
make ci
```

### Via GitHub Actions

The CI workflow runs this use case as job `test-uc1`. You can view the results in the [Actions tab](../../actions/workflows/ci.yml).

## Test implementation

- **File:** `tests/integration/uc1_node_visibility_test.go`
- **Test function:** `TestUC1_*`
- **Build tag:** `integration`

## Expected behavior

When this test passes, it confirms:

Populate SMD with nodes; node visibility chain through boot/metadata, with cloud-init lookup.

## Troubleshooting

If this test fails:

1. Check the job summary in GitHub Actions for the specific assertion that failed.
2. Download the `uc1-logs-*` artifact from the failed run.
3. Inspect the service logs in the artifact bundle.
4. Consult the [troubleshooting guide](../troubleshooting.md) for common failure modes.

## Related documentation

- [Architecture](../architecture.md) — overall stack layout
- [Use cases overview](../use-cases.md) — all use cases at a glance
- [Reference: Endpoints](../reference-endpoints.md) — service URLs and ports
- [Reference: Fixtures](../reference-fixtures.md) — seed data format
