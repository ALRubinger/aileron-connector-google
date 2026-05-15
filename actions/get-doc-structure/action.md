+++
name = "get-doc-structure"
# `version` and the `0.0.0-dev` markers in `source` and the
# `[[requires.connectors]]` block are placeholders. CI substitutes
# them with the real version (from the pushed tag) into a build copy
# of this manifest before signing and packing. Source stays template;
# only the published tarball carries the real version.
version = "0.0.0-dev"
source = "github://ALRubinger/aileron-connector-google/actions/get-doc-structure@0.0.0-dev"

[[requires.connectors]]
name = "github://ALRubinger/aileron-connector-google"
version = "0.0.0-dev"
# `hash` is the connector tarball's content-addressed identity per
# ADR-0002. CI substitutes this placeholder with the real hash at
# release time (see .github/workflows/release.yml). The committed
# source intentionally keeps the placeholder so each release runs the
# same substitution against an unchanged template.
hash = "sha256:bound-at-release"
capabilities = ["get_doc_structure"]

[match]
intent = "fetch the structured JSON of a Google Doc so precise edits can target indices, paragraphs, headings, and styles"

[[execute]]
id = "fetch"
connector = "github://ALRubinger/aileron-connector-google"
op = "get_doc_structure"
idempotent = true

[[inputs]]
name = "document_id"
type = "string"
description = "Google Docs document id (the long alphanumeric in the doc URL, not the human title)."
required = true

[[inputs]]
name = "suggestions_view_mode"
type = "string"
description = "Optional. One of DEFAULT_FOR_CURRENT_ACCESS, SUGGESTIONS_INLINE, PREVIEW_SUGGESTIONS_ACCEPTED, PREVIEW_WITHOUT_SUGGESTIONS. SUGGESTIONS_INLINE is the right pick when an update-doc batchUpdate will run against a document carrying user-made suggestions — it guarantees the index space the caller sees matches the index space batchUpdate operates on."
required = false
+++

# Read a Google Doc's Structure

Returns the full structured representation of a Google Doc as the
Docs API exposes it: body content (paragraphs, tables, section breaks,
table of contents), headers and footers, lists, ranges, named styles,
and the index positions of every structural element. Pair with
`update-doc` — the indices returned here are the indices `update-doc`
operations target.

When it fires:
- "show me the headings in this doc"
- "what are the paragraph indices so I can edit at line 5"
- "is there a table of contents I need to update"

Why this is separate from `get-file-content`:
- `get-file-content` returns exported plain text — useful for an
  agent reading content for context, but it discards index space.
- `get-doc-structure` returns the Docs API's structured JSON —
  paragraphs and runs with `startIndex` / `endIndex` fields the agent
  can target via `update-doc`'s `insertText`, `replaceAllText`,
  `deleteContentRange`, `updateTextStyle`, `updateParagraphStyle`,
  etc.

Returns the raw `documents.get` response. The agent typically walks
`body.content[]` (a sequence of `StructuralElement` objects) and
extracts the indices it cares about.

When working over a doc that already has user-made suggestions
(someone has been suggesting edits in the Docs UI), pass
`suggestions_view_mode = "SUGGESTIONS_INLINE"` so the indices returned
include suggestion ranges. Otherwise the default index space may
differ from what `update-doc`'s batchUpdate sees, and an
insertText-at-index-N can land in the wrong place.

This is a read-only operation. The connector runs in the Aileron WASM
sandbox with `[capabilities.network]` restricted to
`docs.googleapis.com:443`, and the OAuth bearer token is injected
host-side at the network boundary — the connector code never sees the
token. Uses the `documents` scope. See ADR-0005 (sandbox + credential
mediation) in the Aileron docs.
