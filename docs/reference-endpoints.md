# Endpoint reference

Every URL the harness publishes, what it returns, and which test exercises it.

## Infra

| URL | Returns | Tested in |
|---|---|---|
| `http://127.0.0.1:8200/v1/sys/health` | `200 {sealed:false, initialized:true}` | `infra_test.go::TestVault_DevReady`, `cli-smoke.bats::vault status` |
| `http://127.0.0.1:4566/_localstack/health` | `200 {services:{s3:running, …}}` | `infra_test.go::TestLocalstack_S3Healthy` |
| `postgres://openchami:openchami@127.0.0.1:5432/openchami` (TCP only) | n/a | implicit — SMD comes up |

## SMD (`http://127.0.0.1:27779`)

| URL | Returns |
|---|---|
| `GET /hsm/v2/service/ready` | `200 {code:0, message:"HSM is healthy"}` |
| `GET /hsm/v2/State/Components?type=Node` | list of all node components |
| `GET /hsm/v2/State/Components/{xname}` | one component, 404 if absent |
| `POST /hsm/v2/State/Components` | `204` — bulk register components |
| `DELETE /hsm/v2/State/Components/{xname}` | `200`/`204`/`404` |
| `GET /hsm/v2/Inventory/RedfishEndpoints` | list of registered BMCs |
| `POST /hsm/v2/Inventory/RedfishEndpoints` | `201` with per-entry URI list |
| `POST /hsm/v2/Inventory/EthernetInterfaces` | `201` per interface (used so X-Forwarded-For lookups can resolve to xname) |

Tested in `smd_test.go`, all UC files.

## Tokensmith (`http://127.0.0.1:27780`)

| URL | Returns |
|---|---|
| `GET /.well-known/jwks.json` | `200` JWKS document — used as healthcheck |

Tested in `services_test.go::tokensmith-jwks`.

## Boot-service (`http://127.0.0.1:27791`)

| URL | Returns |
|---|---|
| `GET /health` | `200`/`204` |
| `GET /openapi.json` | full OpenAPI spec |
| `GET /nodes` | array of Node objects |
| `GET /nodes/{uid}` | one Node (404 if absent). **uid, not name.** |
| `POST /nodes` | `201` with full Node object |
| `PUT /nodes/{uid}` | `200` updated Node |
| `DELETE /nodes/{uid}` | `200`/`204` |
| `GET /bmcs` | list of BMC entries |
| `POST /bmcs` | create BMC |
| `GET /bootconfigurations` | list of boot configs |
| `POST /bootconfigurations` | create boot config |

Schema reference: `Node.spec` requires `xname` (validated against the OpenCHAMI XName regex). Optional fields: `bootMac`, `nid`, `role`, `subRole`, `hostname`, `groups[]`, `interfaces[]`.

Tested in `services_test.go::boot-health`, all UC files.

## Metadata-service (`http://127.0.0.1:27792`)

| URL | Returns |
|---|---|
| `GET /health` | `200`/`204` |
| `GET /openapi.json` | full OpenAPI spec |
| `GET /groups` | list of groups |
| `GET /groups/{uid}` | one group. **uid, not name.** |
| `POST /groups` | `201` with full Group object |
| `GET /clusterdefaultss` | list of cluster defaults (yes, the route is plural-pluralized) |
| `GET /instanceinfos` | list of instance info entries |
| `GET /profiles` | list of profiles |
| `GET /wireguardpeers` | list of WireGuard peers |
| `GET /meta-data` | cloud-init metadata for the node behind the caller IP |
| `GET /user-data` | cloud-init user-data — same lookup |
| `GET /vendor-data` | cloud-init vendor-data |
| `GET /network-config` | cloud-init network-config |
| `GET /{group}.yaml` | cloud-init group YAML |

Group `spec.template` is rendered via Jinja-style `{{ var }}`. Every variable referenced in the template must be present in `spec.metaData` or POST returns `400 missing required variables`.

The cloud-init endpoints look up the caller's xname via SMD `EthernetInterfaces`. Tests that hit them must seed the IP first (see `uc1_node_visibility_test.go::smd_register_ethernet_interfaces`).

Tested in `services_test.go::metadata-health`, `uc1_node_visibility_test.go`.

## FRU-tracker (`http://127.0.0.1:27793`)

| URL | Returns |
|---|---|
| `GET /health` | `200`/`204` |
| `POST /discoverysnapshots` | `201` — ingest a discovery snapshot |
| `GET /devices` | list of devices |
| `GET /discoverysnapshots` | list of snapshots |

Tested in `services_test.go::fru-health`. Deeper assertions are open work — see [known-issues.md](known-issues.md).

## Power-control (`http://127.0.0.1:28007`)

| URL | Returns |
|---|---|
| `GET /health` | `200`/`204` |

Tested in `services_test.go::power-health`. Power-transition assertions are open work.

## Redfish BMC sim (`https://127.0.0.1:5000`, self-signed)

| URL | Returns |
|---|---|
| `GET /redfish/v1` | `200` Redfish service root (no auth required) |
| `GET /redfish/v1/Systems` | `200` (with basic auth `root`/`root_password`) |
| `GET /redfish/v1/Systems/{id}` | one System |
| `GET /redfish/v1/Managers/BMC` | the BMC's own management info |
| `POST /redfish/v1/Systems/{id}/Actions/ComputerSystem.Reset` | with auth — power transition |

Only `redfish-bmc-0` is published to the host. Inside the docker network, `https://x0c0s{0..7}b0/redfish/v1` resolves to the per-node container.

Tested in `bmcsim_test.go`.

## IPMI sim

UDP/623, `root`/`root_password`. Reachable inside the docker network as `x0c0s0b0-ipmi`. Not published to the host. Drives `remote-console`'s SOL flow when that surface is added.

## What the host doesn't see

These URLs are docker-network-only:

- `http://smd:27779`, `http://tokensmith:27780`, etc. — service-to-service.
- `https://x0c0s{1..7}b0:5000/…` — Redfish BMCs 1 through 7.
- `udp://x0c0s0b0-ipmi:623` — IPMI sim.
- `postgres://postgres:5432/…` — service-to-postgres.
- `http://vault:8200`, `http://localstack:4566` — service views of the same instances the host can reach via 127.0.0.1.
