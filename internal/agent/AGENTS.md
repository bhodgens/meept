# internal/agent/AGENTS.md

Guidance for AI agents working in `internal/agent/`. Referenced from the root
AGENTS.md; full chat-path contract prose moved here from the root file. Update
both in the same commit per the root maintenance rule.

## Chat replies must be honest and user-shaped

The chat path (sync dispatch and task-completion events) is the user's
only window into the daemon. These contracts were added after the
2026-09-04 naive-user comparison (docs/plans/chat-dispatch-ux/) and
are guarded by `scripts/e2e-naive-user-chat.sh`:

- **Turns are asynchronous; acks are immediate.** chat.submit acks a turn
  in milliseconds (turn_id + conversation_id); the result arrives via the
  turn.terminal bus event (Plan: docs/plans/20260916-async-turn-migration).
  The blocking `chat` RPC is a legacy opt-in (`orchestrator.
  sync_chat_enabled=true`, default false): task-dispatched turns then
  block up to the 110s sync-wait ceiling and may return the "still
  running" stub — `waitForTaskCompletion` (internal/agent/handler.go)
  returns the terminal step's `Result`, never the stub except when every
  step result is empty or the store errors. Under the default, no path
  can return the stub. Stalled async turns are reaped by the turn
  watchdog and surface as failed terminal events.
- **Errored steps never pass review.** `ReviewStep` gates on
  `stepHasError` before every policy path; a task with any failed step
  finalizes `StateFailed` and its `task.completed` payload carries
  `"status": "failed"` plus the error text as `result`.
- **Step jobs run in the session's directory.** `resolveStepWorkingDir`
  resolves WorktreePath > ProjectPath > session `DetectionContext.CWD`
  > "". Never fall back to the daemon's CWD (see also the os.Getwd
  rule in the root AGENTS.md).
- **Chat turns always resolve a working directory.** `session.ResolveWorkingDir`
  (internal/session/working_dir.go) is the single precedence for the
  session-bound sources — `WorktreePath > ProjectPath > DetectionContext.CWD`
  — and the chat path binds it at turn start in `ChatHandler.sessionLoop`,
  then falls to the configured default (`daemon.default_working_dir` via
  `SetDefaultWorkingDir`, unset today) and finally to the actionable
  `tools.ErrNoWorkingDir`.
- **Projects are scoped PER SESSION; there is NO global active-project
  fallback.** A session resolves its OWN binding only. `ProjectManager.GetActive`
  is never consulted at turn start or at dispatch — the
  `SetActiveProjectPathResolver` seam and the `WorkingDirFromActiveProject`
  source were REMOVED, not merely left unset. Every session carries its own
  project: session creation (both the services path and the RPC
  `session.create` path) binds the explicit `project_id`, else the client
  CWD (resolved into a project by `CreateOrResolve`), else leaves the
  session UNBOUND; an unbound session is logged at Warn and its filesystem
  tools return `tools.ErrNoWorkingDir`
  ("no working directory for this session; pass an explicit path") — a
  detectable sentinel (`tools.IsNoWorkingDir`), never the bare
  `no path specified` that made the model retry until the cycle guard
  aborted the turn (fresh-rig daemon11, 2026-09-13). Never synthesize a
  project (`EnsureDefault` is not for session binding) and never use the
  daemon's own CWD. The client detection context is PERSISTED
  (`detection_context` column; `Store.SetDetectionContext`) so a session
  created with `meept session create --cwd DIR` still resolves DIR at turn
  time after a daemon restart, from the store alone.
- **Machine-shaped output never becomes a reply.** `RunOnceWithParts`
  applies `applyReplyGuard` — raw `platform_*` tool dumps, agent
  rosters, and status JSON are replaced with user-language fallbacks.
- **Quota failures surface to the user.** Terminal
  `*llm.QuotaResetError` in a step job publishes the existing
  `agent.quota_wait` event and appends a user-language quota sentence
  to the stored step Result. Quota is still never an alias failure and
  never re-queued through the tactical retry gate.

## Output-filter gate order and retry caps

The post-step pipeline order is FROZEN: result → claim-vs-evidence marking →
output filter chain → evidence validation → ReviewStep → adversarial
verification — it never swaps (docs/workflows/output-filters.md). Filter
rejections consume `FilterRetryCount` (cap `max_filter_retries`) and never
validation retries, and vice versa. Every filter action logs
`stage=output_filter` with `action=pass|rewrite|fail|rejected_exhausted`.

## session_id vs conversation_id

`session_id` (primary key, e.g. `session-abc123`) and `conversation_id`
(internal, e.g. `conv-xyz789`) are distinct identifiers. The Flutter client
sends `session_id` as `conversation_id` in chat requests. WS subscriptions use
`session_id`. Bus events carry `conversation_id`. New event/filter/routing code
MUST handle both. The WS filter in `internal/comm/http/server.go` falls back
from `session_id` to `conversation_id` — preserve this.
