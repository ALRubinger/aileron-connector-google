+++
name = "create-doc"
# `version` and the `0.0.0-dev` markers in `source` and the
# `[[requires.connectors]]` block are placeholders. CI substitutes
# them with the real version (from the pushed tag) into a build copy
# of this manifest before signing and packing. Source stays template;
# only the published tarball carries the real version.
version = "0.0.0-dev"
source = "github://ALRubinger/aileron-connector-google/actions/create-doc@0.0.0-dev"

[[requires.connectors]]
name = "github://ALRubinger/aileron-connector-google"
version = "0.0.0-dev"
# `hash` is the connector tarball's content-addressed identity per
# ADR-0002. CI substitutes this placeholder with the real hash at
# release time (see .github/workflows/release.yml). The committed
# source intentionally keeps the placeholder so each release runs the
# same substitution against an unchanged template.
hash = "sha256:bound-at-release"
capabilities = ["create_doc"]

[match]
intent = "create a new Google Doc, optionally with initial body content"

[[execute]]
id = "create"
connector = "github://ALRubinger/aileron-connector-google"
op = "create_doc"
idempotent = false

# No `[approval]` block. Doc creation is reversible — the user can
# trash the doc from Drive (or revert via Docs revision history),
# and the document is private to the creator by default. Approval
# gating is reserved for irreversible writes (send-email, send-draft)
# and third-party-observable writes (move-file). See issue #6 for the
# per-action gating rationale across the connector's write actions.

[[inputs]]
name = "title"
type = "string"
description = "Document title. Shown in the Drive picker and Docs window header."
required = true

[[inputs]]
name = "body"
type = "string"
description = "Optional initial body content (plain text). The connector creates the doc and then issues a batchUpdate `insertText` at index 1 to populate it. Omit for an empty doc the agent will fill in with subsequent `update-doc` calls."
required = false
+++

# Create a Google Doc

Creates a new Google Doc and (optionally) writes initial body
content. The new doc lands in the user's My Drive root, owned by the
user, private by default.

When it fires:
- "draft a design doc titled 'Sharded sessions'"
- "make a new doc with these meeting notes"
- "create an empty doc called 'Sprint planning'"

Two API hits when `body` is supplied (Docs `documents.create` followed
by `documents.batchUpdate` with `insertText`), one when it is omitted.
The response on the body-insert path is a refetched
`documents.get` so the returned `body.content` reflects the inserted
text — the agent can compose follow-up `update-doc` requests against
known indices without an extra `get-doc-structure` call.

Pair with:
- `update-doc` for structured follow-up edits (insertText at chosen
  ranges, replaceAllText, paragraph styles, etc.),
- `move-file` to relocate the new doc into a specific folder once the
  user approves the move.

This action writes to your Drive (creates a doc). It is **not
idempotent** — invoking it twice creates two docs. The runtime's
retry layer is configured to honor that and will not double-write on
transient failure. No approval gate: doc creation is reversible
(trash from Drive) and private by default; the agent is not creating
anything other people can see.

The connector runs in the Aileron WASM sandbox with
`[capabilities.network]` restricted to `docs.googleapis.com:443`, and
the OAuth bearer token is injected host-side at the network boundary
— the connector code never sees the token. Uses the `documents` scope.
See ADR-0005 (sandbox + credential mediation) in the Aileron docs.
