+++
name = "list-upcoming-events"
version = "0.3.0"
source = "github://ALRubinger/aileron-connector-google/actions/list-upcoming-events@0.3.0"

[[requires.connectors]]
name = "github://ALRubinger/aileron-connector-google"
version = "0.3.0"
# `hash` is the connector tarball's content-addressed identity per
# ADR-0002. CI substitutes this placeholder with the real hash at
# release time (see .github/workflows/release.yml). The committed
# source intentionally keeps the placeholder so each release runs the
# same substitution against an unchanged template.
hash = "sha256:bound-at-release"
capabilities = ["list_upcoming_events"]

[match]
intent = "list my upcoming Google Calendar events"

[[execute]]
id = "list"
connector = "github://ALRubinger/aileron-connector-google"
op = "list_upcoming_events"
idempotent = true

[[inputs]]
name = "calendar_id"
type = "string"
description = "Calendar ID to query. Defaults to \"primary\" — the user's main calendar."
required = false

[[inputs]]
name = "max_results"
type = "integer"
description = "Maximum number of events to return. Defaults to 10; capped at 100."
required = false
+++

# List Upcoming Calendar Events

Fetches a chronological list of upcoming events from the authenticated
user's Google Calendar, starting from the current time. Recurring
events are expanded into their concrete instances (`singleEvents=true`)
so each entry corresponds to one occurrence.

When it fires:
- "what's on my calendar this week"
- "do I have any meetings tomorrow"
- "show me my schedule"

Returns the raw `events.list` response from Google Calendar — each item
includes summary, start/end times, attendees, location, and conference
data when present. The agent typically paraphrases the result.

This is a read-only operation. The connector runs in the Aileron WASM
sandbox with `[capabilities.network]` restricted to
`www.googleapis.com:443`, and the OAuth bearer token is injected
host-side at the network boundary — the connector code never sees the
token. See ADR-0005 (sandbox + credential mediation) and ADR-0006
(capability binding) in the Aileron docs.
