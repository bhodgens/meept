# Agent Invariants: Projects & Working Directories

Cross-package invariants about session project binding and working-directory
resolution. Referenced from the root AGENTS.md. Update both in the same
commit per the root maintenance rule.

## Daemon CWD is NOT the user's project

The daemon process's working directory is wherever it was launched from (often
the meept repo itself). It is NEVER the user's project directory.

- **Never use `os.Getwd()` as a project/working-directory fallback** in daemon
  code (`internal/`, `pkg/`). The CLI client (`cmd/meept/`) and TUI
  (`internal/tui/`) may use `os.Getwd()` because they run in the user's shell.
- Tools receive their working directory per-session via
  `tools.ContextWithWorkingDir` or `SetWorkingDir`, not from the daemon's CWD.
- `generate_image` / `generate_video` write under `media.output_dir`
  (`~/.meept/media`) or a relative `output_path` resolved against the session
  working dir. Never `os.Getwd()`.
- Image and video models are `provider/id` entries in `models.json5` with
  capability `image` or `video`. Slots: `image_model`, `video_model`. Do not
  add a second provider catalog.
- The `json_extract` tool runs a dedicated extraction model via the
  `extract_model` slot (`provider/id` ref, typically a small local llama.cpp
  endpoint such as `local-extract/lfm2-extract`). Empty slot = tool reports
  not-configured; never fall back to the chat model.
- Session creation binds PER SESSION: the explicit `project_id`, else the
  client CWD resolved into a project by `CreateOrResolve`, else the session
  stays UNBOUND. Never call `ProjectManager.GetActive()` for session binding
  (no global active-project fallback) and never `EnsureDefault()` — it
  creates a synthetic empty git repo.

## Multi-user is opt-in; RPC stays owner-trusted

`multiuser.enabled` (default **false**) gates per-user key auth. When off:
the daemon never constructs `internal/auth.Store`, HTTP auth uses the legacy
flat-key path byte-identically, and sessions have no owner — every visible
session belongs to everyone. Code touching auth, session ownership, or
identity MUST preserve the disabled-path behavior exactly.

Two trust boundaries, never mix them:

- **HTTP/WS** (`transport.http`): bearer-key identity. In multi-user mode,
  keys map to `*auth.Identity` via `IdentityFromContext`; expired/unknown
  keys get 418. Sessions created through it are owner-scoped.
- **Unix RPC** (`transport.rpc`): kernel-enforced same-OS-user only (0600
  socket, optional SO_PEERCRED/LOCAL_PEERCRED UID allowlist). It has NO
  user identity by design — RPC callers act as the daemon owner. Do not add
  token auth to the socket path and do not expose the socket over a network.

Cluster user pooling syncs users over gossip events
(`USERS_SYNC` in `internal/backup/usersync.go`); foreign users' lifecycle
belongs to peer sync — local code must not delete them.
