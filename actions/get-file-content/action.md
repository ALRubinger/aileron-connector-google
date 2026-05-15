+++
name = "get-file-content"
# `version` and the `0.0.0-dev` markers in `source` and the
# `[[requires.connectors]]` block are placeholders. CI substitutes
# them with the real version (from the pushed tag) into a build copy
# of this manifest before signing and packing. Source stays template;
# only the published tarball carries the real version.
version = "0.0.0-dev"
source = "github://ALRubinger/aileron-connector-google/actions/get-file-content@0.0.0-dev"

[[requires.connectors]]
name = "github://ALRubinger/aileron-connector-google"
version = "0.0.0-dev"
# `hash` is the connector tarball's content-addressed identity per
# ADR-0002. CI substitutes this placeholder with the real hash at
# release time (see .github/workflows/release.yml). The committed
# source intentionally keeps the placeholder so each release runs the
# same substitution against an unchanged template.
hash = "sha256:bound-at-release"
capabilities = ["get_file_content"]

[match]
intent = "read the text content of a Google Drive file (Doc, Sheet, Slide, or text/* file)"

[[execute]]
id = "fetch"
connector = "github://ALRubinger/aileron-connector-google"
op = "get_file_content"
idempotent = true

[[inputs]]
name = "file_id"
type = "string"
description = "Drive file id (e.g. \"1abcXYZ...\"), as returned by `search-files` in `files[].id`. Required."
required = true

[[inputs]]
name = "export_mime_type"
type = "string"
description = "Override the default text export type for Google native files (Docs / Sheets / Slides). Defaults: text/plain (Docs), text/csv (Sheets), text/plain (Slides). Override examples: \"text/html\", \"application/rtf\". Non-text targets succeed against Drive but the returned content may be corrupted by the JSON-string round-trip — stick to text targets in v1. Ignored for non-native files."
required = false
+++

# Read a Google Drive File's Content

Reads the text content of a Drive file. Handles two paths transparently:

- **Google native files** (Docs / Sheets / Slides) are exported via
  Drive's `/export` endpoint. The default export targets are text/plain
  for Docs and Slides, text/csv for Sheets — chosen because the
  dominant consumer is an agent reading content for context, and text
  is the cheapest faithful representation. Override via
  `export_mime_type`.
- **Non-native files** with text-shaped mimeTypes (text/*,
  application/json, application/xml, application/yaml, etc.) are
  downloaded directly via `alt=media`.

When it fires:
- "what does the Q2 plan doc say"
- "read the budget spreadsheet"
- "open the README in the docs folder"

Returns `{name, mimeType, exportedAs, content}`. `exportedAs` is the
mime the content is in — for native exports it's the requested or
default export type; for non-native downloads it's the file's source
mime. `content` is the file's text as a UTF-8 string.

**v1 scope: text content only.** Binary files (PDFs, images,
archives) are rejected with a clear error message. The reason is the
host-ABI's JSON-string body field: arbitrary bytes get coerced to
valid UTF-8 (replacing invalid sequences with the Unicode replacement
character), which silently corrupts binary content. Binary support is
a follow-up that needs a host-ABI binary-body field. Native Google
types with no text export (Drawings, Forms, Folders) also return a
clear error unless the caller passes `export_mime_type` with a text-
compatible target — there is no default fallback.

Pair with:
- `search-files` to find the file id in the first place,
- `get-doc-structure` when the agent needs the Docs API index space
  for precise edits (this action returns text, not structure),
- `get-file-metadata` when only the file's properties are needed
  (cheaper than reading content).

This is a read-only operation. The connector runs in the Aileron WASM
sandbox with `[capabilities.network]` restricted to
`www.googleapis.com:443`, and the OAuth bearer token is injected
host-side at the network boundary — the connector code never sees the
token. Uses the `drive` scope. See ADR-0005 (sandbox + credential
mediation) in the Aileron docs.
