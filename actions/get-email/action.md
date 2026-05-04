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
intent = "fetch headers and a snippet for a single Gmail message by id"

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
+++

# Get a Gmail Message (metadata)

Fetches a single Gmail message by id and returns its headers
(Subject, From, To, Date, etc.), label ids, and a ~200-character body
snippet. Does **not** fetch the full MIME body — that is a future
extension. The metadata-only shape is the right cost trade-off for
"summarize my recent emails" and "what does this email say" agent
flows: ten `get_email` calls cost about as much Gmail quota as one
`list_recent_emails` call.

When it fires:
- after `list_recent_emails`, drilling into one or more results to
  read subjects/snippets
- "what does this email say" with a known message id
- summarization flows that need subject + sender + snippet across N
  recent messages — the agent fans out parallel `get_email` calls

Returns the raw `users.messages.get?format=metadata` response. The
agent typically reads `payload.headers[]` (matching `name=Subject`,
`name=From`, etc.) and `snippet`.

This is a read-only operation. The connector runs in the Aileron
WASM sandbox with `[capabilities.network]` restricted to
`gmail.googleapis.com:443`, and the OAuth bearer token is injected
host-side at the network boundary — the connector code never sees the
token. See ADR-0005 (sandbox + credential mediation) and ADR-0006
(capability binding) in the Aileron docs.
