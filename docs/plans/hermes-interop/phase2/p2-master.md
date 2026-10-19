# Hermes Interop Phase 2 — Master Plan

Tree: `docs/plans/hermes-interop/phase2/` (p2-master.md + leaves p2-01..p2-05).
Phase 1 landed: client + config (90da68e8), 8-verb tool (1ed9948f), docs (edcfac1d).

## Scope (user directive 2026-10-18)

1. Approval relay (`POST /v1/runs/{id}/approval`).
2. SSE streaming (`GET /v1/runs/{id}/events`, `POST /api/sessions/{id}/chat/stream`).
3. Session continuity (`POST /api/sessions` create + `X-Hermes-Session-Key`).
4. Cron/jobs verbs (`/api/jobs` CRUD + pause/resume/run).
5. Capability-gated verbs via `/v1/capabilities` flags.

## Frozen contracts

### Contract P2-A — client REST extensions (leaf p2-01 owns)

```go
// internal/hermes/client.go (extend; keep existing methods byte-compatible)
func (c *Client) CreateSession(ctx, title string) (sessionID string, err error)
// POST /api/sessions {"title": title}; response carries the new id.

// ChatOptions/RunOptions gain: SessionKey string -> sent as
// X-Hermes-Session-Key header when non-empty.

func (c *Client) RunApproval(ctx, runID, decision, reason string) error
// POST /v1/runs/{runID}/approval. PIN THE BODY SHAPE FROM UPSTREAM SOURCE
// (github.com/NousResearch/hermes-agent gateway/platforms/_api_runs.py or
// api_server.py) BEFORE implementing; if unreachable, use
// {"decision": "approve"|"deny", "reason": ...}, mark TODO(upstream-shape)
// and report it.

type Job struct { ID, Schedule, Prompt, Status string /* + fields per upstream */ }
func (c *Client) ListJobs(ctx) ([]Job, error)              // GET /api/jobs
func (c *Client) CreateJob(ctx, j Job) (Job, error)        // POST /api/jobs
func (c *Client) GetJob(ctx, id) (Job, error)              // GET /api/jobs/{id}
func (c *Client) PauseJob(ctx, id) error                   // POST /api/jobs/{id}/pause
func (c *Client) ResumeJob(ctx, id) error                  // POST /api/jobs/{id}/resume
func (c *Client) RunJob(ctx, id) error                     // POST /api/jobs/{id}/run
```

Config knob (p2-01 owns, single config writer):
`HermesConfig.AutoApprove bool` (`auto_approve`, default false) + template
comment: "when false, the hermes tool's approve verb requires meept-side
user confirmation before forwarding a decision to the remote gateway."

### Contract P2-B — SSE reader (leaf p2-02 owns; NEW FILE ONLY)

```go
// internal/hermes/sse.go (new file; do NOT edit client.go)
type Event struct {
    Type string          // SSE event name ("assistant.delta", "tool.started",
                         // "run.completed", "run.failed", "run.cancelled", ...)
    Data json.RawMessage // raw payload
}
// StreamRunEvents subscribes to GET /v1/runs/{id}/events.
// StreamChat runs POST /api/sessions/{id}/chat/stream.
// Both return (<-chan Event, error); channel closes after the terminal
// run.* event or on ctx cancel. MUST skip ':'-prefixed keepalive comment
// lines (upstream emits them every 10s). Bearer header on both.
func (c *Client) StreamRunEvents(ctx, runID string) (<-chan Event, error)
func (c *Client) StreamChat(ctx, sessionID, input string, opts ChatOptions) (<-chan Event, error)
```

Same-package access to unexported client fields is allowed; name-ownership
split: p2-02 defines ONLY sse.go symbols (Event, StreamRunEvents,
StreamChat, internal sse helpers prefixed `sse`).

### Contract P2-C — tool verbs (leaf p2-03 owns hermes_agent.go)

- `approve` {run_id, decision(approve|deny), reason?}: when
  cfg.AutoApprove false -> return builtin.ConfirmationResponse phase-1
  shape (requires_confirmation, confirm_arg "confirmed"); on
  confirmed=true re-invoke forward to RunApproval. When true -> forward
  directly.
- `jobs` {action: list|get|create|pause|resume|run, ...args}.
- `chat`: empty session_id -> CreateSession first (restores phase-1
  Contract C), return the created id in the result. New optional arg
  `session_key` -> ChatOptions.SessionKey.
- `run`: optional `session_key` arg.
- Capability gating: before each mutating verb, check the cached
  Capabilities flags (run_steer, run_stop, run_approval_response,
  session_chat, session_fork...); flag false -> readable
  "hermes gateway does not advertise <feature>" error, no HTTP call.
  Read-only verbs stay ungated.
- IsReadOnly: approve=false, jobs=false (mutating actions) except
  action=list/get which are read-only — mark the TOOL as not read-only
  (conservative) and document per-action in the description.

### Contract P2-D — streaming in tool (leaf p2-04; AFTER p2-03, same file)

- `runs` gains `wait` (bool) + `collect_events` (bool): wait blocks on
  StreamRunEvents until terminal event (respect ctx), returns final
  status + collected tool-progress timeline (tool.started/completed
  names + run.* terminal). No wait -> existing poll behavior.
- `chat` gains `stream` (bool): true -> StreamChat, aggregate
  assistant.delta into final text, append tool timeline. Result shape
  identical to sync chat + "progress" array.
- Terminal-event mapping: run.completed -> success; run.failed ->
  error result with payload; run.cancelled -> "cancelled" result.

### Contract P2-E — docs (leaf p2-05; AFTER all)

Extend docs/workflows/hermes.md: new verbs table rows, approval security
model (auto_approve default false + confirmation flow), streaming usage,
jobs verbs, capability-gating behavior, session continuity
(session_key semantics + 409 foreign-session rule). Remove the v1
"chat requires session_id" limitation note (fixed by p2-03). Matrix row
touch-up only if text is now wrong.

## Waves

- Wave 1 (parallel, disjoint files): p2-01 (client.go + config) ∥ p2-02 (sse.go new file).
- Wave 2: p2-03 (hermes_agent.go verbs + gating) — needs p2-01 API.
- Wave 3: p2-04 (hermes_agent.go streaming) — needs p2-02 API + p2-03 file state. SERIAL after p2-03 (same file).
- Wave 4: p2-05 (docs).
- Integration gate: builds + go test -p 2 ./internal/hermes/ ./internal/config/ ./internal/tools/builtin/ ./internal/daemon/; tracking flip; close-out commit.

## Tracking table

| Leaf | Status | Commit | Notes |
|------|--------|--------|-------|
| p2-01 | COMPLETE 2026-10-19 | bcefdff9 (+test commit) | CreateSession (w/ model-lock opts), SessionKey header, RunApproval, Jobs CRUD, SetSessionModel; AutoApprove knob; orchestrator finished the consent-stalled tail |
| p2-02 | COMPLETE 2026-10-19 | 20b55ce3 | sse.go: StreamRunEvents + StreamChat, keepalive skip, 4MiB lines, timeout-less stream client; 9 tests -race |
| p2-03 | COMPLETE 2026-10-19 | 3dd4e7e9 | 12 verbs (approve two-phase, jobs CRUD, chat auto-create, session_key, create_session/set_model); gating on REAL upstream flags (run_submission, session_chat, run_steer/stop, run_approval_response, session_model_lock); jobs ungated (no upstream flag); NewHermesTool(client, autoApprove) |
| p2-04 | COMPLETE 2026-10-19 | 38d6cdf5 | runs wait/collect_events + chat stream; terminal mapping (completed/failed/cancelled); legacy paths pinned byte-identical; 13 tests -race |
| p2-05 | COMPLETE 2026-10-19 | e16e31c9 | hermes.md rewritten (12-verb table, gating, continuity, approval, streaming, jobs); matrix row fixed; sibling's uncommitted 10-verb edits absorbed; subagent commit was consent-denied, orchestrator committed after verification |

Tree COMPLETE 2026-10-19 — 5/5 leaves committed, 100%. Integration gate:
both binaries build; hermes/config/tools.builtin/daemon suites green
(-count=1). Phase-2 scope delivered in full: approval relay (two-phase
confirm, auto_approve default false), SSE streaming (runs wait +
collect_events, chat stream), session continuity (create-session,
session_key header), jobs verbs, capability gating on real upstream flags.

## Non-goals

- No bus-event relay of Hermes progress into meept WS clients (tool-result
  timeline only; live relay is a future tree).
- No admin_config_rw / memory_write_api (upstream flags are false).
- No ACP adapter.
