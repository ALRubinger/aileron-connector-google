+++
name = "send-draft"
# `version` and the `0.0.0-dev` markers in `source` and the
# `[[requires.connectors]]` block are placeholders. CI substitutes
# them with the real version (from the pushed tag) into a build copy
# of this manifest before signing and packing. Source stays template;
# only the published tarball carries the real version.
version = "0.0.0-dev"
source = "github://ALRubinger/aileron-connector-google/actions/send-draft@0.0.0-dev"

[[requires.connectors]]
name = "github://ALRubinger/aileron-connector-google"
version = "0.0.0-dev"
# `hash` is the connector tarball's content-addressed identity per
# ADR-0002. CI substitutes this placeholder with the real hash at
# release time (see .github/workflows/release.yml). The committed
# source intentionally keeps the placeholder so each release runs the
# same substitution against an unchanged template.
hash = "sha256:bound-at-release"
capabilities = ["send_draft"]

[match]
intent = "send an existing Gmail draft by id"

[[execute]]
id = "send"
connector = "github://ALRubinger/aileron-connector-google"
op = "send_draft"
idempotent = false

# Per-call approval gate. Same reasoning as send-email: dispatched
# mail is not reversible, so the runtime asks the user via the
# launch-comms channel before invoking the connector. On approval the
# connector runs; on denial the connector is never invoked, no quota
# is burned, and the runtime audit-logs the deny. Worth noting: the
# fact that the body was already authored as a draft does not lower
# the irreversibility — once dispatched, the message is gone.
#
# The approval prompt would otherwise show only the opaque draft_id,
# which gives the user no signal about what they are about to send.
# The [approval.preview] block below tells the runtime to call the
# read-only get_draft op against Gmail *before* showing the prompt,
# so the user sees authoritative To / Subject / snippet pulled
# straight from the same source send_draft will dispatch from.
# Agent-supplied previews are deliberately not used (ADR-0009: agent
# is never in the trust path); see ADR-0016 in the Aileron docs for
# the preview directive's contract, validation rules, and failure
# modes. The render order here is the order the manifest declares
# the keys, which is the order the approval UI renders them.
[approval]
required = true

[approval.preview]
op = "get_draft"
args = { id = "${args.draft_id}" }
multiline = ["Body"]

[approval.preview.render]
To      = "message.payload.headers.To"
Subject = "message.payload.headers.Subject"
Body    = "message.snippet"

[[inputs]]
name = "draft_id"
type = "string"
description = "Gmail draft id to dispatch. Comes from `draft_email`'s response (the `id` field on the created draft) or from the user. The draft's recipients, subject, and body are not re-specified here — they live on the draft in Gmail."
required = true
+++

# Send an Existing Draft

Dispatches an existing draft from the user's Gmail Drafts folder. The
draft's recipients, subject, and body are already set in Gmail — this
action only takes the draft id and tells Gmail to send it. Pairs
naturally with `draft-email`: the agent (or the user) drafts first,
reviews, then sends without the agent having to reconstruct the body.

When it fires:
- "send the draft we just created"
- "send draft r-12345"
- "send the recap draft I left in my drafts folder"

This action is **gated on per-call user approval**. When the agent
calls `send_draft`, the Aileron runtime pauses the call and asks the
user to approve via the launch-comms channel (CLI prompt or the
webapp `/approvals` surface). The Gmail API's `drafts/send` endpoint
is not contacted until approval is granted. On denial the call
returns an error to the agent and is recorded in the audit log; no
message is dispatched and no quota is burned. See ADR-0009 (user
channel — agent in trust path) for the rationale.

The approval prompt surfaces an **authoritative preview** fetched
from Gmail at approval time — not from the agent. Before showing the
prompt, the runtime invokes `get_draft` (an idempotent read-only op
on the same connector) against the supplied `draft_id` and renders
the draft's To and Subject inline, with the Body shown as a
scrollable blockquote (per ADR-0016's `multiline` directive) so the
user can read the full message before approving. The preview output
is shown only to the user; it is never returned to the agent's
context. If the fetch fails (e.g., the agent passed a message id
from `list_recent_emails` instead of a draft id and Gmail returns
404), the prompt renders "preview unavailable: `<reason>`" and the
user can deny on the spot. See ADR-0016 (approval-time preview
fetch) for the directive's contract, validation rules, and failure
modes.

This action writes to your Gmail (sends a message). It is **not
idempotent** — invoking it twice on the same draft id will send once
and then fail with a 404 on the second attempt (the draft no longer
exists after a successful send), but the runtime's retry layer is
still configured to honor `idempotent = false` and will not retry on
transient failure.

`draft-email` remains the safer default for unattended flows. Reach
for `send-draft` when there is already a draft in the user's Drafts
folder — either because `draft-email` produced it earlier in the
session, or because the user composed it by hand and asked the agent
to dispatch.

The connector runs in the Aileron WASM sandbox with
`[capabilities.network]` restricted to `gmail.googleapis.com:443`, and
the OAuth bearer token is injected host-side at the network boundary —
the connector code never sees the token. Uses the same
`gmail.compose` scope as `draft-email` and `send-email`; no scope
expansion or re-verification cost. See ADR-0005 (sandbox + credential
mediation) and ADR-0009 (user channel — agent in trust path) in the
Aileron docs.
