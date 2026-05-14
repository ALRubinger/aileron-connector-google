+++
name = "list-contacts"
# `version` and the `0.0.0-dev` markers in `source` and the
# `[[requires.connectors]]` block are placeholders. CI substitutes
# them with the real version (from the pushed tag) into a build copy
# of this manifest before signing and packing. Source stays template;
# only the published tarball carries the real version.
version = "0.0.0-dev"
source = "github://ALRubinger/aileron-connector-google/actions/list-contacts@0.0.0-dev"

[[requires.connectors]]
name = "github://ALRubinger/aileron-connector-google"
version = "0.0.0-dev"
# `hash` is the connector tarball's content-addressed identity per
# ADR-0002. CI substitutes this placeholder with the real hash at
# release time (see .github/workflows/release.yml). The committed
# source intentionally keeps the placeholder so each release runs the
# same substitution against an unchanged template.
hash = "sha256:bound-at-release"
capabilities = ["list_contacts"]

[match]
intent = "list my Google Contacts"

[[execute]]
id = "list"
connector = "github://ALRubinger/aileron-connector-google"
op = "list_contacts"
idempotent = true

[[inputs]]
name = "person_fields"
type = "string"
description = "Comma-separated People API person fields to return on each connection, e.g. \"names,emailAddresses,phoneNumbers\". Defaults to that lean set since list responses can carry hundreds of records; pair with get_contact to fetch a wider field set per record."
required = false

[[inputs]]
name = "max_results"
type = "integer"
description = "Maximum number of contacts per page. Defaults to 100; capped at 100 to keep API quota usage predictable. Use `page_token` to paginate beyond the cap."
required = false

[[inputs]]
name = "page_token"
type = "string"
description = "Continuation token from a prior call's `nextPageToken`. Pass to fetch the next page; omit on the first call."
required = false

[[inputs]]
name = "sort_order"
type = "string"
description = "Optional ordering. One of \"LAST_MODIFIED_ASCENDING\", \"LAST_MODIFIED_DESCENDING\", \"FIRST_NAME_ASCENDING\", \"LAST_NAME_ASCENDING\". Unknown values surface as the People API's HTTP 400."
required = false
+++

# List Google Contacts

Enumerates the authenticated user's Google Contacts via the People
API's `people/me/connections` endpoint. Returns the raw response: a
list of `connections[]` (each a person object with the fields named in
`person_fields`) plus `nextPageToken`, `totalPeople`, and `totalItems`
for pagination.

When it fires:
- "who's in my contacts"
- "list everyone I have on file"
- bulk export / sync flows that walk the full address book

Unlike `search-contacts`, this returns connections without filtering —
which means it's the right call when the agent doesn't know who it's
looking for yet (e.g. "show me everyone whose birthday is this month"
where the filtering happens client-side after the fetch). For
keyword-style discovery, prefer `search-contacts`; its server-side
match is cheaper than fetching all connections and filtering locally.

Returns the raw `people.connections.list` response. Pair with
`get-contact` when a richer field set is needed for one record than
`person_fields` returned for all of them.

This is a read-only operation. The connector runs in the Aileron WASM
sandbox with `[capabilities.network]` restricted to
`people.googleapis.com:443`, and the OAuth bearer token is injected
host-side at the network boundary — the connector code never sees the
token. Uses the same `contacts.readonly` scope as `search-contacts` and
`get-contact`; no scope expansion. See ADR-0005 (sandbox + credential
mediation) and ADR-0006 (capability binding) in the Aileron docs.
