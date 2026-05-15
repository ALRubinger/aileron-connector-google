+++
name = "move-file"
# `version` and the `0.0.0-dev` markers in `source` and the
# `[[requires.connectors]]` block are placeholders. CI substitutes
# them with the real version (from the pushed tag) into a build copy
# of this manifest before signing and packing. Source stays template;
# only the published tarball carries the real version.
version = "0.0.0-dev"
source = "github://ALRubinger/aileron-connector-google/actions/move-file@0.0.0-dev"

[[requires.connectors]]
name = "github://ALRubinger/aileron-connector-google"
version = "0.0.0-dev"
# `hash` is the connector tarball's content-addressed identity per
# ADR-0002. CI substitutes this placeholder with the real hash at
# release time (see .github/workflows/release.yml). The committed
# source intentionally keeps the placeholder so each release runs the
# same substitution against an unchanged template.
hash = "sha256:bound-at-release"
capabilities = ["move_file"]

[match]
intent = "move a Google Drive file to a different folder"

[[execute]]
id = "move"
connector = "github://ALRubinger/aileron-connector-google"
op = "move_file"
idempotent = true

# Per-call approval gate. Moving a file between folders changes its
# folder-inherited sharing permissions — collaborators in the source
# folder may lose access; collaborators in the destination folder may
# gain it. That side effect is third-party-observable in the same way
# send-email is observable: once a collaborator's access list changes
# on Drive's side, the change is visible (and may be acted on)
# regardless of whether the file is later "moved back". Reversibility
# at the connector level is not enough; the act of granting / revoking
# access has already occurred. See ADR-0009 (user channel — agent in
# trust path).
#
# The approval prompt would otherwise show only opaque file_id /
# add_parents / remove_parents ids, which gives the user no signal
# about what they are about to approve. The [approval.preview] block
# below tells the runtime to call the read-only get_file_metadata op
# against Drive *before* showing the prompt, so the user sees the
# authoritative file name and current parents pulled straight from
# the same source move_file will write to. Agent-supplied previews
# are deliberately not used (ADR-0009: agent is never in the trust
# path); see ADR-0016 for the preview directive's contract.
[approval]
required = true

[approval.preview]
op = "get_file_metadata"
args = { file_id = "${args.file_id}" }

[approval.preview.render]
Name           = "name"
Type           = "mimeType"
CurrentParents = "parents"

[[inputs]]
name = "file_id"
type = "string"
description = "Drive file id to move."
required = true
label = "File ID"

[[inputs]]
name = "add_parents"
type = "string"
description = "Comma-separated parent folder id(s) to add. Typically a single destination folder id, e.g. \"1newFolderId\"."
required = true
label = "Move to (folder ids)"

[[inputs]]
name = "remove_parents"
type = "string"
description = "Optional comma-separated parent folder id(s) to remove — typically the file's current parent. Omit to add the destination as an additional parent without unparenting from the source (multi-parent semantics)."
required = false
label = "Move from (folder ids)"
+++

# Move a Google Drive File

Moves a Drive file between folders by changing its parent(s). The
operation works in terms of Drive's "parents" field — a file is a
child of one or more folders; moving means adding a new parent and
(usually) removing the current one. Pass `add_parents` for the
destination and `remove_parents` for the source. Omitting
`remove_parents` makes the file a multi-parent child of both folders
rather than moving it.

When it fires:
- "move the Q2 plan to the Archive folder"
- "file this in the Quarterly Reports folder"
- "tidy these into the Resolved folder"

This action is **gated on per-call user approval.** When the agent
calls `move_file`, the Aileron runtime pauses the call and asks the
user to approve via the launch-comms channel (CLI prompt or the
webapp `/approvals` surface). Drive's `files.update` endpoint is not
contacted until approval is granted. On denial the call returns an
error to the agent and is recorded in the audit log; no move is
performed and no quota is burned. See ADR-0009 (user channel — agent
in trust path) for the rationale.

The approval prompt surfaces an **authoritative preview** fetched
from Drive at approval time — not from the agent. Before showing
the prompt, the runtime invokes `get_file_metadata` (an idempotent
read-only op on the same connector) against the supplied `file_id`
and renders the authoritative file name, mimeType, and current
parents on the approval card. The preview output is shown only to
the user; it is never returned to the agent's context. If the fetch
fails (e.g., the agent passed an id that does not exist or the user
no longer has access), the prompt renders "preview unavailable:
`<reason>`" and the user can deny on the spot. See ADR-0016
(approval-time preview fetch) for the directive's contract,
validation rules, and failure modes.

Why approval (when other write actions in this connector aren't
gated): moving a file between folders changes its inherited
permissions. A file in a folder shared with team-A picks up team-A's
access; moving it to team-B's folder revokes team-A's access and
grants team-B's. Even though the move is "reversible" in the sense
that you can move it back, the access change is third-party-
observable — collaborators may have already opened, copied, or
linked from the file during the window. That makes move-file more
like send-email (observable side effect) than like update-doc
(reversible via revisions). The decision matrix:

| Action       | Reversible? | Third-party-observable? | Approval? |
|--------------|-------------|-------------------------|-----------|
| update-doc   | yes (revisions) | no (private edit)   | no        |
| rename-file  | yes (rename back) | no                | no        |
| move-file    | yes (move back) | **yes** (permissions) | **yes** |
| send-email   | no            | yes                   | yes       |

**Idempotent in effect.** Re-running with the same `add_parents` /
`remove_parents` arguments leaves Drive in the same state. The
manifest sets `idempotent = true` so the gateway retry layer
(ADR-0010) may safely re-issue on transient failure between the
approval grant and the API call.

The connector runs in the Aileron WASM sandbox with
`[capabilities.network]` restricted to `www.googleapis.com:443`, and
the OAuth bearer token is injected host-side at the network boundary
— the connector code never sees the token. Uses the `drive` scope.
See ADR-0005 (sandbox + credential mediation) and ADR-0009 (user
channel — agent in trust path) in the Aileron docs.
