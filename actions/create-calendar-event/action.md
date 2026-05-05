+++
name = "create-calendar-event"
# `version` and the `0.0.0-dev` markers in `source` and the
# `[[requires.connectors]]` block are placeholders. CI substitutes
# them with the real version (from the pushed tag) into a build copy
# of this manifest before signing and packing. Source stays template;
# only the published tarball carries the real version.
version = "0.0.0-dev"
source = "github://ALRubinger/aileron-connector-google/actions/create-calendar-event@0.0.0-dev"

[[requires.connectors]]
name = "github://ALRubinger/aileron-connector-google"
version = "0.0.0-dev"
# `hash` is the connector tarball's content-addressed identity per
# ADR-0002. CI substitutes this placeholder with the real hash at
# release time (see .github/workflows/release.yml). The committed
# source intentionally keeps the placeholder so each release runs the
# same substitution against an unchanged template.
hash = "sha256:bound-at-release"
capabilities = ["create_calendar_event"]

[match]
intent = "create a Google Calendar event with the specified time and attendees"

[[execute]]
id = "create"
connector = "github://ALRubinger/aileron-connector-google"
op = "create_calendar_event"
idempotent = false

# Per-call approval gate. The runtime asks the user via the
# launch-comms channel before dispatching to Calendar (see
# ALRubinger/aileron#421 for the manifest field + MCP signaling
# contract). On approval the connector runs; on denial the connector
# is never invoked, no quota is burned, and the runtime audit-logs
# the deny. create_calendar_event is gated because Calendar's
# events.insert dispatches invitation emails to the listed attendees
# as part of event creation — those "X invited you" notifications
# don't retract cleanly when the event is later deleted, so the call
# is closer to a send than to a draft. See issue #6 for the
# per-action gating rationale.
[approval]
required = true

[[inputs]]
name = "title"
type = "string"
description = "Event title (the Calendar \"summary\" field). Keep it short — what the event is about."
required = true

[[inputs]]
name = "start_time"
type = "string"
description = "Start time in RFC3339 format with timezone offset, e.g. \"2026-05-04T15:00:00-07:00\"."
required = true

[[inputs]]
name = "end_time"
type = "string"
description = "End time in RFC3339 format. Must be after start_time."
required = true

[[inputs]]
name = "timezone"
type = "string"
description = "Optional IANA timezone, e.g. \"America/New_York\". When set, Calendar treats start_time and end_time as wall-clock times in this zone (handy for recurring events). Omit when start_time/end_time already carry an offset."
required = false

[[inputs]]
name = "description"
type = "string"
description = "Optional long-form description (agenda, links, notes)."
required = false

[[inputs]]
name = "location"
type = "string"
description = "Optional physical address or virtual meeting URL."
required = false

[[inputs]]
name = "attendees"
type = "string"
description = "Optional comma-separated list of attendee email addresses. Calendar sends invitations to these addresses on event creation."
required = false

[[inputs]]
name = "calendar_id"
type = "string"
description = "Optional calendar ID. Defaults to \"primary\" — the user's main calendar."
required = false
+++

# Create a Calendar Event

Creates an event on the user's Google Calendar. Calendar sends
invitations to the listed attendees automatically as part of event
creation.

When it fires:
- "schedule a 30-minute design review with alice and bob next Tuesday at 2pm"
- "block tomorrow afternoon 1-3 for focus time"
- "set up the Q3 planning meeting on July 15 from 10 to noon"

This action **creates a real event** on the user's calendar and
**sends invitations** to listed attendees. Because invitation emails
are not reversible — recipients see "X invited you" notifications
that don't retract when the event is later deleted — this action is
**gated on per-call user approval**. When the agent calls
`create_calendar_event`, the Aileron runtime pauses the call and asks
the user to approve via the launch-comms channel (CLI prompt or the
webapp `/approvals` surface). The Calendar API is not contacted until
approval is granted; on denial the call returns an error to the agent
and is recorded in the audit log. See ADR-0009 (user channel — agent
in trust path) for the rationale.

It is **not idempotent** — invoking it twice creates two events with
two sets of invitations. The runtime's retry layer honors that and
will not double-write on transient failure.

The connector runs in the Aileron WASM sandbox with
`[capabilities.network]` restricted to `www.googleapis.com:443`, and
the OAuth bearer token is injected host-side at the network boundary —
the connector code never sees the token. See ADR-0005 (sandbox +
credential mediation) and ADR-0009 (user channel — agent in trust
path) in the Aileron docs.
