# Use Case 7: FRU Discovery

## Overview

**Services exercised:** fru-tracker

**What it asserts:** POST a 32-device discovery snapshot (8 nodes × CPU + 2 DIMMs); poll until reconciler completes; verify parent/child UID linkage in the device tree.

## Description

No additional description available.

## Running the test

### Standalone (requires stack to be up)

```bash
make up
make seed
make uc7
```

### As part of full CI

```bash
make ci
```

### Via GitHub Actions

The CI workflow runs this use case as job `test-uc7`. You can view the results in the [Actions tab](../../actions/workflows/ci.yml).

## Test implementation

- **File:** `tests/integration/uc7_fru_discovery_test.go`
- **Test function:** `TestUC7_*`
- **Build tag:** `integration`

## Expected behavior

When this test passes, it confirms:

POST a 32-device discovery snapshot (8 nodes × CPU + 2 DIMMs); poll until reconciler completes; verify parent/child UID linkage in the device tree.

## Troubleshooting

If this test fails:

1. Check the job summary in GitHub Actions for the specific assertion that failed.
2. Download the `uc7-logs-*` artifact from the failed run.
3. Inspect the service logs in the artifact bundle.
4. Consult the [troubleshooting guide](../troubleshooting.md) for common failure modes.

## Related documentation

- [Architecture](../architecture.md) — overall stack layout
- [Use cases overview](../use-cases.md) — all use cases at a glance
- [Reference: Endpoints](../reference-endpoints.md) — service URLs and ports
- [Reference: Fixtures](../reference-fixtures.md) — seed data format
