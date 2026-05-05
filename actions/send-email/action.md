+++
name = "send-email"
# `version` and the `0.0.0-dev` markers in `source` and the
# `[[requires.connectors]]` block are placeholders. CI substitutes
# them with the real version (from the pushed tag) into a build copy
# of this manifest before signing and packing. Source stays template;
# only the published tarball carries the real version.
version = "0.0.0-dev"
source = "github://ALRubinger/aileron-connector-google/actions/send-email@0.0.0-dev"

[[requires.connectors]]
name = "github://ALRubinger/aileron-connector-google"
version = "0.0.0-dev"
# `hash` is the connector tarball's content-addressed identity per
# ADR-0002. CI substitutes this placeholder with the real hash at
# release time (see .github/workflows/release.yml). The committed
# source intentionally keeps the placeholder so each release runs the
# same substitution against an unchanged template.
hash = "sha256:bound-at-release"
capabilities = ["send_email"]

[match]
intent = "send an email from the user's Gmail account"

[[execute]]
id = "send"
connector = "github://ALRubinger/aileron-connector-google"
op = "send_email"
idempotent = false

# Per-call approval gate. The runtime asks the user via the
# launch-comms channel before dispatching to Gmail (see
# ALRubinger/aileron#421 for the manifest field + MCP signaling
# contract). On approval the connector runs; on denial the connector
# is never invoked, no quota is burned, and the runtime audit-logs
# the deny. send_email is gated because dispatched mail is not
# recoverable the way a draft is — draft-email stays the safer
# default for unattended write paths.
[approval]
required = true

[[inputs]]
name = "to"
type = "string"
description = "Comma-separated recipient email addresses, e.g. \"alice@example.com, bob@example.com\"."
required = true

[[inputs]]
name = "subject"
type = "string"
description = "Email subject line."
required = true

[[inputs]]
name = "body"
type = "string"
description = "Plain-text body of the email. The user will be asked to approve this exact body before send."
required = true

[[inputs]]
name = "cc"
type = "string"
description = "Optional comma-separated Cc addresses."
required = false

[[inputs]]
name = "bcc"
type = "string"
description = "Optional comma-separated Bcc addresses."
required = false
+++

# Send an Email

Sends an email from the user's Gmail account. Unlike `draft-email`,
the message leaves the user's outbox immediately and lands in the
recipient's inbox — there is no Drafts-folder review step.

When it fires:
- "send the recap email we just drafted"
- "email alice that the deploy is done"
- "send a thank-you note to my hiring manager"

This action is **gated on per-call user approval**. When the agent
calls `send_email`, the Aileron runtime pauses the call and asks the
user to approve via the launch-comms channel (CLI prompt or the
webapp `/approvals` surface). The Gmail API is not contacted until
approval is granted. On denial the call returns an error to the agent
and is recorded in the audit log; no message is dispatched and no
quota is burned. See ADR-0009 (user channel — agent in trust path)
for the rationale.

This action writes to your Gmail (sends a message). It is **not
idempotent** — invoking it twice sends two emails. The runtime's
retry layer is configured to honor that and will not double-send on
transient failure.

`draft-email` remains the safer default for unattended flows: it
produces a draft the user reviews and sends from Gmail. Reach for
`send-email` only when the autonomy / latency win of skipping the
manual click is worth the per-call approval prompt.

The connector runs in the Aileron WASM sandbox with
`[capabilities.network]` restricted to `gmail.googleapis.com:443`, and
the OAuth bearer token is injected host-side at the network boundary —
the connector code never sees the token. Uses the same
`gmail.compose` scope as `draft-email`; no scope expansion or
re-verification cost. See ADR-0005 (sandbox + credential mediation)
and ADR-0009 (user channel — agent in trust path) in the Aileron
docs.
