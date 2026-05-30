# Use Case 3: Restart Resilience

## Overview

**Services exercised:** All 9 services

**What it asserts:** Restart each container in turn (k8s-style); re-confirm node visibility and cross-service health.

## Description

No additional description available.

## Running the test

### Standalone (requires stack to be up)

```bash
make up
make seed
make uc3
```

### As part of full CI

```bash
make ci
```

### Via GitHub Actions

The CI workflow runs this use case as job `test-uc3`. You can view the results in the [Actions tab](../../actions/workflows/ci.yml).

## Test implementation

- **File:** `tests/integration/uc3_restart_resilience_test.go`
- **Test function:** `TestUC3_*`
- **Build tag:** `integration`

## Expected behavior

When this test passes, it confirms:

Restart each container in turn (k8s-style); re-confirm node visibility and cross-service health.

## Troubleshooting

If this test fails:

1. Check the job summary in GitHub Actions for the specific assertion that failed.
2. Download the `uc3-logs-*` artifact from the failed run.
3. Inspect the service logs in the artifact bundle.
4. Consult the [troubleshooting guide](../troubleshooting.md) for common failure modes.

## Related documentation

- [Architecture](../architecture.md) — overall stack layout
- [Use cases overview](../use-cases.md) — all use cases at a glance
- [Reference: Endpoints](../reference-endpoints.md) — service URLs and ports
- [Reference: Fixtures](../reference-fixtures.md) — seed data format
