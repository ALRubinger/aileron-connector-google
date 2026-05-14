+++
name = "get-contact"
# `version` and the `0.0.0-dev` markers in `source` and the
# `[[requires.connectors]]` block are placeholders. CI substitutes
# them with the real version (from the pushed tag) into a build copy
# of this manifest before signing and packing. Source stays template;
# only the published tarball carries the real version.
version = "0.0.0-dev"
source = "github://ALRubinger/aileron-connector-google/actions/get-contact@0.0.0-dev"

[[requires.connectors]]
name = "github://ALRubinger/aileron-connector-google"
version = "0.0.0-dev"
# `hash` is the connector tarball's content-addressed identity per
# ADR-0002. CI substitutes this placeholder with the real hash at
# release time (see .github/workflows/release.yml). The committed
# source intentionally keeps the placeholder so each release runs the
# same substitution against an unchanged template.
hash = "sha256:bound-at-release"
capabilities = ["get_contact"]

[match]
intent = "fetch a single Google Contact by resource name"

[[execute]]
id = "get"
connector = "github://ALRubinger/aileron-connector-google"
op = "get_contact"
idempotent = true

[[inputs]]
name = "resource_name"
type = "string"
description = "Google-issued resource name of the form \"people/<id>\" (as returned by search_contacts / list_contacts in `resourceName`). The connector validates this prefix before dispatch; bare ids are rejected."
required = true

[[inputs]]
name = "person_fields"
type = "string"
description = "Comma-separated People API person fields to return, e.g. \"names,emailAddresses,phoneNumbers,birthdays,addresses,organizations,biographies,urls\". Defaults to that wider set since this is the drill-into-one-record call."
required = false
+++

# Get a Google Contact (full record)

Fetches a single contact by `resource_name` and returns the wide field
set requested in `person_fields`. Pair this with `search-contacts` or
`list-contacts` when the lean field shape returned by those ops is not
enough — for example when search returned `names + emails` and the
agent now wants the birthday or postal address for one specific
result.

When it fires:
- after `search_contacts`, drilling into one result for fields not in
  the search readMask
- "give me everything you have on Alice" with a known resource name
- summarization flows that fan out parallel `get_contact` calls across
  several search hits

Returns the raw `people.get` response — a single person object with
the fields named in `person_fields`. Fields not requested are omitted
by the API.

This is a read-only operation. The connector runs in the Aileron WASM
sandbox with `[capabilities.network]` restricted to
`people.googleapis.com:443`, and the OAuth bearer token is injected
host-side at the network boundary — the connector code never sees the
token. Uses the same `contacts.readonly` scope as `search-contacts` and
`list-contacts`; no scope expansion. See ADR-0005 (sandbox + credential
mediation) and ADR-0006 (capability binding) in the Aileron docs.
