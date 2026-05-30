# GitHub Actions Workflow Quick Reference

## Running the CI Workflow

### Automatic Triggers

The workflow runs automatically on:
- ✅ Every push to `main`
- ✅ Every pull request
- ✅ Daily at 06:00 UTC (drift detection)

### Manual Trigger

1. Go to [Actions tab](https://github.com/OpenCHAMI/integration-sandbox/actions/workflows/ci.yml)
2. Click "Run workflow"
3. Select branch (usually `main`)
4. Configure options:
   - **Manifest:** `release` (default), `default`, `edge`, or `release-v1.0`
   - **Skip simulators:** `true` (default) or `false`
   - **Override images:** Fill in any `SBX_*_IMAGE` field to test a specific image
5. Click "Run workflow"

## Workflow Inputs Reference

### Manifest Selection

| Input | Description | When to use |
|-------|-------------|-------------|
| `images=release` | Latest GitHub Release tags (default) | Normal CI, Renovate PRs |
| `images=default` | Floating `:latest` tags | Pre-release testing |
| `images=edge` | Bleeding-edge `:main` builds | Drift detection, experimental |
| `images=release-v1.0` | Pinned v1.0 snapshot | Regression testing |

### Simulator Control

| Input | Description | When to use |
|-------|-------------|-------------|
| `skip_sim=true` | Skip ipmi-sim and remote-console (default) | Most UCs don't need simulators |
| `skip_sim=false` | Build and start simulators | Testing UC3 (restart resilience) or future simulator-dependent tests |

### Service Image Overrides

All 12 services can be overridden individually:

| Input | Service | Example |
|-------|---------|---------|
| `sbx_vault_image` | Vault | `hashicorp/vault:1.15` |
| `sbx_localstack_image` | LocalStack | `localstack/localstack:3.0` |
| `sbx_postgres_image` | PostgreSQL | `postgres:16` |
| `sbx_sushy_image` | csm-rie (Redfish emulator) | `ghcr.io/openchami/csm-rie:v0.1.0` |
| `sbx_ipmi_sim_image` | ipmi_sim | `ghcr.io/openchami/ipmi-sim:pr-123` |
| `sbx_smd_image` | SMD | `ghcr.io/openchami/smd:pr-456` |
| `sbx_tokensmith_image` | tokensmith | `ghcr.io/openchami/tokensmith:pr-789` |
| `sbx_boot_image` | boot-service | `ghcr.io/openchami/boot-service:main` |
| `sbx_metadata_image` | metadata-service | `ghcr.io/openchami/metadata-service:v1.2.3` |
| `sbx_fru_image` | fru-tracker | `ghcr.io/openchami/fru-tracker:latest` |
| `sbx_power_image` | power-control | `ghcr.io/openchami/power-control:pr-42` |
| `sbx_magellan_image` | magellan-runner | `ghcr.io/openchami/magellan:edge` |

**Common pattern for PR testing:**
```
sbx_smd_image=ghcr.io/openchami/smd:pr-${{ github.event.pull_request.number }}
```

## Job Structure

The workflow runs **sequentially** through these jobs:

```
setup (checkout, Go, Docker, bats)
  ↓
build-images (pull/build all images)
  ↓
infra-up (Vault, LocalStack, Postgres)
  ↓
bmc-sim-up (optional, if SKIP_SIM=false)
  ↓
core-up (SMD, tokensmith, boot, metadata, fru, power, magellan)
  ↓
seed (Vault, S3, SMD fixtures)
  ↓
test-bats (CLI smoke tests)
  ↓
test-uc1 (Node visibility)
  ↓
test-uc2 (Multi-cluster)
  ↓
test-uc3 (Restart resilience)
  ↓
test-uc4 (Tokensmith + SMD)
  ↓
test-uc5 (Magellan scan)
  ↓
test-uc6 (Power control)
  ↓
test-uc7 (FRU discovery)
  ↓
teardown (always runs, cleanup)
```

## Viewing Results

### Job Summary

Each `test-uc<N>` job writes a summary with:
- ✅/❌ Pass/fail status
- Services exercised
- What the test asserts
- Link to detailed documentation

**To view:** Click on any job in the Actions UI, scroll to "Summary"

### Artifacts

Each UC uploads logs on completion (success or failure):
- `uc1-logs-<run-id>` (30-day retention)
- `uc2-logs-<run-id>`
- ...
- `uc7-logs-<run-id>`

**To download:** Click "Artifacts" at the bottom of the workflow run page

### Live Status Badges

README.md shows live status for each UC:
- [![UC1](https://github.com/OpenCHAMI/integration-sandbox/actions/workflows/ci.yml/badge.svg?event=push)](https://github.com/OpenCHAMI/integration-sandbox/actions/workflows/ci.yml)

Click any badge to jump to the workflow run.

## Local Testing with act

### Installation

```bash
# macOS
brew install act

# Linux
curl https://raw.githubusercontent.com/nektos/act/master/install.sh | sudo bash

# Windows
choco install act-cli
```

### Usage

```bash
# List all jobs without running
act --list

# Run a single use case
act -j test-uc1

# Run all jobs (full workflow)
act

# Run with custom env vars
act -j test-uc1 --env SKIP_SIM=false

# Dry-run (show what would run)
act -n
```

### act Limitations

- Artifact upload may not work (files stay local)
- Job summaries not visible (written to `$GITHUB_STEP_SUMMARY` file)
- Slower than GitHub runners (no pre-cached images)

## Troubleshooting

### "Job failed" — which UC?

Look at the job name in the Actions UI. Example:
- ✅ `test-uc1` passed
- ❌ `test-uc2` failed ← this is the one to investigate

### Download logs

1. Click the failed workflow run
2. Scroll to "Artifacts"
3. Download `uc2-logs-<run-id>.zip`
4. Extract and inspect `logs/<timestamp>/`

### Re-run a single UC

You can't re-run a single job (GitHub limitation), but you can:
1. Trigger a new manual run
2. Wait for the workflow to reach the failed UC
3. The previous UCs will pass quickly (if they're green)

Or locally:
```bash
make up
make seed
make uc2  # just the failing UC
```

### "SKIP_SIM didn't work"

Make sure:
1. You set `skip_sim=true` in the workflow dispatch UI
2. The `bmc-sim-up` job shows as "Skipped" (not "Success")
3. `scripts/build-images.sh` logs show "⚡ Skipping ipmi_sim"

## Common Workflows

### Test a PR build of SMD

1. Build your PR in the SMD repo (creates `ghcr.io/openchami/smd:pr-123`)
2. Go to [Actions tab](https://github.com/OpenCHAMI/integration-sandbox/actions/workflows/ci.yml)
3. Click "Run workflow"
4. Fill in: `sbx_smd_image=ghcr.io/openchami/smd:pr-123`
5. Click "Run workflow"

### Test edge builds (nightly drift)

The workflow automatically runs at 06:00 UTC with `IMAGES=edge`.

To manually trigger:
1. Go to [Actions tab](https://github.com/OpenCHAMI/integration-sandbox/actions/workflows/ci.yml)
2. Click "Run workflow"
3. Select `images=edge`
4. Click "Run workflow"

### Test a new use case locally

```bash
# 1. Write the test
vim tests/integration/uc8_new_feature_test.go

# 2. Start the stack
make up
make seed

# 3. Run just your test
cd tests
go test -tags integration -count=1 -v -run '^TestUC8_' ./integration/...

# 4. Generate docs
bash scripts/gen-usecase-docs.sh

# 5. Update the workflow (add test-uc8 job)
vim .github/workflows/ci.yml
```

## FAQ

**Q: Can I run UCs in parallel?**  
A: Not yet. They run sequentially to avoid state interference. Parallelization requires state isolation work.

**Q: Why is SKIP_SIM=true by default?**  
A: Most UCs don't need the simulators. Building them wastes ~5 minutes. Enable when needed.

**Q: Can I use a local image?**  
A: Not directly in GitHub Actions. You must push to a registry (GHCR, Docker Hub) first.

**Q: How do I add a new UC?**  
A: See [docs/use-cases/README.md](use-cases/README.md#adding-a-new-use-case)

**Q: Why 30-day artifact retention?**  
A: Balances storage cost with debugging needs. Increase in workflow if needed.

## Related Documentation

- [CI Refactoring Summary](CI_REFACTORING_SUMMARY.md) — what changed and why
- [Make → Actions Mapping](MAKE_TO_ACTIONS_MAPPING.md) — detailed migration guide
- [Use Cases](use-cases/README.md) — UC documentation index
- [CI Integration](ci-integration.md) — using the sandbox from service repos
