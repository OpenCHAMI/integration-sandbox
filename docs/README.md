# integration-sandbox docs

Read these in order if you've never touched the harness:
1. [Quickstart](quickstart.md) — five-minute walkthrough.
2. [Architecture](architecture.md) — what's in the box, how the layers fit.
3. [Use cases](use-cases.md) — what each `make uc<N>` actually validates.
4. [Configuration](configuration.md) — env vars, image manifests, fixtures, port map.
5. [Operations](operations.md) — make targets, scripts, log bundles, heartbeat.
6. [Troubleshooting](troubleshooting.md) — every failure mode we've hit, with fix.
7. [Extending](extending.md) — adding a service, a fixture, a test.
8. [CI integration](ci-integration.md) — GitHub Actions, PR-build overrides.
9. [Known issues](known-issues.md) — harness limitations and open work.
10. [Relationship to the operator](relationship-to-operator.md) — what each test suite does and doesn't cover.

Reference cards (skim, don't read):
- [Endpoints](reference-endpoints.md) — every URL the harness exposes.
- [Helpers](reference-helpers.md) — every test helper in `clientutil_test.go`.
- [Fixtures](reference-fixtures.md) — every JSON/SQL fixture and what it does.
