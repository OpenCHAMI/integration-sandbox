# Use Case 8: Ochami CLI

Run the containerized `ochami` binary against tokensmith, SMD, boot-service,
and power-control. The suite checks the CLI version, creates an SMD group,
adds a fleet member, reads membership back, removes the member and verifies
its absence, creates and lists a boot configuration, and reads power status.
Version output must contain nonempty Version, Go, and Compiler fields;
development builds with an `unknown` version remain valid.

Before reading PCS status, the suite runs the existing Magellan discovery
pipeline against the BMC simulators to populate SMD ComponentEndpoints. It
then polls PCS until the BMC has no component error and reports `on` and
`available`. The CLI response must independently satisfy those conditions.
This adds Magellan and the Redfish simulators to UC8's prerequisites. The
status check targets the BMC, whose successful manager response establishes
availability; it does not verify node power transitions.

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

Version, SMD, boot, and PCS run as separate subtests. A service failure does
not suppress the remaining service checks. Version has a 30-second command
budget, SMD and boot each have one minute, and PCS has three minutes for
discovery, cache convergence, and the CLI command. Discovery restores fleet
inventory using the same pipeline as UC6; that shared fixture data remains
available for later tests.

Isolated regression tests cover release and development version output,
PCS pending/error/malformed responses and convergence, and cleanup success,
absence, failed deletion, failed listing, malformed listing, and missing
UIDs. These tests use in-process HTTP servers; they do not establish live
CLI compatibility.

The runner uses host networking to reach the sandbox's published loopback
ports. Linux supports this directly; Docker Desktop requires host networking
to be enabled. Token minting and CLI execution disable pseudo-TTY allocation
so they work under `go test` and GitHub Actions.

Implementation: `tests/integration/uc8_ochami_cli_test.go`.
On CI failure, inspect the `uc8-logs-*` artifact and the job summary.
