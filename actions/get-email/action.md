+++
name = "get-email"
# `version` and the `0.0.0-dev` markers in `source` and the
# `[[requires.connectors]]` block are placeholders. CI substitutes
# them with the real version (from the pushed tag) into a build copy
# of this manifest before signing and packing. Source stays template;
# only the published tarball carries the real version.
version = "0.0.0-dev"
source = "github://ALRubinger/aileron-connector-google/actions/get-email@0.0.0-dev"

[[requires.connectors]]
name = "github://ALRubinger/aileron-connector-google"
version = "0.0.0-dev"
# `hash` is the connector tarball's content-addressed identity per
# ADR-0002. CI substitutes this placeholder with the real hash at
# release time (see .github/workflows/release.yml). The committed
# source intentionally keeps the placeholder so each release runs the
# same substitution against an unchanged template.
hash = "sha256:bound-at-release"
capabilities = ["get_email"]

[match]
intent = "fetch headers and a snippet (or the decoded body) for a single Gmail message by id"

[[execute]]
id = "get"
connector = "github://ALRubinger/aileron-connector-google"
op = "get_email"
idempotent = true

[[inputs]]
name = "id"
type = "string"
description = "Gmail message id (as returned by list_recent_emails in messages[].id)."
required = true

[[inputs]]
name = "format"
type = "string"
description = "How much to fetch: \"metadata\" (default — headers + snippet only, the cheap path) or \"full\" (additionally decode the MIME body into a top-level `body` field). Unrecognized values fall back to \"metadata\"."
required = false
+++

# Get a Gmail Message

Fetches a single Gmail message by id. By default (`format=metadata`)
it returns the message's headers (Subject, From, To, Date, etc.),
label ids, and a ~200-character body `snippet` — without fetching the
full MIME body. That metadata-only shape is the right cost trade-off
for "summarize my recent emails" and "what does this email say" agent
flows: ten metadata `get_email` calls cost about as much Gmail quota
as one `list_recent_emails` call.

Pass `format=full` to additionally fetch and decode the full message
body. The connector walks the nested MIME `payload` tree, decoding the
base64url-encoded `text/plain` part (falling back to `text/html` when
the sender shipped HTML only) into a top-level `body` field, with
`body_mime_type` naming which part it came from. A `multipart/mixed`
message with an attachment is handled as the normal case — the
attachment part is skipped and the text body still comes back.
Attachment bytes are out of scope. `format=full` is opt-in precisely
so the cheap default fan-out path stays cheap.

When it fires:
- after `list_recent_emails`, drilling into one or more results to
  read subjects/snippets (default `metadata`)
- "what does this email actually say" / "read me this email" with a
  known message id — pass `format=full` to get the decoded body
- summarization flows that need subject + sender + snippet across N
  recent messages — the agent fans out parallel `get_email` calls at
  the default `metadata` format

Returns the raw `users.messages.get` response (the `format=metadata`
or `format=full` variant depending on the arg). The agent typically
reads `payload.headers[]` (matching `name=Subject`, `name=From`, etc.)
and `snippet`; with `format=full` it reads the decoded `body`.

Both formats are read-only and **idempotent** — repeating the call
with the same id and format yields the same result and changes no
server state.

This is a read-only operation. The connector runs in the Aileron
WASM sandbox with `[capabilities.network]` restricted to
`gmail.googleapis.com:443`, and the OAuth bearer token is injected
host-side at the network boundary — the connector code never sees the
token. See ADR-0005 (sandbox + credential mediation) and ADR-0006
(capability binding) in the Aileron docs.
