+++
name = "list-upcoming-events"
# `version` and the `0.0.0-dev` markers in `source` and the
# `[[requires.connectors]]` block are placeholders. CI substitutes
# them with the real version (from the pushed tag) into a build copy
# of this manifest before signing and packing. Source stays template;
# only the published tarball carries the real version.
version = "0.0.0-dev"
source = "github://ALRubinger/aileron-connector-google/actions/list-upcoming-events@0.0.0-dev"

[[requires.connectors]]
name = "github://ALRubinger/aileron-connector-google"
version = "0.0.0-dev"
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

[[inputs]]
name = "time_min"
type = "string"
description = "RFC3339 lower bound for the window (e.g. \"2026-06-15T00:00:00Z\"). Overrides the default, which is the current time. Use it to start the window on a specific day."
required = false

[[inputs]]
name = "time_max"
type = "string"
description = "RFC3339 upper bound for the window (e.g. \"2026-06-21T23:59:59Z\"). Optional; when set, bounds the far edge so unbounded recurring expansions (e.g. yearly birthdays) don't crowd out the page. Pair with time_min to request a precise range like June 15–21."
required = false
+++

# List Upcoming Calendar Events

Fetches a chronological list of upcoming events from the authenticated
user's Google Calendar, starting from the current time. Recurring
events are expanded into their concrete instances (`singleEvents=true`)
so each entry corresponds to one occurrence.

The window defaults to "now onward," but `time_min` and `time_max`
(both RFC3339) bound it explicitly. Set `time_min` to start the window
on a specific day; set `time_max` to cap the far edge — useful for a
precise range like "June 15–21" and for keeping unbounded far-future
recurring expansions (yearly birthdays, etc.) from filling the page.
Server-side bounding also avoids over-fetching and filtering
client-side.

When it fires:
- "what's on my calendar this week"
- "do I have any meetings tomorrow"
- "show me my schedule"
- "what's on my calendar between June 15 and June 21"

Returns the raw `events.list` response from Google Calendar — each item
includes summary, start/end times, attendees, location, and conference
data when present. The agent typically paraphrases the result.

This is a read-only operation. The connector runs in the Aileron WASM
sandbox with `[capabilities.network]` restricted to
`www.googleapis.com:443`, and the OAuth bearer token is injected
host-side at the network boundary — the connector code never sees the
token. See ADR-0005 (sandbox + credential mediation) and ADR-0006
(capability binding) in the Aileron docs.
