# CI Refactoring Summary

## What Changed

This refactoring moves the OpenCHAMI integration sandbox from a monolithic `make ci` flow to a GitHub Actions-driven, job-per-use-case workflow.

## Key Improvements

### 1. Explicit, Traceable Jobs

**Before:**
- Single `make ci` job that ran all tests
- Failures hidden in nested script output
- No way to see which use case failed without reading full logs

**After:**
- 15+ separate jobs (setup, build, infra, seed, test-uc1...test-uc7, teardown)
- Each job reports independently in GitHub Actions UI
- Job summaries with pass/fail badges and links to documentation

### 2. Per-Use-Case Artifacts

**Before:**
- Single log bundle only on full failure
- 7-day retention
- No per-UC granularity

**After:**
- Separate artifact for each use case (`uc1-logs-*`, `uc2-logs-*`, etc.)
- 30-day retention
- Easy to download just the failing UC's logs

### 3. Simulator Skip Flag

**Before:**
- `ipmi-sim` and `remote-console` always built
- Wasted time in CI when not testing those components

**After:**
- `SKIP_SIM=true` (default) skips building and starting simulators
- Exposed as workflow dispatch input
- Can be enabled when needed for UC3 (restart resilience) or future tests

### 4. UI-Driven Image Overrides

**Before:**
- Image overrides only via env vars in workflow file
- No way to override from UI for manual runs

**After:**
- All 12 service images exposed as workflow dispatch inputs
- `SKIP_SIM` toggle
- `IMAGES` manifest selection (release | default | edge)
- Easy to test a PR build: just fill in the `SBX_*_IMAGE` field in the UI

### 5. Auto-Generated Documentation

**Before:**
- Use case docs manually maintained in `docs/use-cases.md`
- Easy to get out of sync with test code

**After:**
- `scripts/gen-usecase-docs.sh` generates per-UC markdown files
- Extracted from test file comments
- Links to CI workflow, test file, troubleshooting guide
- Index file with live status badges

### 6. Sequential Execution

**Before:**
- Tests ran in sequence within `make test-integration`
- But not explicit in CI UI

**After:**
- Explicit job dependencies (`needs:`) guarantee sequential execution
- Prevents accidental state interference between UCs
- Easy to see which UC is currently running

## Files Changed

### New Files

- `.github/workflows/ci.yml` — new GitHub Actions workflow
- `scripts/gen-usecase-docs.sh` — documentation generator
- `docs/use-cases/` — auto-generated UC documentation (8 files)
- `docs/MAKE_TO_ACTIONS_MAPPING.md` — migration guide

### Modified Files

- `scripts/build-images.sh` — added `SKIP_SIM` support
- `README.md` — added UC badges, workflow instructions, status column
- `.github/workflows/ci.yaml` → `.github/workflows/ci-legacy.yaml.bak` — old workflow backed up

### Unchanged Files

- `Makefile` — still works, `make ci` is unchanged for local development
- All test files (`tests/integration/uc*_test.go`)
- All fixture scripts (`fixtures/*.sh`)
- All compose files (`compose/*.yaml`)

## Migration Path

### For Developers

1. **Local development:** `make ci` still works exactly as before
2. **New workflow:** Runs automatically on push/PR, or manually from Actions tab
3. **Local CI testing:** Install `act` and run `act -j test-uc1` to test a single UC

### For Service Repos

1. **No changes required** — `sandbox-consumer.example.yaml` still calls `make ci`
2. **Optional:** Update to use the new workflow inputs for per-service image overrides
3. **Future:** Can switch to calling individual jobs for faster feedback

### Rollback Plan

If issues arise:
1. Restore `.github/workflows/ci-legacy.yaml.bak` to `.github/workflows/ci.yaml`
2. Delete `.github/workflows/ci.yml`
3. The old workflow will resume working immediately

## Testing Checklist

- [ ] Run `make ci` locally to verify Makefile still works
- [ ] Install `act` and run `act --list` to verify workflow syntax
- [ ] Run `act -j test-uc1` to test a single UC locally
- [ ] Push to a branch and verify the workflow runs on GitHub
- [ ] Trigger a manual workflow dispatch and verify all inputs work
- [ ] Verify artifacts are uploaded and retained for 30 days
- [ ] Check job summaries for correct pass/fail badges
- [ ] Verify use case documentation links work

## Next Steps

1. **Test locally with act** — verify workflow syntax and job dependencies
2. **Push to a feature branch** — let GitHub Actions run the full workflow
3. **Verify artifacts** — download a few UC log bundles to confirm structure
4. **Update `docs/ci-integration.md`** — add examples of using new workflow inputs
5. **Announce to team** — document new features (SKIP_SIM, per-UC artifacts, UI overrides)
6. **Monitor nightly runs** — ensure drift detection still works with new workflow

## Known Limitations

1. **No parallel execution** — UCs run sequentially (by design, to avoid interference)
2. **Runner state not preserved** — each job pulls images fresh (could add caching later)
3. **No self-hosted runner support yet** — using standard GitHub runners (can add later)
4. **act compatibility** — some features (artifact upload, job summaries) may not work perfectly in act

## Future Improvements

- [ ] Add Docker layer caching to speed up builds
- [ ] Parallelize independent UCs (requires state isolation work)
- [ ] Add self-hosted runner support for faster builds
- [ ] Auto-commit generated docs on push to main
- [ ] Add Slack/Discord notifications for nightly drift runs
- [ ] Create a "fast feedback" workflow that runs only UC1 on every commit

## Questions?

See:
- [`docs/MAKE_TO_ACTIONS_MAPPING.md`](MAKE_TO_ACTIONS_MAPPING.md) — detailed mapping
- [`docs/use-cases/README.md`](use-cases/README.md) — UC documentation index
- [`.github/workflows/ci.yml`](../.github/workflows/ci.yml) — workflow source

Or open an issue!
