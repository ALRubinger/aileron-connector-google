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
label = "To"

[[inputs]]
name = "subject"
type = "string"
description = "Email subject line."
required = true
label = "Subject"

[[inputs]]
name = "body"
type = "string"
description = "Plain-text body of the email. The user will be asked to approve this exact body before send."
required = true
label = "Body"
multiline = true

[[inputs]]
name = "cc"
type = "string"
description = "Optional comma-separated Cc addresses."
required = false
label = "Cc"

[[inputs]]
name = "bcc"
type = "string"
description = "Optional comma-separated Bcc addresses."
required = false
label = "Bcc"

[[inputs]]
name = "in_reply_to_message_id"
type = "string"
description = "Optional. The Gmail message id you are replying to (as returned by find-recent-emails / read-email in `messages[].id` or `id`). When set, the sent message is nested inside that message's existing thread instead of starting a new one: the connector reads the original's Message-ID, References chain, and threadId, writes the In-Reply-To/References headers, prefixes \"Re: \" on the subject if not already present, and sends on the same thread. Leave empty for a brand-new conversation."
required = false
label = "In reply to (message id)"

[[inputs]]
name = "attachments"
type = "array"
items_type = "object"
description = "Optional. Array of files to attach, each an object `{\"filename\": \"report.html\", \"content\": \"<html>…</html>\", \"mimeType\": \"text/html\"}`. `filename` and `content` are required; `mimeType` defaults to `text/plain`. When present the email is sent as a multipart/mixed message and each file arrives as a true attachment (Content-Disposition: attachment). v1 supports **text-like content only** (text/*, application/json, application/xml, application/yaml, etc.) — a self-contained UTF-8 HTML report is the primary use case. Non-text content (PDF/image bytes) is rejected: binary attachments are deferred until the host exposes a binary-body carrier (see the note below)."
required = false
label = "Attachments"
+++

# Send an Email

Sends an email from the user's Gmail account. Unlike `draft-email`,
the message leaves the user's outbox immediately and lands in the
recipient's inbox — there is no Drafts-folder review step.

When it fires:
- "send the recap email we just drafted"
- "email alice that the deploy is done"
- "send a thank-you note to my hiring manager"

## Replying within a thread

To send a reply that lands **inside** an existing Gmail conversation
rather than as a standalone message grouped only by subject, pass the
original message's id as `in_reply_to_message_id` (you get it from
`find-recent-emails` or `read-email`). The connector fetches that
message, copies its threading headers (In-Reply-To / References) onto
the outgoing message, prefixes `Re: ` on the subject when it isn't
already, and sends on the same `threadId`. Omit the field and behavior
is exactly as before — a new thread. See issue #37.

## Attachments

Pass `attachments` to send the message as a `multipart/mixed` email
with one or more files attached. Each element is
`{"filename": "...", "content": "...", "mimeType": "..."}`; the
connector emits a text body part plus one base64-encoded attachment
part per file, so the recipient receives real attachments rather than
inline text or a Drive link. Omit `attachments` and the message is
byte-for-byte the previous single-part `text/plain` output — the plain
path is provably unchanged.

**Text-only in v1 (deferral).** Attachment content must be text-like
(text/*, application/json, application/xml, application/yaml, and
similar); a self-contained UTF-8 HTML report is the intended MVP and
round-trips cleanly. Binary attachments (PDFs, images) are **out of
scope for now**: the connector receives its request body as a JSON
string over the host ABI, which coerces arbitrary bytes to valid UTF-8
and would silently corrupt binary content. Lifting this needs a
host-ABI binary-body field that does not exist yet (tracked as an
external dependency in ALRubinger/aileron — the same gap that limits
the Drive `upload-file` path); until it lands, non-text `mimeType`
values are rejected with a clear error.

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
