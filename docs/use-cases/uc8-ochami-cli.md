# Use Case 8: Ochami CLI

Run `make uc8` against a healthy seeded stack. The suite executes the
containerized CLI in separate version, SMD group, boot configuration,
boot node, and PCS subtests. CRUD checks use subsequent CLI reads and
expected missing-resource failures after CLI deletion.

Inputs cover stdin and read-only JSON/YAML files. Output checks compare
JSON, pretty JSON, and YAML, normalizing omitted empty status. Profiles are
configuration `spec.profile` labels, with no separate profile command.
Boot node CRUD uses reserved `x0c0s0b0n1`, checks for existing records, and
never issues power operations for that fixture.

PCS runs Magellan discovery, checks SMD component endpoints and readiness,
then issues CLI force-off/on transitions for `x0c0s0b0n0`. Monitor success
alone is insufficient: show must report a completed successful task, and
independent Redfish reads must observe Off and On. Cleanup restores On.

Each invocation owns a unique container, and each live flow uses a unique
resource name. Independent bounded cleanup deletes only matching names
and UIDs in scopes used by the flow. Cleanup failures fail the test.
Cancellation captures diagnostics before removing only its owned container.

HTTP fixtures run the real CLI to check configuration/token precedence,
validation without mutation, HTTP/transport errors, timeout, cancellation,
and legacy cloud-init formats. Legacy `/admin/*` routes are unavailable in
the pinned metadata service, so fixture success is not live compatibility.

CI runs default and release independently, twice against each seeded stack
without reseeding. Each Go run has 20 minutes and each job 60 minutes.
Version has 30 seconds, SMD one minute, each boot flow two minutes, and PCS
eight minutes. Artifacts contain per-attempt logs and resolved image
identities; summaries use the actual test-step outcome.

The default CLI digest contains open PR #169's cluster-precedence fix;
release v0.10.4 still fails that contract. Caller `SBX_*_IMAGE` overrides
win over manifests. Linux uses host networking; Docker Desktop CLI URLs
use `host.docker.internal`.

Run `make uc8-auth` separately with boot port 27791 free. Its owned Compose
project publishes only that existing port, has no HSM dependency, and uses
ephemeral signing keys. Tests require direct acceptance of valid tokens and
rejection of missing, incorrectly signed, and expired tokens, then check
CLI behavior. Boot v0.3.2 fails rejection because inbound auth is unwired.
The separate profile refuses an occupied port and preserves existing stacks.

See [completeness and validation limits](../uc8-completeness-review.md).
Live boot CRUD passed twice; full SMD/PCS runs await a healthy stack.
