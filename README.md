# aileron-connector-google

Aileron connector for Google APIs — Gmail + Calendar read/write at v0.0.1.

This repo is the first reference connector for the Aileron action runtime
(see [github.com/ALRubinger/aileron](https://github.com/ALRubinger/aileron)).
It demonstrates the full publisher flow: WASM connector source, manifest
declaring sandboxed capabilities and OAuth provider config, action
templates with declared inputs, ed25519-signed release tarballs.

## What it ships

Four operations at v0.0.1, two read + two write, across Gmail and
Calendar:

| Action | Op | HTTP | Endpoint |
|---|---|---|---|
| `list-recent-emails` | `list_recent_emails` | GET | `gmail.googleapis.com/gmail/v1/users/me/messages` |
| `list-upcoming-events` | `list_upcoming_events` | GET | `www.googleapis.com/calendar/v3/calendars/{calendarId}/events` |
| `draft-email` | `draft_email` | POST | `gmail.googleapis.com/gmail/v1/users/me/drafts` |
| `create-calendar-event` | `create_calendar_event` | POST | `www.googleapis.com/calendar/v3/calendars/{calendarId}/events` |

All four run inside the Aileron WASM sandbox with `[capabilities.network]`
restricted to `gmail.googleapis.com:443` and `www.googleapis.com:443`.
The connector never holds OAuth tokens — Aileron's runtime resolves the
bound credential and injects `Authorization: Bearer <token>` host-side
when the connector marks an outbound HTTP request with
`credential: "oauth2"` (see ADR-0005 credential mediation in the Aileron
docs).

The two write ops are **not idempotent** — invoking them twice creates
duplicate drafts/events. Their action manifests declare
`[[execute]].idempotent = false` so the gateway's retry layer
(ADR-0010) does not double-write on transient failures.

`draft-email` deliberately creates drafts only — it does not send.
Sending stays the user's choice from Gmail's drafts folder. A
dedicated `send-email` action with explicit per-call approval is a
later v0.x consideration.

## Demo path

```sh
# Install the connector and an action.
aileron connector install github://ALRubinger/aileron-connector-google@0.0.1
aileron action add github://ALRubinger/aileron-connector-google/actions/list-recent-emails@0.0.1

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
│   └── list-upcoming-events/action.md
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
single `vX.Y.Z` tag; CI builds the connector, computes the content
hash, signs everything, and creates one connector release plus one
release per action — each at the per-FQN tag the Aileron install
pipeline expects.

```sh
# Bump version in connector/manifest.toml and every actions/*/action.md.
# CI validates that manifest versions match the tag and fails fast
# if they're out of sync, so this step is required.
sed -i '' 's/version = "0.0.1"/version = "0.0.2"/' connector/manifest.toml actions/*/action.md
sed -i '' 's|/aileron-connector-google@0.0.1|/aileron-connector-google@0.0.2|' actions/*/action.md
git commit -am "chore: bump to v0.0.2"

git tag v0.0.2
git push origin main v0.0.2
# Wait ~2 minutes. Done — connector + 4 actions all published.
```

What CI does on each `vX.Y.Z` push:

1. Validates every manifest's `version` field matches the tag.
2. Builds `connector.wasm` (wasip1).
3. Computes `sha256(connector.wasm || manifest.toml)` — the
   canonical-hash input from ADR-0004.
4. Signs the connector payload, packs `aileron.tar.gz`, publishes at
   tag `vX.Y.Z`.
5. For each `actions/*/action.md`: substitutes `sha256:bound-at-release`
   with the real connector hash, signs the substituted manifest, packs
   `aileron.tar.gz`, publishes at tag `actions/<name>/vX.Y.Z`.

The committed source manifests keep `sha256:bound-at-release` as a
permanent placeholder — they're release templates. Only the published
tarballs carry the real hash. Each release runs the same substitution
against the unchanged template, so version bumps are the only commits
the publisher hand-edits before tagging.

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

This connector ships with a Google OAuth Desktop app `client_id`
registered by the publisher; users do not register their own apps.
Per ADR-0006, the runtime drives the OAuth dance via PKCE so no
client secret is shipped or stored client-side. See the manifest's
`[capabilities.credential.oauth2]` block for the configured
authorize/token URLs and scopes.

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
| `gmail.readonly` | Restricted | `list-recent-emails` |
| `gmail.compose` | Restricted | `draft-email` (drafts only — not used for send at v0.0.1) |
| `calendar.readonly` | Sensitive | `list-upcoming-events` |
| `calendar.events` | Sensitive | `create-calendar-event` |

Submit the full scope list at once when registering the OAuth app, so
all four go through verification together. Restricted scopes also
require an annual CASA security re-review once verified — front-loading
the eventual scope list at registration is the cheapest path.

## License

Apache-2.0.
