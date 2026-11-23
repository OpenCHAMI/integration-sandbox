# Declarative boot fixture file compatibility

The native broken-reference control operates on an isolated copy of the boot
store. It reads series state from `revisions/bootconfig/series/` using the
candidate runtime's canonical `sha256.<lowercase SHA-256 of the exact logical
name>.json` filename. This canonical format is from the independently reviewed
Fabrica source change after the v0.4.15 baseline; it is not assumed to be present
in the baseline runtime.

Only when the canonical filename is absent does the control read the baseline
`<raw URL-base64 of the exact logical name>.json` filename. Canonical malformed,
wrong-name, nonregular or symlinked state fails closed without falling back to
legacy state. Legacy state must identify the requested logical name, preventing
a case-insensitive filename alias from selecting another series. Old legacy
state without a UID list can derive its authoritative UIDs from its pointers and
name bindings. The retired-reference factory intentionally still writes a
legacy-named test series to exercise backward reads.

Revision UID filenames and series/record JSON fields remain unchanged. The
control deletes only the requested revision record from the copied store; it
does not change the source series, assignment or public API assertions. The
existing snapshot inventory continues to include all files in the covered
namespaces regardless of either naming scheme. Use a matching runtime and a
complete directory for backups; this does not establish mixed-version or
downgrade safety.

## Source provenance

The independent review was against Fabrica v0.4.15 base
`61807c05fca762fd224bb34bf56e4c6c5bba30cd` plus an uncommitted reviewed source
delta, not an immutable published release. At review time:

- `pkg/revision/file_store_series.go` SHA-256:
  `9de4b45f96d4dc48985268a51ee5d449b9ced99496371261fb717959c2831692`
- `pkg/revision/file_store_canonical.go` SHA-256:
  `54b4706ada5dcf38ae0fc24535e159ee5f677f27a338ca37996ba101119e6e5a`
- Reviewed full tracked and untracked source diff SHA-256:
  `199ba758b184eaaa5c9c3887dc1ac5dcaba4092e0b38fa78d4879177ee5cf329`
- `task24-independent-review-r2.out` SHA-256:
  `44f4bfa5e796ea8bed1fe7f7a2efdd2c1a016c8acd0b29ebcc8dbb05e28abcea`

These hashes identify the inspected inputs, not a published dependency or a
live candidate qualification. The sandbox tests do not import Fabrica's
revision package to compute expected behavior.
