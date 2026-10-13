# UC8 completeness review

UC8 is a partial CLI integration smoke suite. It exercises the containerized
binary and stateful service round-trips. It does not cover the complete
Ochami command surface or prove authentication enforcement.

## Current coverage

| Area | Live integration assertions | Isolated regression assertions |
| --- | --- | --- |
| Version | Nonempty Version, Go, and Compiler output; release and development values accepted | Six cases: valid release/development, empty/unstructured output, missing compiler, empty version |
| SMD | Create a unique group, add a member, GET membership, delete the member without confirmation, GET an empty member list | None; these operations require live SMD |
| Boot | Create a uniquely named configuration using JSON on stdin, list it, verify UID and payload, ignore unrelated configurations | Ownership filtering is exercised by cleanup tests |
| PCS | Run Magellan discovery, wait for a healthy BMC entry, validate CLI xname/error/power/management fields | Seven readiness cases: convergence, component error, empty list, wrong component, stale state, malformed JSON, HTTP failure |
| Cleanup | Independently delete owned boot configuration UIDs and SMD group after the service flows | Seven cases: HTTP 200/204 success, 404 absence, deletion failure, listing failure, malformed listing, missing UID; unrelated resources preserved |

Four independent integration subtests have separate command budgets. A
failed SMD flow no longer prevents boot or PCS coverage. Version execution
does not depend on token minting. The standalone Go timeout accommodates
all service budgets and cleanup.

PCS checks BMC availability after discovery. A successful BMC manager
response produces the expected `on` state; this does not demonstrate node
power transitions. UC6 tests those transitions through the service API.

## Remaining gaps

1. CLI failure behavior: malformed input, missing resources, invalid flags,
   inaccessible endpoints, and expected nonzero exits and stderr are untested.
2. Additional mutations: group updates/deletion, direct member GET, boot
   GET/update/delete, and PCS transition commands are untested. Current
   cleanup uses HTTP APIs rather than CLI delete commands.
3. Other command families: cloud-init, boot nodes/profiles, and the rest of
   the SMD and PCS command surfaces are outside UC8.
4. Authentication: tokens are minted and passed to the CLI, but the harness
   runs boot authentication disabled and uses development token settings.
   Invalid/expired tokens, refresh/exchange, scopes, and audience enforcement
   require a separate enforcing configuration.
5. Configuration and presentation: alternate clusters, endpoint precedence,
   file input, other output formats, and CLI timeout behavior are untested.
6. Cleanup transport failures and cancellation are not simulated. An abruptly
   killed test process cannot run cleanup; unique names avoid collisions but
   leave records for later diagnosis.

## Validation and limits

The 20 isolated regression cases passed with the race detector. Integration
test compilation, go vet, and golangci-lint passed. These results establish
helper behavior and static correctness, not compatibility with running CLI
images and services.

A full live UC8 run and consecutive-run verification remain outstanding:
the existing stack has stopped Postgres and unhealthy SMD. Diagnostics were
captured under `logs/20261012T205622Z/uc8-validation/`. No containers were
changed to bypass the failure protocol.

Once a healthy seeded stack is available, run `make uc8` twice against the
same stack, then run the complete integration suite to check ordering with
UC5 and UC6 discovery. Validate both the default PR image and release CLI
manifest before treating UC8 as verified across supported image sets.
