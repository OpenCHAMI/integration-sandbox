# Use Case 8: Ochami CLI

Run the containerized `ochami` binary against tokensmith, SMD, boot-service,
and power-control. The suite checks the CLI version, creates an SMD group,
adds a fleet member, reads membership back, creates and lists a boot
configuration, and reads power status.

With a healthy, seeded stack, run `make uc8`. The suite also runs in
`make test-integration`, `make ci`, and the GitHub Actions UC8 matrix entry.
The active image manifest and caller-provided image overrides select the
CLI image just as they select service images.

Each run uses a unique `uc8-<timestamp>` name for its SMD group and boot
configuration. The boot list assertion selects that name so unrelated
configurations do not affect the result. Cleanup deletes only the owned
group and boot configurations, even after an assertion failure. Cleanup
errors fail the test. The CLI configuration file lives in a temporary test
directory and is removed automatically.

The runner uses host networking to reach the sandbox's published loopback
ports. Linux supports this directly; Docker Desktop requires host networking
to be enabled. Token minting and CLI execution disable pseudo-TTY allocation
so they work under `go test` and GitHub Actions.

Implementation: `tests/integration/uc8_ochami_cli_test.go`.
On CI failure, inspect the `uc8-logs-*` artifact and the job summary.
