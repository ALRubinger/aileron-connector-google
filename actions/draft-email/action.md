+++
name = "draft-email"
# `version` and the `0.0.0-dev` markers in `source` and the
# `[[requires.connectors]]` block are placeholders. CI substitutes
# them with the real version (from the pushed tag) into a build copy
# of this manifest before signing and packing. Source stays template;
# only the published tarball carries the real version.
version = "0.0.0-dev"
source = "github://ALRubinger/aileron-connector-google/actions/draft-email@0.0.0-dev"

[[requires.connectors]]
name = "github://ALRubinger/aileron-connector-google"
version = "0.0.0-dev"
# `hash` is the connector tarball's content-addressed identity per
# ADR-0002. CI substitutes this placeholder with the real hash at
# release time (see .github/workflows/release.yml). The committed
# source intentionally keeps the placeholder so each release runs the
# same substitution against an unchanged template.
hash = "sha256:bound-at-release"
capabilities = ["draft_email"]

[match]
intent = "draft an email and save it to the Gmail drafts folder"

[[execute]]
id = "draft"
connector = "github://ALRubinger/aileron-connector-google"
op = "draft_email"
idempotent = false

# No `[approval]` block here — the absence is deliberate. Drafts land
# in Gmail's Drafts folder and are fully reversible (the user can
# discard or edit before sending), and Gmail's UI already provides
# the human-in-the-loop review step (the user clicks Send from the
# Drafts folder). A runtime-level approval prompt would duplicate
# that review without adding safety, and would penalize the
# safer-by-default action with extra friction relative to send-email.
# See issue #6 for the per-action gating rationale across the
# connector's write actions.

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
description = "Plain-text body of the email. The user will review and send the draft from Gmail; you are not sending it."
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
description = "Optional. The Gmail message id you are replying to (as returned by find-recent-emails / read-email in `messages[].id` or `id`). When set, the draft is nested inside that message's existing thread instead of starting a new one: the connector reads the original's Message-ID, References chain, and threadId, writes the In-Reply-To/References headers, prefixes \"Re: \" on the subject if not already present, and files the draft on the same thread. Leave empty for a brand-new conversation."
required = false
label = "In reply to (message id)"
+++

# Draft an Email

Composes an email and saves it to the user's Gmail drafts folder. The
draft is **not sent** — the user reviews the draft in Gmail and
chooses to send (or edit, or discard) from there. This is the safer
default shape for the connector: agents produce drafts, humans send.

When it fires:
- "draft a reply to alice saying we'll have the migration done by Friday"
- "write an email to the team about the deploy outage"
- "draft a thank-you note to my hiring manager"

## Replying within a thread

To draft a reply that lands **inside** an existing Gmail conversation
rather than as a standalone message grouped only by subject, pass the
original message's id as `in_reply_to_message_id` (you get it from
`find-recent-emails` or `read-email`). The connector fetches that
message, copies its threading headers (In-Reply-To / References) onto
the draft, prefixes `Re: ` on the subject when it isn't already, and
files the draft on the same `threadId`. Omit the field and behavior is
exactly as before — a new thread. See issue #37.

This action writes to your Gmail (creates a draft). It is **not
idempotent** — invoking it twice creates two drafts. The runtime's
retry layer is configured to honor that and will not double-write on
transient failure.

The connector runs in the Aileron WASM sandbox with
`[capabilities.network]` restricted to `gmail.googleapis.com:443`, and
the OAuth bearer token is injected host-side at the network boundary —
the connector code never sees the token. See ADR-0005 (sandbox +
credential mediation) and ADR-0009 (user channel — agent in
trust path) in the Aileron docs.
