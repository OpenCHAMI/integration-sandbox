# Clarify and wire inbound JWT enforcement for administrative routes

Repository: `OpenCHAMI/cloud-init`

Status: draft; do not publish automatically.

The integration-sandbox PR162 validation work needs independent authentication enforcement on administrative APIs.

In cloud-init v1.4.12 (92b47c69cfb2ec92a511cd984ab1f4ab37174b0e), main.go registers initCiAdminRouter(router, handler) on the main router, then attaches JWT verification/authentication to a separate /secure router with no registered routes:
https://github.com/OpenCHAMI/cloud-init/blob/92b47c69cfb2ec92a511cd984ab1f4ab37174b0e/cmd/cloud-init-server/main.go#L292-L305

The administrative routes, including /admin/groups, are registered directly on the unprotected router:
https://github.com/OpenCHAMI/cloud-init/blob/92b47c69cfb2ec92a511cd984ab1f4ab37174b0e/cmd/cloud-init-server/main.go#L345-L361

This is source-inspection evidence; I have not run an enforcement probe against this image. Setting JWKS_URL appears unable to protect these native admin routes. Please clarify whether enforcement is expected in the service or exclusively in a deployment gateway. If native enforcement is intended, wire verification/authentication to the admin router and define intended issuer, audience, scope, and skew policy.

Acceptance for the sandbox: against a protected admin route, a real issuer token succeeds while missing, wrong-signature, expired, and future-nbf tokens are independently rejected through direct HTTP. Public node-serving routes can retain their intended policy. No tokens, keys, or credentials are included in this report.
