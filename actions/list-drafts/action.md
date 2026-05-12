+++
name = "list-drafts"
# `version` and the `0.0.0-dev` markers in `source` and the
# `[[requires.connectors]]` block are placeholders. CI substitutes
# them with the real version (from the pushed tag) into a build copy
# of this manifest before signing and packing. Source stays template;
# only the published tarball carries the real version.
version = "0.0.0-dev"
source = "github://ALRubinger/aileron-connector-google/actions/list-drafts@0.0.0-dev"

[[requires.connectors]]
name = "github://ALRubinger/aileron-connector-google"
version = "0.0.0-dev"
# `hash` is the connector tarball's content-addressed identity per
# ADR-0002. CI substitutes this placeholder with the real hash at
# release time (see .github/workflows/release.yml). The committed
# source intentionally keeps the placeholder so each release runs the
# same substitution against an unchanged template.
hash = "sha256:bound-at-release"
capabilities = ["list_drafts"]

[match]
intent = "list my Gmail drafts"

[[execute]]
id = "list"
connector = "github://ALRubinger/aileron-connector-google"
op = "list_drafts"
idempotent = true

[[inputs]]
name = "query"
type = "string"
description = "Optional Gmail search query, e.g. \"subject:invoice\" or \"to:alice@example.com\". Passed through to the API's `q` parameter. Empty fetches drafts without filtering."
required = false

[[inputs]]
name = "max_results"
type = "integer"
description = "Maximum number of drafts to return. Defaults to 10; capped at 100 to keep API quota usage predictable."
required = false

[[inputs]]
name = "page_token"
type = "string"
description = "Continuation token from a prior call's `nextPageToken`. Pass to fetch the next page; omit on the first call."
required = false
+++

# List Gmail Drafts

Fetches a list of Gmail drafts for the authenticated user. Returns the
raw `users.drafts.list` response — a paginated list of
`{id, message: {id, threadId}}` entries plus `nextPageToken` and
`resultSizeEstimate`. The list shape carries ids only; pair with
`get-draft` to resolve headers and a snippet, then `send-draft` to
dispatch.

When it fires:
- "send the draft I composed yesterday about the invoice"
- "what drafts do I have sitting in Gmail right now"
- "find the draft I wrote to alice@example.com"

This is the read-only counterpart to `draft-email` / `send-draft` /
`get-draft`. Without it the agent cannot discover drafts it did not
create itself in the current session (e.g. drafts the user composed
earlier in Gmail's UI). The two-call composition
`list_drafts` → `get_draft` → `send_draft` mirrors
`list_recent_emails` → `get_email` for received messages.

Read-only operation. The connector runs in the Aileron WASM sandbox
with `[capabilities.network]` restricted to `gmail.googleapis.com:443`,
and the OAuth bearer token is injected host-side at the network
boundary — the connector code never sees the token. Uses the same
`gmail.compose` scope already required by `draft-email`, `send-draft`,
`send-email`, and `get-draft`; no scope expansion. See ADR-0005
(sandbox + credential mediation) and ADR-0006 (capability binding) in
the Aileron docs.
