+++
name = "draft-email"
version = "0.3.0"
source = "github://ALRubinger/aileron-connector-google/actions/draft-email@0.3.0"

[[requires.connectors]]
name = "github://ALRubinger/aileron-connector-google"
version = "0.3.0"
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
description = "Plain-text body of the email. The user will review and send the draft from Gmail; you are not sending it."
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

# Draft an Email

Composes an email and saves it to the user's Gmail drafts folder. The
draft is **not sent** — the user reviews the draft in Gmail and
chooses to send (or edit, or discard) from there. This is the safer
shape for v0.1.0: agents produce drafts, humans send.

When it fires:
- "draft a reply to alice saying we'll have the migration done by Friday"
- "write an email to the team about the deploy outage"
- "draft a thank-you note to my hiring manager"

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
