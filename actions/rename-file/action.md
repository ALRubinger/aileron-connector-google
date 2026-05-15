+++
name = "rename-file"
# `version` and the `0.0.0-dev` markers in `source` and the
# `[[requires.connectors]]` block are placeholders. CI substitutes
# them with the real version (from the pushed tag) into a build copy
# of this manifest before signing and packing. Source stays template;
# only the published tarball carries the real version.
version = "0.0.0-dev"
source = "github://ALRubinger/aileron-connector-google/actions/rename-file@0.0.0-dev"

[[requires.connectors]]
name = "github://ALRubinger/aileron-connector-google"
version = "0.0.0-dev"
# `hash` is the connector tarball's content-addressed identity per
# ADR-0002. CI substitutes this placeholder with the real hash at
# release time (see .github/workflows/release.yml). The committed
# source intentionally keeps the placeholder so each release runs the
# same substitution against an unchanged template.
hash = "sha256:bound-at-release"
capabilities = ["rename_file"]

[match]
intent = "rename a Google Drive file"

[[execute]]
id = "rename"
connector = "github://ALRubinger/aileron-connector-google"
op = "rename_file"
idempotent = true

# No `[approval]` block. Renames are reversible (rename back) and
# the file's id, owners, parents, and content are unchanged — only
# the display name shifts. Same reasoning as create-doc / upload-file
# / update-doc; see issue #6 for the per-action gating rationale.

[[inputs]]
name = "file_id"
type = "string"
description = "Drive file id (or Docs document id — they're the same id space)."
required = true

[[inputs]]
name = "new_name"
type = "string"
description = "The new display name."
required = true
+++

# Rename a Google Drive File

Renames a Drive file (or Google Doc — Docs use the same id space as
Drive). Only the display name changes; the file's id, owners,
parents, content, and sharing remain untouched.

When it fires:
- "rename 'Untitled Document' to 'Q2 Plan'"
- "call this doc 'Final' instead of 'Draft v3'"
- "fix the typo in this file's name"

The op uses Drive's `files.update` (HTTP PATCH) with `{"name": "..."}`
as the body. Returns the updated file's metadata.

**Idempotent in effect.** Renaming a file to a name it already has
is a no-op against Drive. The action manifest sets
`idempotent = true` so the gateway's retry layer (ADR-0010) may
safely re-issue on transient failure.

No approval gate: renames are reversible by reissuing the rename
with the previous name. Drive does not retain a name history the
way Docs retains revision history, so for high-stakes renames the
caller may want to capture the prior name via `get-file-metadata`
first — but that is a pattern choice, not a safety requirement
addressed by the manifest.

The connector runs in the Aileron WASM sandbox with
`[capabilities.network]` restricted to `www.googleapis.com:443`, and
the OAuth bearer token is injected host-side at the network boundary
— the connector code never sees the token. Uses the `drive` scope.
See ADR-0005 (sandbox + credential mediation) in the Aileron docs.
