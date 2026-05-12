+++
name = "get-draft"
# `version` and the `0.0.0-dev` markers in `source` and the
# `[[requires.connectors]]` block are placeholders. CI substitutes
# them with the real version (from the pushed tag) into a build copy
# of this manifest before signing and packing. Source stays template;
# only the published tarball carries the real version.
version = "0.0.0-dev"
source = "github://ALRubinger/aileron-connector-google/actions/get-draft@0.0.0-dev"

[[requires.connectors]]
name = "github://ALRubinger/aileron-connector-google"
version = "0.0.0-dev"
# `hash` is the connector tarball's content-addressed identity per
# ADR-0002. CI substitutes this placeholder with the real hash at
# release time (see .github/workflows/release.yml). The committed
# source intentionally keeps the placeholder so each release runs the
# same substitution against an unchanged template.
hash = "sha256:bound-at-release"
capabilities = ["get_draft"]

[match]
intent = "fetch headers and a snippet for a single Gmail draft by id"

[[execute]]
id = "get"
connector = "github://ALRubinger/aileron-connector-google"
op = "get_draft"
idempotent = true

[[inputs]]
name = "id"
type = "string"
description = "Gmail draft id (as returned by draft_email in the response's `id` field). Not the same as a message id from list_recent_emails — drafts have their own id space (typically `r-` prefixed)."
required = true
+++

# Get a Gmail Draft (metadata)

Fetches a single Gmail draft by id and returns its headers (Subject,
From, To, Date, etc.), label ids, and a ~200-character body snippet.
Does **not** fetch the full MIME body — the metadata-only shape is the
right cost trade-off for previewing what is about to be sent.

When it fires:
- after `draft-email`, to confirm the draft Gmail created matches
  what the agent intended
- immediately before `send-draft`, so the user can see To / Subject /
  snippet in chat and decide whether to dispatch
- "what's in draft r-12345"

Returns the raw `users.drafts.get?format=metadata` response. The
draft wraps a message — Subject / From / To live at
`message.payload.headers[]` (match by `name`), and the body preview at
`message.snippet`.

This is a read-only operation. The connector runs in the Aileron WASM
sandbox with `[capabilities.network]` restricted to
`gmail.googleapis.com:443`, and the OAuth bearer token is injected
host-side at the network boundary — the connector code never sees the
token. Uses the same `gmail.compose` scope already required by
`draft-email` and `send-draft`; no scope expansion. See ADR-0005
(sandbox + credential mediation) and ADR-0006 (capability binding) in
the Aileron docs.
