#!/usr/bin/env bash
# Generate use-case documentation from test files.
# Scans tests/integration/uc*_test.go and creates markdown files under docs/use-cases/.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

DOCS_DIR="$ROOT/docs/use-cases"
mkdir -p "$DOCS_DIR"

# Function to get UC metadata
get_uc_title() {
  case "$1" in
    1) echo "Node Visibility" ;;
    2) echo "Multi-Cluster" ;;
    3) echo "Restart Resilience" ;;
    4) echo "Tokensmith + SMD" ;;
    5) echo "Magellan Scan" ;;
    6) echo "Power Control" ;;
    7) echo "FRU Discovery" ;;
  esac
}

get_uc_services() {
  case "$1" in
    1) echo "SMD, boot-service, metadata-service" ;;
    2) echo "boot-service, metadata-service" ;;
    3) echo "All 9 services" ;;
    4) echo "tokensmith, SMD" ;;
    5) echo "magellan, csm-rie, SMD" ;;
    6) echo "power-control, SMD, csm-rie" ;;
    7) echo "fru-tracker" ;;
  esac
}

get_uc_description() {
  case "$1" in
    1) echo "Populate SMD with nodes; node visibility chain through boot/metadata, with cloud-init lookup." ;;
    2) echo "Two clusters with disjoint node sets; move a node between them; membership reflects on both services." ;;
    3) echo "Restart each container in turn (k8s-style); re-confirm node visibility and cross-service health." ;;
    4) echo "RFC 8693 bootstrap-token mint + token exchange; JWKS signature verification; authenticated SMD write round-trips through postgres." ;;
    5) echo "Run the documented \`magellan scan → collect → send\` pipeline against the 8 BMC sims; verify SMD's \`/Inventory/RedfishEndpoints\` reflects the discovered fleet." ;;
    6) echo "POST a \`force-off\`/\`on\` transition; observe \`PowerState\` mutation on the BMC sim via an independent Redfish read; reverse and re-verify." ;;
    7) echo "POST a 32-device discovery snapshot (8 nodes × CPU + 2 DIMMs); poll until reconciler completes; verify parent/child UID linkage in the device tree." ;;
  esac
}

get_uc_slug() {
  case "$1" in
    1) echo "node-visibility" ;;
    2) echo "multi-cluster" ;;
    3) echo "restart-resilience" ;;
    4) echo "tokensmith-smd" ;;
    5) echo "magellan-scan" ;;
    6) echo "power-redfish" ;;
    7) echo "fru-discovery" ;;
  esac
}

# Generate individual use-case markdown files
for UC_NUM in {1..7}; do
  UC_SLUG=$(get_uc_slug "$UC_NUM")
  UC_TITLE=$(get_uc_title "$UC_NUM")
  UC_SERVICES=$(get_uc_services "$UC_NUM")
  UC_DESCRIPTION=$(get_uc_description "$UC_NUM")
  UC_DOC="$DOCS_DIR/uc${UC_NUM}-${UC_SLUG}.md"
  
  # Find test file
  TEST_FILE=$(find tests/integration -name "uc${UC_NUM}_*.go" 2>/dev/null | head -n1)
  
  if [ -z "$TEST_FILE" ]; then
    echo "Warning: No test file found for UC${UC_NUM}"
    continue
  fi
  
  # Extract comments from test file
  COMMENTS=$(awk '/^\/\/ Use case '"$UC_NUM"':/,/^func TestUC'"$UC_NUM"'_/ {
    if (/^\/\//) {
      sub(/^\/\/ ?/, "");
      print;
    }
  }' "$TEST_FILE" 2>/dev/null | head -n -1 || echo "")
  
  if [ -z "$COMMENTS" ]; then
    COMMENTS="No additional description available."
  fi
  
  # Generate markdown
  cat > "$UC_DOC" <<EOF
# Use Case ${UC_NUM}: ${UC_TITLE}

## Overview

**Services exercised:** ${UC_SERVICES}

**What it asserts:** ${UC_DESCRIPTION}

## Description

${COMMENTS}

## Running the test

### Standalone (requires stack to be up)

\`\`\`bash
make up
make seed
make uc${UC_NUM}
\`\`\`

### As part of full CI

\`\`\`bash
make ci
\`\`\`

### Via GitHub Actions

The CI workflow runs this use case as job \`test-uc${UC_NUM}\`. You can view the results in the [Actions tab](../../actions/workflows/ci.yml).

## Test implementation

- **File:** \`${TEST_FILE}\`
- **Test function:** \`TestUC${UC_NUM}_*\`
- **Build tag:** \`integration\`

## Expected behavior

When this test passes, it confirms:

${UC_DESCRIPTION}

## Troubleshooting

If this test fails:

1. Check the job summary in GitHub Actions for the specific assertion that failed.
2. Download the \`uc${UC_NUM}-logs-*\` artifact from the failed run.
3. Inspect the service logs in the artifact bundle.
4. Consult the [troubleshooting guide](../troubleshooting.md) for common failure modes.

## Related documentation

- [Architecture](../architecture.md) — overall stack layout
- [Use cases overview](../use-cases.md) — all use cases at a glance
- [Reference: Endpoints](../reference-endpoints.md) — service URLs and ports
- [Reference: Fixtures](../reference-fixtures.md) — seed data format
EOF

  echo "Generated: $UC_DOC"
done

# Generate index file
INDEX_FILE="$DOCS_DIR/README.md"
cat > "$INDEX_FILE" <<'EOF'
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
EOF

echo "Generated: $INDEX_FILE"
echo ""
echo "✅ Use case documentation generated successfully!"
echo "   Files created in: $DOCS_DIR"
