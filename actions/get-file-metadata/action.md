+++
name = "get-file-metadata"
# `version` and the `0.0.0-dev` markers in `source` and the
# `[[requires.connectors]]` block are placeholders. CI substitutes
# them with the real version (from the pushed tag) into a build copy
# of this manifest before signing and packing. Source stays template;
# only the published tarball carries the real version.
version = "0.0.0-dev"
source = "github://ALRubinger/aileron-connector-google/actions/get-file-metadata@0.0.0-dev"

[[requires.connectors]]
name = "github://ALRubinger/aileron-connector-google"
version = "0.0.0-dev"
# `hash` is the connector tarball's content-addressed identity per
# ADR-0002. CI substitutes this placeholder with the real hash at
# release time (see .github/workflows/release.yml). The committed
# source intentionally keeps the placeholder so each release runs the
# same substitution against an unchanged template.
hash = "sha256:bound-at-release"
capabilities = ["get_file_metadata"]

[match]
intent = "look up a Google Drive file's metadata (name, mimeType, parents, modifiedTime, owners)"

[[execute]]
id = "fetch"
connector = "github://ALRubinger/aileron-connector-google"
op = "get_file_metadata"
idempotent = true

[[inputs]]
name = "file_id"
type = "string"
description = "Drive file id, as returned by `search-files` in `files[].id`."
required = true

[[inputs]]
name = "fields"
type = "string"
description = "Drive partial-response field mask. Default includes id, name, mimeType, parents, modifiedTime, owners, webViewLink — enough for an authoritative approval card and most \"what is this file?\" reads. Override to fetch capabilities, shared-drive info, etc."
required = false
+++

# Look Up a Google Drive File's Metadata

Fetches a single Drive file's properties without downloading its
content. Returns the raw `files.get` response, defaulting to a field
set that covers the common cases: id, name, mimeType, parents (folder
ids), modifiedTime, owners, webViewLink.

When it fires:
- "what's the name of file 1abc..."
- "where is this file — what folder does it live in"
- "who owns this Doc"
- "when was the budget spreadsheet last modified"

Pair with:
- `search-files` to find a file id from a name/query, then this for
  the wider field set,
- `get-file-content` when the agent needs the file's text,
- `get-doc-structure` when the agent needs Docs API index space.

This action also powers the **authoritative approval preview** on
`move-file`. When `move-file` is invoked, the runtime calls this op
against the file id (read-only, idempotent) and renders the
authoritative file name and current parent folder ids on the approval
card — so the user sees what they are actually agreeing to move,
rather than opaque ids the agent supplied. See ADR-0016 (approval-
time preview fetch) for the directive's contract.

This is a read-only operation. The connector runs in the Aileron WASM
sandbox with `[capabilities.network]` restricted to
`www.googleapis.com:443`, and the OAuth bearer token is injected
host-side at the network boundary — the connector code never sees the
token. Uses the `drive` scope. See ADR-0005 (sandbox + credential
mediation) in the Aileron docs.
