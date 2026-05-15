# aileron-connector-google

Aileron connector for Google APIs — Gmail, Calendar, Contacts, and Google Drive + Docs read/write.

This repo is the first reference connector for the Aileron action runtime
(see [github.com/ALRubinger/aileron](https://github.com/ALRubinger/aileron)).
It demonstrates the full publisher flow: WASM connector source, manifest
declaring sandboxed capabilities and OAuth provider config, action
templates with declared inputs, ed25519-signed release tarballs.

## What it ships

Twenty-one operations, twelve read + nine write, across Gmail,
Calendar, Google Contacts, Google Drive, and Google Docs:

| Action | Op | HTTP | Endpoint |
|---|---|---|---|
| `list-recent-emails` | `list_recent_emails` | GET | `gmail.googleapis.com/gmail/v1/users/me/messages` |
| `get-email` | `get_email` | GET | `gmail.googleapis.com/gmail/v1/users/me/messages/{id}?format=metadata` |
| `list-drafts` | `list_drafts` | GET | `gmail.googleapis.com/gmail/v1/users/me/drafts` |
| `get-draft` | `get_draft` | GET | `gmail.googleapis.com/gmail/v1/users/me/drafts/{id}?format=metadata` |
| `list-upcoming-events` | `list_upcoming_events` | GET | `www.googleapis.com/calendar/v3/calendars/{calendarId}/events` |
| `search-contacts` | `search_contacts` | GET | `people.googleapis.com/v1/people:searchContacts` |
| `get-contact` | `get_contact` | GET | `people.googleapis.com/v1/{resourceName}` |
| `list-contacts` | `list_contacts` | GET | `people.googleapis.com/v1/people/me/connections` |
| `search-files` | `search_files` | GET | `www.googleapis.com/drive/v3/files` |
| `get-file-content` | `get_file_content` | GET | `www.googleapis.com/drive/v3/files/{id}` (export or `alt=media`) |
| `get-file-metadata` | `get_file_metadata` | GET | `www.googleapis.com/drive/v3/files/{id}` |
| `get-doc-structure` | `get_doc_structure` | GET | `docs.googleapis.com/v1/documents/{documentId}` |
| `draft-email` | `draft_email` | POST | `gmail.googleapis.com/gmail/v1/users/me/drafts` |
| `send-email` | `send_email` | POST | `gmail.googleapis.com/gmail/v1/users/me/messages/send` |
| `send-draft` | `send_draft` | POST | `gmail.googleapis.com/gmail/v1/users/me/drafts/send` |
| `create-calendar-event` | `create_calendar_event` | POST | `www.googleapis.com/calendar/v3/calendars/{calendarId}/events` |
| `create-doc` | `create_doc` | POST | `docs.googleapis.com/v1/documents` (+ `documents/{id}:batchUpdate` for initial body) |
| `upload-file` | `upload_file` | POST | `www.googleapis.com/upload/drive/v3/files?uploadType=multipart` |
| `update-doc` | `update_doc` | POST | `docs.googleapis.com/v1/documents/{documentId}:batchUpdate` |
| `rename-file` | `rename_file` | PATCH | `www.googleapis.com/drive/v3/files/{id}` |
| `move-file` | `move_file` | PATCH | `www.googleapis.com/drive/v3/files/{id}?addParents=&removeParents=` |

`list-recent-emails` returns `{id, threadId}` pairs only — the cheapest
shape for the Gmail API. Pair it with `get-email` to drill into
metadata (subject, from, snippet) for one or more results. Agent flows
that summarize the inbox typically fan out parallel `get-email` calls
after one `list-recent-emails`.

All twenty-one run inside the Aileron WASM sandbox with
`[capabilities.network]` restricted to `gmail.googleapis.com:443`,
`www.googleapis.com:443`, `people.googleapis.com:443`, and
`docs.googleapis.com:443`. The connector never holds OAuth tokens —
Aileron's runtime resolves the bound credential and injects
`Authorization: Bearer <token>` host-side when the connector marks an
outbound HTTP request with `credential: "oauth2"` (see ADR-0005
credential mediation in the Aileron docs).

Write idempotency splits two ways:

- **Not idempotent** (creates / sends / structured edits): `draft_email`,
  `send_email`, `send_draft`, `create_calendar_event`, `create_doc`,
  `upload_file`, `update_doc`. Repeating these creates duplicate
  drafts/messages/events/docs/files, or applies the same edit twice.
  Their action manifests declare `[[execute]].idempotent = false` so
  the gateway's retry layer (ADR-0010) does not double-write on
  transient failures.
- **Idempotent in effect** (target-state writes): `rename_file`,
  `move_file`. Re-issuing with the same arguments leaves Drive in the
  same state. Their manifests declare `idempotent = true` so the
  retry layer may safely re-issue between the approval grant and the
  API call.

Approval gating is per-action and reflects reversibility plus
third-party observability (the runtime prompts the user via the
launch-comms channel — CLI or the webapp `/approvals` surface —
before invoking the connector; on denial nothing reaches Google):

| Action | Gated? | Why |
|---|---|---|
| `draft-email` | no | Drafts are reversible; Gmail's Send UI is the human-in-the-loop. |
| `send-email` | **yes** | Dispatched mail is irreversible. |
| `send-draft` | **yes** | Same as send-email; preview op = `get_draft`. |
| `create-calendar-event` | **yes** | `events.insert` dispatches attendee invites that don't retract cleanly. |
| `create-doc` | no | Reversible via Drive trash; doc is private to creator. |
| `upload-file` | no | Reversible via Drive trash; file is private to creator. |
| `update-doc` | no | Reversible via Docs revision history (owner can roll back). |
| `rename-file` | no | Reversible by renaming back; no third-party side effect. |
| `move-file` | **yes** | Changes folder-inherited permissions — third-party-observable to collaborators in source/destination folders, the same kind of side effect that gates `send-email`. Preview op = `get_file_metadata`. |

The decision matrix the Drive writes follow: **reversibility + private
side effects → no gate; irreversibility or third-party-observable
side effects → gate.** Docs revision history is the safety net for
`update-doc`; Drive's permission-inheritance side effect is the
reason `move-file` is the one Drive write that gates.

The gating posture is recorded in each action.md's `[approval]`
block (or the comment explaining its absence). Future write actions
inherit nothing — each one's gating is its own judgment call.

## Demo path

```sh
# Install the connector and an action. Replace <version> with a tag
# from the releases page. The Aileron resolver requires a pinned
# version per ADR-0004 — there is no `latest` channel.
aileron connector install github://ALRubinger/aileron-connector-google@<version>
aileron action add github://ALRubinger/aileron-connector-google/actions/list-recent-emails@<version>

# CLI auto-prompts for OAuth setup; complete the consent in the browser.
# Aileron stores the refresh token in your local vault; the connector
# never sees it.

# Launch your agent. Aileron exposes the action via MCP.
aileron launch claude

# In the agent: "summarize my unread emails from this week"
# The LLM picks list_recent_emails, Aileron executes it in the WASM
# sandbox with the bound credential, returns the result, the LLM
# summarizes.
```

## Repo layout

```
aileron-connector-google/
├── connector/
│   ├── main.go         # wasip1 source — calls aileron_host.* imports
│   ├── go.mod
│   └── manifest.toml   # capability declarations + OAuth provider config
├── actions/
│   ├── list-recent-emails/action.md
│   ├── get-email/action.md
│   ├── list-drafts/action.md
│   ├── get-draft/action.md
│   ├── list-upcoming-events/action.md
│   ├── search-contacts/action.md
│   ├── get-contact/action.md
│   ├── list-contacts/action.md
│   ├── draft-email/action.md
│   ├── send-email/action.md
│   ├── send-draft/action.md
│   └── create-calendar-event/action.md
├── keys/
│   └── publisher.pub   # ed25519 public key — installed users add to
│                       # ~/.aileron/keyring.json to trust this publisher
├── Taskfile.yml        # local build
└── .github/workflows/release.yml  # signed release on tag push
```

## Building locally

```sh
task build
```

Produces `connector.wasm` from `connector/main.go` (Go's native WASI
Preview 1 target).

## Testing

Three layers, runnable independently:

### 1. Unit tests — pure helpers, host platform

```sh
task test:unit
```

Runs `go test ./connector/...` against the helper functions
(`buildRFC2822`, `normalizeAttendees`, `readMaxResults`) that have no
host-import dependencies. These live in `connector/helpers.go` (no
build tag) so `go test` exercises them on the host platform; the
WASM-only entry point in `connector/main.go` is excluded by its
`//go:build wasip1` tag during host builds.

### 2. wasip1 build smoke test

```sh
task test:wasip1
```

Confirms `connector/main.go` still compiles for the wasip1 target
(catches host-import signature mismatches, missing imports, etc.).
Runs as `GOOS=wasip1 GOARCH=wasm go build -o /dev/null .`.

`task test` runs both of the above.

### 3. Live API integration via the Aileron dev runner

The unit tests don't exercise the host-import path or hit Google's
APIs. For that, use [`aileron-connector-dev-run`](https://github.com/ALRubinger/aileron/tree/main/cmd/aileron-connector-dev-run)
in the Aileron repo. It loads this connector's `connector.wasm` into
the production Wazero runtime, enforces the manifest's
`[capabilities.network]` grant, wires a stub credential resolver from
`AILERON_DEV_TOKEN`, and invokes ops directly:

```sh
# Get a token from https://developers.google.com/oauthplayground
# (gmail.compose + calendar.events scopes — or whichever ops you're testing).
export AILERON_DEV_TOKEN=ya29...

cd ~/git/ALRubinger/aileron && task build:connector-dev-run

./build/aileron-connector-dev-run \
  --wasm     ~/git/ALRubinger/aileron-connector-google/connector.wasm \
  --manifest ~/git/ALRubinger/aileron-connector-google/connector/manifest.toml \
  --op       create_calendar_event \
  --args     '{"title":"test","start_time":"2026-05-04T15:00:00-07:00","end_time":"2026-05-04T15:30:00-07:00"}'
```

The op runs against real Google APIs; output is the parsed Calendar
event response. This validates the credential-mediation path, the
network grant, and the API call shape end-to-end without going
through release / install / binding setup.

## Releasing

**One tag, one workflow run, all artifacts.** The publisher pushes a
`vX.Y.Z` tag; CI does the rest. There are no manifest edits before
tagging — the source manifests are templates with placeholder
versions and a placeholder connector hash, and CI substitutes the
real values into build copies before signing and packing.

```sh
# Pick the next version, tag the current commit, push the tag.
git tag vX.Y.Z
git push origin vX.Y.Z
# Wait ~2 minutes. Done — connector + every action tarball is
# published at the per-FQN tag the install pipeline expects.
```

The source manifests carry two placeholders that CI binds at release
time:

- `version = "0.0.0-dev"` in `connector/manifest.toml` and every
  `actions/*/action.md` (also in each action's `source` URL and
  `[[requires.connectors]]` block). CI replaces `0.0.0-dev` with the
  version extracted from the pushed tag (`vX.Y.Z` → `X.Y.Z`).
- `hash = "sha256:bound-at-release"` in every action manifest's
  `[[requires.connectors]]` block. CI replaces this with the real
  content-addressed hash of the connector tarball after the connector
  is built.

The committed source intentionally keeps both placeholders. Each
release runs the same substitution against an unchanged template, so
the publisher does not hand-edit version fields and there is no
"bump to vX.Y.Z" commit per release.

What CI does on each `vX.Y.Z` push:

1. Substitutes `0.0.0-dev` with the tag's version across every
   manifest in the working tree.
2. Builds `connector.wasm` (wasip1).
3. Computes `sha256(connector.wasm || manifest.toml)` — the
   canonical-hash input from ADR-0004.
4. Signs the connector payload, packs `aileron.tar.gz`, publishes at
   tag `vX.Y.Z`.
5. For each `actions/*/action.md`: substitutes
   `sha256:bound-at-release` with the real connector hash, signs the
   substituted manifest, packs `aileron.tar.gz`, publishes at tag
   `actions/<name>/vX.Y.Z`.

Aileron's install pipeline resolves
`github://ALRubinger/aileron-connector-google@<ver>` to the connector
release's `aileron.tar.gz`, and
`github://ALRubinger/aileron-connector-google/actions/<name>@<ver>`
to the per-action release's `aileron.tar.gz`. All artifacts in a
version cohort share provenance — CI creates every per-action release
at the same commit the connector tag points to.

### What you see on the releases page

Each `vX.Y.Z` push produces one connector release plus one per-action
release, all from the same commit. The connector release (tagged
`vX.Y.Z`) carries the **Latest** badge; the per-action releases
(tagged `actions/<name>/vX.Y.Z`) are marked **Pre-release** so the
page anchors visually on the connector. Per-action tags are how
Aileron's resolver locates artifacts — they aren't subordinate
releases despite their tag prefix; they're sibling artifacts in the
same cohort, all pinned to the same connector content hash. The
release notes cross-link each release to its siblings so you can
navigate the cohort from any starting point.

This split is per-FQN-tag plumbing the resolver requires today
(see ADR-0004 in the Aileron repo). Future versions of Aileron's
Hub will present a single unified cohort view that hides the per-tag
shape; until then, the GitHub releases page is the raw form.

## Trusting this publisher

To install connectors from this repo, add the public key from
`keys/publisher.pub` to your local keyring:

```sh
# One-time setup per user:
mkdir -p ~/.aileron
cat keys/publisher.pub | jq -R --arg authority 'github://ALRubinger/aileron-connector-google' \
  '. as $key | {authorities: {($authority): {keys: [$key]}}}' \
  >> ~/.aileron/keyring.json
```

(Or merge into an existing keyring manually.) Without the public key in
the keyring, `aileron connector install` fails closed — see ADR-0004's
verification rules in the Aileron docs.

## OAuth setup (publisher side)

This connector ships with a Google OAuth Desktop-app `client_id`
registered by the publisher; users do not register their own apps.
Per ADR-0006 the runtime drives the OAuth dance via PKCE.

### `client_secret` is bound at release time

Google's "Desktop app" OAuth client type rejects token-exchange and
refresh requests that omit `client_secret` even with PKCE present
— a Google quirk, not a spec requirement. Per ADR-0002, the value
ships in the connector binary the same way `gcloud` and `gh` ship
their bundled secrets, but it is **never committed to this source
repo**: GitHub's secret scanner forwards Google client secrets to
Google, which auto-rotates them on detection.

The committed source manifest carries
`client_secret = "bound-at-release"` as a placeholder. The release
workflow substitutes it from the `GOOGLE_OAUTH_CLIENT_SECRET`
repository secret before signing and packing — same template
pattern as the connector content hash and the version. The bound
value lives only in the signed connector tarball.

Publisher one-time setup:

1. Google Cloud Console → APIs & Services → Credentials → OAuth
   client ID for the Desktop app → copy the **Client secret**.
2. Repo Settings → Secrets and variables → Actions → New
   repository secret. Name: `GOOGLE_OAUTH_CLIENT_SECRET`. Paste
   the value. Save. Done — never recorded anywhere else.

If the secret rotates (manually or because Google detected an
exposed value), the publisher updates the same Actions secret;
the next `vX.Y.Z` push picks up the new value automatically.

### Demo before verification: Google's "Testing" publishing status

Google's OAuth verification (required for production publishing of
"sensitive" or "restricted" scope apps) takes days for sensitive
scopes (Calendar) and weeks for restricted scopes (Gmail). To demo
the connector against real Google APIs *during* that review window,
keep the OAuth consent screen in **Testing** publishing status:

1. Google Cloud Console → APIs & Services → OAuth consent screen.
2. Publishing status: **Testing**.
3. Test users: add up to 100 Google account emails (yourself, anyone
   you're demoing to). Test users skip Google's verified-app gate but
   see an "unverified app" warning at consent — they click
   *Advanced → Go to <app> (unsafe)* to proceed.

In Testing mode the connector functions identically to production —
all the same scopes are issuable; the OAuth dance, capability
binding, and credential mediation in the Aileron runtime work
unchanged. **One catch:** refresh tokens issued in Testing mode
expire after 7 days. Test users redo the OAuth dance weekly via
`aileron binding rebind`.

When the verification submission clears, switch publishing status to
**In production**. Refresh tokens issued thereafter become long-lived
and the test-user list stops applying.

### Scope expansion

The verification tier is per-scope; the cost is per-tier, not
per-scope. Adding new scopes within an already-verified tier costs
zero additional verification time. The connector's eventual scope
set:

| Scope | Tier | Used by |
|---|---|---|
| `gmail.readonly` | Restricted | `list-recent-emails`, `get-email` |
| `gmail.compose` | Restricted | `draft-email`, `send-email`, `send-draft`, `get-draft`, `list-drafts` |
| `drive` | Restricted | `search-files`, `get-file-content`, `get-file-metadata`, `upload-file`, `rename-file`, `move-file` |
| `documents` | Restricted | `get-doc-structure`, `create-doc`, `update-doc` |
| `calendar.readonly` | Sensitive | `list-upcoming-events` |
| `calendar.events` | Sensitive | `create-calendar-event` |
| `contacts.readonly` | Sensitive | `search-contacts`, `get-contact`, `list-contacts` |

Submit the full scope list at once when registering the OAuth app, so
all five go through verification together. Restricted scopes also
require an annual CASA security re-review once verified — front-loading
the eventual scope list at registration is the cheapest path.

## License

Apache-2.0.
