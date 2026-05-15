+++
name = "search-files"
# `version` and the `0.0.0-dev` markers in `source` and the
# `[[requires.connectors]]` block are placeholders. CI substitutes
# them with the real version (from the pushed tag) into a build copy
# of this manifest before signing and packing. Source stays template;
# only the published tarball carries the real version.
version = "0.0.0-dev"
source = "github://ALRubinger/aileron-connector-google/actions/search-files@0.0.0-dev"

[[requires.connectors]]
name = "github://ALRubinger/aileron-connector-google"
version = "0.0.0-dev"
# `hash` is the connector tarball's content-addressed identity per
# ADR-0002. CI substitutes this placeholder with the real hash at
# release time (see .github/workflows/release.yml). The committed
# source intentionally keeps the placeholder so each release runs the
# same substitution against an unchanged template.
hash = "sha256:bound-at-release"
capabilities = ["search_files"]

[match]
intent = "search Google Drive for files by name, content, mimeType, folder, or modified-time"

[[execute]]
id = "search"
connector = "github://ALRubinger/aileron-connector-google"
op = "search_files"
idempotent = true

[[inputs]]
name = "query"
type = "string"
description = "Drive query expression. Pass through to Drive's files.list `q` parameter. Examples: \"name contains 'budget'\", \"mimeType='application/vnd.google-apps.document'\", \"modifiedTime > '2026-01-01T00:00:00Z'\", \"'<folderId>' in parents\", \"trashed=false\". Combine with \" and \" / \" or \". Omit for an unfiltered list."
required = false

[[inputs]]
name = "max_results"
type = "integer"
description = "Page size. Defaults to 25; capped at 100 to keep response payloads bounded. Use `page_token` to walk past the cap."
required = false

[[inputs]]
name = "order_by"
type = "string"
description = "Sort key, e.g. \"modifiedTime desc\", \"name\", \"createdTime desc\". Multiple keys comma-separated. Unknown keys surface as Drive's HTTP 400 rather than being pre-validated here."
required = false

[[inputs]]
name = "page_token"
type = "string"
description = "Continuation token from a prior call's `nextPageToken`. Pass to fetch the next page; omit on the first call."
required = false

[[inputs]]
name = "fields"
type = "string"
description = "Drive partial-response field mask, e.g. \"files(id,name,owners,capabilities),nextPageToken\". Defaults to a lean set: id, name, mimeType, modifiedTime, parents, webViewLink. Override when the agent needs owner info, sharing details, or capabilities."
required = false
+++

# Search Google Drive

Searches the authenticated user's Google Drive via the Drive v3
`files.list` endpoint and returns matching files with the fields you
request. The `query` argument is Drive's full query language — name
matching, content matching, mimeType filtering, folder scoping,
date ranges — passed through unchanged so the agent has the full
expressive surface.

When it fires:
- "find my budget docs"
- "what files have I touched this week"
- "list everything in the Quarterly Reports folder"
- "find spreadsheets shared with me containing 'forecast'"

Returns the raw `files.list` response: `{files: [...], nextPageToken}`.
Each `files[].id` feeds `get-file-content` for body text,
`get-doc-structure` for Doc index data, `get-file-metadata` for a
richer field set, or the editing actions (`update-doc`, `rename-file`,
`move-file`) when the agent has identified the right file.

Quirks worth knowing:
- Drive's query language is its own thing — see Google's "Search for
  files and folders" reference. Invalid queries surface as HTTP 400
  with a useful error message; the connector passes the error through
  rather than pre-validating.
- By default Drive includes trashed items. Add `trashed=false` to the
  query for "live files only".
- `pageSize` is capped at 100 by the connector even though Drive
  itself allows up to 1000 — predictable quota usage, paginate for
  larger walks.

This is a read-only operation. The connector runs in the Aileron WASM
sandbox with `[capabilities.network]` restricted to
`www.googleapis.com:443`, and the OAuth bearer token is injected
host-side at the network boundary — the connector code never sees the
token. Uses the `drive` scope. See ADR-0005 (sandbox + credential
mediation) and ADR-0006 (capability binding) in the Aileron docs.
