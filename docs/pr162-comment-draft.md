Implemented sandbox support for PR162 with pinned CLI source inputs, corrected
PS256 fixtures, real API lifecycle assertions, and a dedicated validation flow.

Completed: built baseline `79051e47` and migrated `26882481` images with the same
Go 1.27.3 toolchain; both pass the real-CLI HTTP fixture contracts with race
detection, including the intentional v3/v4 duplicate-claim difference. Local
time-claim rejection is tested with cluster authentication explicitly enabled.
Static Go/shell/Compose/license checks pass. Existing PostgreSQL/SMD recovered
without replacing containers, images, or database storage.

Not completed: dedicated live UC4/UC8 repeats, real metadata/BSS/legacy-cloud-init
lifecycles, UC5/UC6 interactions, and independent enforcement profiles. Boot
v0.3.2 remains blocked by boot-service #58; legacy cloud-init's tagged admin
router also lacks inbound enforcement. SMD/metadata/PCS enforcement and external
OIDC compatibility are not established. See `docs/pr162-validation.md` for pinned
inputs, policies, commands, and validation limits. No full integration/auth pass
is claimed.
