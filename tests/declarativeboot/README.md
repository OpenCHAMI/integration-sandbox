# Declarative boot client helpers

These tagged helpers are independent of boot-service and Fabrica. They do not
start a server or contact the network at package initialization. The API is
ready for lifecycle and safety workers to exercise against a candidate, but
has only been verified against local wire fixtures, not live services.

Wire provenance: Fabrica v0.4.15 `pkg/revision/models.go`,
`pkg/revision/file_store.go` (`Record`),
`pkg/codegen/templates/server/handlers.go.tmpl` and
`docs/guides/resource-revisions.md`; SMD `pkg/sm/groups.go` and
`cmd/smd/main.go` group route. The task-1 contract sets `/bootconfigs` as
the concrete resource path rather than the guide's illustrative path.

## Available API

- `NewHTTPClient(base string, timeout time.Duration) (*HTTPClient, error)`;
  `(*HTTPClient).Do(ctx context.Context, input Request) (Response, error)`.
  `Request` contains method, absolute-path-plus-query, headers and raw body.
  Redirects are not followed; response bodies are bounded and closed. The
  caller must keep raw response content out of logs when it may contain secrets.
- `(Response).RequireStatus(expected ...int) error` returns a `*StatusError`
  without including the body. `DecodeJSON[T](Response) (T, error)` and
  `EncodeJSON[T](T) ([]byte, error)` encode local typed values. A successful
  decode alone does not prove that required server response fields exist.
- `(BootClient).Raw(ctx context.Context, request Request) (Response, error)`
  sends arbitrary public routes and conditional headers, including negative
  requests. `CreateConfig(ctx, NamedConfig)`, `EnsureRevision(ctx, series,
  NamedConfig)`, and `ApplyAssignment(ctx, ConditionalAssignment)` return
  `(Response, error)`; they do not hide unexpected statuses. Copy the exact
  strong `ETag` from `Response.Header` and opaque IDs from server responses;
  never construct either. Both create and ensure send the verified top-level
  `metadata`/`spec` request envelope, with optional labels and annotations.
- Revision reads: `GetSeries(ctx, series)`, `ListRevisions(ctx, series)`,
  `GetRevision(ctx, RevisionID)`, `ResolveRevision(ctx, ConfigReference)`,
  `PromoteDefault(ctx, Promotion)` return `(Response, error)`. Check the
  status and ETag explicitly, then decode with `DecodeRevision`,
  `DecodeRevisions`, `DecodeSeries` or `DecodeResolution`. Decoders reject
  missing required metadata and malformed JSON. The revision record embeds
  both metadata and resource; the resolve response contains metadata only.
  Promotion sends `revisionUid` and the exact supplied `If-Match`.
- Assignment reads and deletes: `GetAssignment(ctx, name)`,
  `GetAssignmentConditional(ctx, ConditionalName)`, and
  `DeleteAssignment(ctx, ConditionalName)` return raw `(Response, error)`;
  `ApplyAssignment` supplies conditional PUT. `DecodeAssignment` checks name,
  UID and config reference. For UID routes, status/alias cases and malformed
  negative requests use `Raw`; no helper silently changes preconditions.
- `CreateBootstrapToken(ctx context.Context, run CommandRunner, options
  BootstrapOptions) (string, error)` uses the UC4 docker-exec command; pass
  `DockerExec` in a real run or a narrow injected function in tests.
  `ExchangeToken(ctx context.Context, client *HTTPClient, bootstrap string)
  (string, error)` performs the RFC 8693 exchange. `VerifyToken(ctx
  context.Context, client *HTTPClient, raw string, policy TokenPolicy) error`
  verifies RSA JWKS signatures and issuer, audience, expiry and requested
  scopes. Neither error contains the raw token or upstream response body.
- `(SMDClient).PutComponents(ctx context.Context, value Components) error` and
  `(SMDClient).CreateInterface(ctx context.Context, value EthernetInterface)
  error` use the UC1 wire payloads. Interface conflicts are errors, not
  implicit proof that the stored interface matches. `CreateGroup(ctx, Group)`,
  `UpdateGroup(ctx, Group)` and `GetGroup(ctx, label)` return `(Response,
  error)`; `DecodeGroup(Response) (Group, error)` validates its required fields.
  `EnsureGroup(ctx, Group) error` reads on create conflict and succeeds only
  if the stored group matches the requested group: labels and member IDs are
  case-sensitive, while member order is ignored. A mismatch returns
  `ErrGroupConflict`; it never overwrites the existing group. Group mutation
  status codes must be qualified against the actual SMD deployment in task 5.
- `LoadFixtures(dir string) (Fixtures, error)` loads configs, nodes,
  assignments, reference binding instructions, negative cases, legacy data,
  all 36 manifest entries and independent expected artifacts. It rejects
  duplicate/missing scenario IDs and an unsafe manifest policy. Bindings are
  instructions, not wire fields: insert captured runtime UIDs into the
  indicated typed reference yourself. Expected artifact values are read from
  `expected.json`, never server responses.

Only HTTP wire fakes were run. This is not evidence of a real TokenSmith, SMD,
revision API, Docker stack, or the 36 acceptance scenarios.
