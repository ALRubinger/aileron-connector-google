+++
name = "upload-file"
# `version` and the `0.0.0-dev` markers in `source` and the
# `[[requires.connectors]]` block are placeholders. CI substitutes
# them with the real version (from the pushed tag) into a build copy
# of this manifest before signing and packing. Source stays template;
# only the published tarball carries the real version.
version = "0.0.0-dev"
source = "github://ALRubinger/aileron-connector-google/actions/upload-file@0.0.0-dev"

[[requires.connectors]]
name = "github://ALRubinger/aileron-connector-google"
version = "0.0.0-dev"
# `hash` is the connector tarball's content-addressed identity per
# ADR-0002. CI substitutes this placeholder with the real hash at
# release time (see .github/workflows/release.yml). The committed
# source intentionally keeps the placeholder so each release runs the
# same substitution against an unchanged template.
hash = "sha256:bound-at-release"
capabilities = ["upload_file"]

[match]
intent = "upload a text file to Google Drive"

[[execute]]
id = "upload"
connector = "github://ALRubinger/aileron-connector-google"
op = "upload_file"
idempotent = false

# No `[approval]` block. Uploads land in the user's Drive private by
# default and are reversible — the user can trash the file from Drive.
# Same reasoning as create-doc; see issue #6 for the per-action
# gating rationale across the connector's write actions.

[[inputs]]
name = "name"
type = "string"
description = "File name (the Drive display name)."
required = true

[[inputs]]
name = "content"
type = "string"
description = "File content as UTF-8 text. v1 supports text content only — markdown, code, plain text, JSON / XML / YAML / TOML payloads. Binary content (PDFs, images, archives) is rejected with a clear error; binary support is a follow-up that needs a host-ABI binary-body field."
required = true

[[inputs]]
name = "mime_type"
type = "string"
description = "Optional content mimeType. Defaults to text/plain. Examples: text/markdown, text/csv, application/json. To create a native Google Doc instead, use `create-doc` — its Docs API path gives a proper Doc document_id back."
required = false

[[inputs]]
name = "parents"
type = "string"
description = "Optional comma-separated parent folder id(s), e.g. \"1abc...,1def...\". Omit to land in the user's My Drive root."
required = false
+++

# Upload a File to Google Drive

Uploads a new file to Drive via Drive v3's multipart upload endpoint
(`upload/drive/v3/files?uploadType=multipart`). The file lands in the
user's Drive — by default in My Drive root, optionally in one or more
specified folders.

When it fires:
- "upload these notes as notes.md"
- "save this CSV to my Reports folder"
- "create a config.json in Drive with this content"

Two-part multipart/related body: a JSON metadata part (name, mimeType,
parents) and the content part. The connector packs the parts; the
agent only supplies the inputs.

**v1 scope: UTF-8 text content only.** Binary files (PDFs, images,
archives) are rejected upfront with a clear error message. The reason
is the host-ABI's JSON-string body field: arbitrary bytes get
coerced to valid UTF-8 (replacing invalid sequences with the Unicode
replacement character), which silently corrupts binary content.
Binary support is a follow-up that needs a host-ABI binary-body
field.

For creating a native Google Doc, use `create-doc` — it returns a
proper Docs document_id that `update-doc` and `get-doc-structure` can
target. Uploading text/plain or text/markdown through this action
creates a Drive file, not a Doc, and Docs-specific operations will
not work against it.

This action writes to your Drive (creates a file). It is **not
idempotent** — invoking it twice creates two files. The runtime's
retry layer is configured to honor that and will not double-write on
transient failure. No approval gate: uploads are reversible (trash
from Drive) and private by default.

The connector runs in the Aileron WASM sandbox with
`[capabilities.network]` restricted to `www.googleapis.com:443`, and
the OAuth bearer token is injected host-side at the network boundary
— the connector code never sees the token. Uses the `drive` scope.
See ADR-0005 (sandbox + credential mediation) in the Aileron docs.
