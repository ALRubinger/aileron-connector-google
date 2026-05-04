+++
name = "list-recent-emails"
version = "0.3.0"
source = "github://ALRubinger/aileron-connector-google/actions/list-recent-emails@0.3.0"

[[requires.connectors]]
name = "github://ALRubinger/aileron-connector-google"
version = "0.3.0"
# `hash` is the connector tarball's content-addressed identity per
# ADR-0002. CI substitutes this placeholder with the real hash at
# release time (see .github/workflows/release.yml). The committed
# source intentionally keeps the placeholder so each release runs the
# same substitution against an unchanged template.
hash = "sha256:bound-at-release"
capabilities = ["list_recent_emails"]

[match]
intent = "list my recent Gmail messages"

[[execute]]
id = "list"
connector = "github://ALRubinger/aileron-connector-google"
op = "list_recent_emails"
idempotent = true

[[inputs]]
name = "query"
type = "string"
description = "Optional Gmail search query, e.g. \"is:unread\" or \"from:alice@example.com\". Empty fetches the most recent messages without filtering."
required = false

[[inputs]]
name = "max_results"
type = "integer"
description = "Maximum number of messages to return. Defaults to 10; capped at 100 to keep API quota usage predictable."
required = false
+++

# List Recent Gmail Messages

Fetches a list of recent Gmail messages for the authenticated user.
Returns the raw `users.messages.list` response from Gmail (a paginated
list of `{id, threadId}` pairs plus a `resultSizeEstimate`); the agent
or a downstream action resolves message bodies as needed.

When it fires:
- "summarize my unread emails from this week"
- "show me messages from alice@example.com"
- "what's in my inbox right now"

This is a read-only operation. The connector runs in the Aileron WASM
sandbox with `[capabilities.network]` restricted to
`gmail.googleapis.com:443`, and the OAuth bearer token is injected
host-side at the network boundary — the connector code never sees the
token. See ADR-0005 (sandbox + credential mediation) and ADR-0006
(capability binding) in the Aileron docs.
