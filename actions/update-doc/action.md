+++
name = "update-doc"
# `version` and the `0.0.0-dev` markers in `source` and the
# `[[requires.connectors]]` block are placeholders. CI substitutes
# them with the real version (from the pushed tag) into a build copy
# of this manifest before signing and packing. Source stays template;
# only the published tarball carries the real version.
version = "0.0.0-dev"
source = "github://ALRubinger/aileron-connector-google/actions/update-doc@0.0.0-dev"

[[requires.connectors]]
name = "github://ALRubinger/aileron-connector-google"
version = "0.0.0-dev"
# `hash` is the connector tarball's content-addressed identity per
# ADR-0002. CI substitutes this placeholder with the real hash at
# release time (see .github/workflows/release.yml). The committed
# source intentionally keeps the placeholder so each release runs the
# same substitution against an unchanged template.
hash = "sha256:bound-at-release"
capabilities = ["update_doc"]

[match]
intent = "apply structured edits to a Google Doc — insert text, replace text, delete a range, change styles, insert tables or images"

[[execute]]
id = "update"
connector = "github://ALRubinger/aileron-connector-google"
op = "update_doc"
idempotent = false

# No `[approval]` block. Doc edits are reversible via Docs revision
# history — the owner can roll back to any prior version from
# File → Version history → See version history. The runtime-level
# approval prompt would penalize the safer-by-default editing path
# (where the safety net is the API providing first-class
# reversibility) with friction that doesn't add safety. See issue #6
# for the per-action gating rationale across the connector's write
# actions; ADR-0009 establishes that approval is reserved for
# *irreversible* writes.

[[inputs]]
name = "document_id"
type = "string"
description = "Google Docs document id."
required = true

[[inputs]]
name = "requests"
type = "array"
description = "Array of Docs API Request objects to apply in order. Common request types: {\"insertText\": {\"text\": \"...\", \"location\": {\"index\": N}}}, {\"replaceAllText\": {\"containsText\": {\"text\": \"OLD\", \"matchCase\": true}, \"replaceText\": \"NEW\"}}, {\"deleteContentRange\": {\"range\": {\"startIndex\": A, \"endIndex\": B}}}, {\"updateTextStyle\": {...}}, {\"updateParagraphStyle\": {...}}, {\"insertTable\": {...}}. See the Docs API reference for the full Request union. Pair with `get-doc-structure` to obtain accurate indices before constructing range-based requests."
required = true
+++

# Update a Google Doc

Applies a batch of structured edits to a Google Doc via the Docs API
`documents.batchUpdate` endpoint. The `requests` array is the Docs
API's own Request object union — `insertText`, `replaceAllText`,
`deleteContentRange`, `updateTextStyle`, `updateParagraphStyle`,
`insertTable`, `insertInlineImage`, `createNamedRange`, and the rest.
The agent constructs them; the connector passes them through.

When it fires:
- "insert this paragraph at the start of the doc"
- "replace 'TBD' with the final number throughout the doc"
- "delete the section between indices 120 and 340"
- "make the first heading bold"
- "insert a 3x4 table after the introduction"

Workflow:
1. `get-doc-structure` to fetch the document's index space (where
   paragraphs, runs, and headings live).
2. Construct the appropriate Request objects with the indices you
   want to target.
3. `update-doc` to apply them as a single batch.

Order-sensitivity: the Docs API processes requests in array order
and computes index shifts between them. If you issue
`insertText(index=100, text="hello")` followed by
`deleteContentRange(50, 60)`, the delete operates on the post-insert
index space, not the original. The conventional safe pattern is to
construct requests in descending end-index order so earlier requests
do not shift the indices later requests target.

**No approval gate.** Docs revision history makes every edit
reversible — the owner can roll back from File → Version history.
ADR-0009 reserves approval for *irreversible* writes (sending mail)
and *third-party-observable* writes (move-file's permission
inheritance change); structured doc edits are neither.

This action writes to your Doc. It is **not idempotent** — re-running
the same `insertText` inserts the text twice. The runtime's retry
layer is configured to honor that and will not double-write on
transient failure. `replaceAllText` is the closest thing to an
idempotent request shape but the connector declares the action
non-idempotent because the broader request union is not.

The connector runs in the Aileron WASM sandbox with
`[capabilities.network]` restricted to `docs.googleapis.com:443`, and
the OAuth bearer token is injected host-side at the network boundary
— the connector code never sees the token. Uses the `documents` scope.
See ADR-0005 (sandbox + credential mediation) in the Aileron docs.
