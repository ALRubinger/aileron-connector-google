+++
name = "search-contacts"
# `version` and the `0.0.0-dev` markers in `source` and the
# `[[requires.connectors]]` block are placeholders. CI substitutes
# them with the real version (from the pushed tag) into a build copy
# of this manifest before signing and packing. Source stays template;
# only the published tarball carries the real version.
version = "0.0.0-dev"
source = "github://ALRubinger/aileron-connector-google/actions/search-contacts@0.0.0-dev"

[[requires.connectors]]
name = "github://ALRubinger/aileron-connector-google"
version = "0.0.0-dev"
# `hash` is the connector tarball's content-addressed identity per
# ADR-0002. CI substitutes this placeholder with the real hash at
# release time (see .github/workflows/release.yml). The committed
# source intentionally keeps the placeholder so each release runs the
# same substitution against an unchanged template.
hash = "sha256:bound-at-release"
capabilities = ["search_contacts"]

[match]
intent = "search Google Contacts by name, email, phone, or organization"

[[execute]]
id = "search"
connector = "github://ALRubinger/aileron-connector-google"
op = "search_contacts"
idempotent = true

[[inputs]]
name = "query"
type = "string"
description = "Search string matched against names, email addresses, phone numbers, organizations, and other searchable fields on the user's contacts. Required — an empty query is rejected before dispatch."
required = true

[[inputs]]
name = "read_mask"
type = "string"
description = "Comma-separated People API person fields to return on each match, e.g. \"names,emailAddresses,phoneNumbers,birthdays,addresses,organizations\". Defaults to \"names,emailAddresses,phoneNumbers,birthdays\". Only the fields named here appear in results."
required = false

[[inputs]]
name = "max_results"
type = "integer"
description = "Maximum number of contacts to return. Defaults to 10; clamped to 30 (the People API's documented per-request cap for this endpoint)."
required = false
+++

# Search Google Contacts

Searches the authenticated user's Google Contacts and returns matching
people with the fields you request. Powered by the People API's
`people:searchContacts` endpoint. Match is fuzzy across names, email
addresses, phone numbers, organizations, and other searchable fields —
"alice", "alice@example.com", and "Acme Corp" are all valid queries.

When it fires:
- "what's Alice's email address"
- "find Bob's phone number"
- "who do I have at Acme Corp"
- "when is Carol's birthday"

Returns the raw `people:searchContacts` response: a list of
`{person: {resourceName, names, emailAddresses, phoneNumbers, ...}}`
entries. Each `person.resourceName` (e.g. `people/c123456789`) feeds
`get-contact` if the agent needs a wider field set than the search
returned.

Quirk worth knowing: the People API recommends a cache-warming
empty-query request before the first real search so freshly modified
contacts appear. The connector intentionally skips that — the WASM
sandbox is stateless per call and the warm-up would double the quota
cost of every search. For most queries the existing search index is
already populated; on rare stale-result cases the documented mitigation
is to re-run the search after a moment.

This is a read-only operation. The connector runs in the Aileron WASM
sandbox with `[capabilities.network]` restricted to
`people.googleapis.com:443`, and the OAuth bearer token is injected
host-side at the network boundary — the connector code never sees the
token. See ADR-0005 (sandbox + credential mediation) and ADR-0006
(capability binding) in the Aileron docs.
