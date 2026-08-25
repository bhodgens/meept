# Change Review (Pending Changes + Journal)

Human-facing surfaces for reviewing staged file changes before they are
applied, and for inspecting/reverting changes after they were applied. This
closes the parity-audit gap "no user-facing diff surface": humans no longer
need to ask the agent to see or resolve pending changes.

The underlying mechanics are described in
[adversarial-input-defense.md](adversarial-input-defense.md) (write staging,
pre-image hashes, drift guard) and [change-journal.md](change-journal.md)
(journal storage and revert semantics). This page documents the review
surfaces.

## How it works

1. **Stage** — `file_edit`, `write_file`, and `stage_write` register a
   pending change (original content + SHA256 pre-image hash + unified diff)
   instead of writing directly.
2. **Review** — a human opens the review surface (TUI `ctrl+d`, Flutter
   ChangesPanel, or the HTTP API) and sees the list of staged changes with
   diffs.
3. **Accept or reject** — accepting applies the change through the *shared
   accept path* (`ResolveTool.AcceptChange`): fence re-validation, pre-image
   drift check, write, journal record. Rejecting drops the staged change and
   leaves the file untouched. Every surface (tool, TUI, HTTP, Flutter) calls
   the same code path, so accept semantics can never diverge.
4. **Revert** — applied changes are journaled; list them and revert to the
   pre-image (guarded by the three-way checksum, see
   [change-journal.md](change-journal.md)) via the CLI, HTTP, or RPC.

Drift guard (all surfaces): if the on-disk file changed after staging,
accept refuses with `file changed since staging` (HTTP `409`) and keeps the
change staged for re-staging against the current content.

## HTTP API

All routes live on the daemon HTTP server under `/api/v1/*` and inherit the
server's API-key auth middleware. Enabled with the REST API
(`transport.http.rest`, default on); wired via `WithChangesAPI`.

| Method | Route | Success | Errors |
|--------|-------|---------|--------|
| GET | `/api/v1/sessions/{sid}/pending-changes` | `[{id, file_path, diff, created_at, expires_at}]` | `503` if the changes API is not wired |
| POST | `/api/v1/pending-changes/{id}/accept` | `{"status":"applied"}` | `409` drift, `404` unknown id |
| POST | `/api/v1/pending-changes/{id}/reject` | `{"status":"rejected"}` | `404` unknown id |
| GET | `/api/v1/changes/journal?session=<sid>&limit=<n>` | `[{id, session_id, file_path, post_sha, applied_at, change_ids, pre_image_size}]` | `503` if the journal is disabled |
| POST | `/api/v1/changes/journal/{id}/revert` | `{"restored_path":"<path>"}` | `409` drift, `400` size-capped / pre-image missing, `404` unknown id, `403` fence refused |

Notes:

- The pending-changes list streams **diffs only** — never full file bodies.
- The journal list **never returns pre-image bytes**; only `pre_image_size`
  travels, so clients can show a revertable/size column cheaply.
- A malformed or negative `limit` falls back to the journal default (100).

Example session:

```bash
KEY="***"
# What is awaiting review in this session?
curl -s -H "Authorization: Bearer ***" \
  http://127.0.0.1:8081/api/v1/sessions/session-abc/pending-changes | jq

# Approve one staged change.
curl -s -X POST -H "Authorization: Bearer ***" \
  http://127.0.0.1:8081/api/v1/pending-changes/stage-123/accept

# Discard it instead.
curl -s -X POST -H "Authorization: Bearer ***" \
  http://127.0.0.1:8081/api/v1/pending-changes/stage-123/reject

# Applied changes (newest first).
curl -s -H "Authorization: Bearer ***" \
  "http://127.0.0.1:8081/api/v1/changes/journal?session=session-abc&limit=20" | jq

# Undo one applied change.
curl -s -X POST -H "Authorization: Bearer ***" \
  http://127.0.0.1:8081/api/v1/changes/journal/change-456/revert
```

## TUI

Open the pending changes modal with **`ctrl+d`** (requires an active
session). The status bar shows `<n> pending changes (ctrl+d)` while any
changes await review (polled every 10 seconds and refreshed after each
action).

Modal keys (all strings lowercase):

| Key | Action |
|-----|--------|
| `j` / `k` | navigate the list (also `up`/`down`) |
| `v` | toggle the full diff view (j/k scroll inside it) |
| `a` | accept the selected change |
| `r` | reject the selected change |
| `esc` | leave diff view, or close the modal |

After an accept/reject the list refreshes automatically and the status bar
shows `change accepted` / `change rejected`, or the daemon error (e.g. the
drift message) on failure. The TUI calls the daemon over its RPC socket
(`changes.list` / `changes.accept` / `changes.reject`), which dispatches to
the same shared accept path as the HTTP routes.

## Flutter GUI

The ChangesPanel mirrors the TUI surface (list cards with diff bodies,
accept/reject buttons) by calling the same HTTP endpoints above through the
existing API service layer. See `ui/flutter_ui` for the implementation; the
contract it consumes is the HTTP table above.

## CLI

The CLI covers the journal side (see `meept changes --help`):

```bash
meept changes list                 # applied changes + revertable flag
meept changes revert change-456    # restore the pre-image
```

Pending changes on the CLI are resolved through the agent's `resolve` tool
(accept/reject by change ID or `all`), which shares the same accept path.

## Edge cases

- **Drift on accept**: refused with the short pre-image/current hashes in
  the message; the change stays staged. Re-stage against the current file.
- **Drift on revert**: refused with `changed since apply`; the file is left
  untouched.
- **Size-capped journal entries** (>1 MiB pre-image): listed with
  `pre_image_size = 0` but not revertible (HTTP `400`).
- **Journal disabled** (database failed to open at daemon start): journal
  routes answer `503`; staging/accept/reject still work without revert
  history.
- **Legacy staged changes** (no pre-image hash, mid-upgrade): accept proceeds
  with a warning log, matching the resolve tool.
