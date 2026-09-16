# Async Turn Migration (Plan 2 of 2) - Implementation Orchestrator

> **For the executing agent:** You are the orchestrator for this tree node.
> Your job: (1) dispatch implementation agents, (2) review their work,
> (3) re-dispatch if incomplete, (4) track completion.
> Do NOT implement code yourself. All implementation happens in leaf agents.

## Meta

- **Role:** Root
- **Parent:** none (root)
- **Children:** 7 leaf documents under this node
- **Scope:** Migrate chat turn consumption from blocking sync RPC to submit-ack + terminal-event everywhere: daemon RPC mode, bench client, CLI, TUI, GUI, liveness watchdog, sync-wait deprecation.

## Goal

Plan 1 (`20260916-turn-lifecycle-events/`) gave every turn a canonical
`turn.terminal` event. This plan completes the architecture: agent
interaction becomes asynchronous by default. A client submits work, receives
an immediate acknowledgement (turn id + conversation id), and consumes the
terminal event when the work completes. No server-side task-wait, no 110s
stub, no static workload-relative timeouts. The only timeout is a liveness
watchdog: "no progress event for N seconds" — workload-independent.

User-visible outcome: timely and contextually correct task/agent information
in the TUI and GUI — progress as it happens, results when they land, honest
error and park states, no blank waits, no stale stubs.

## Architecture

Three layers change:

1. **Daemon (Go):** new `chat.submit` RPC method — validates, creates the
   turn record, publishes `chat.request` with a turn id, returns the ack in
   milliseconds. Existing `chat` RPC stays (deprecated, still works) so the
   migration is per-client. Terminal events already flow via Plan 1.
2. **Clients:** each surface (bench, CLI, TUI, GUI) gains a submit+await
   client that submits, then awaits `turn.terminal` (RPC bus subscribe or
   WS for the GUI), rendering progress from existing `agent_progress`
   events. Sync `chat` calls are replaced per surface, not flag-day.
3. **Liveness:** a per-turn watchdog client-side (and one daemon-side
   turn-reaper for orphaned turns): "no `agent_progress`/`turn.terminal`
   event for N seconds" → surface a stalled state; daemon reaper cancels
   and emits `status=failed` for turns that vanished between ack and
   dispatch.

Execution order is strict: 01 (daemon) first; then 02-05 (clients) in
parallel; 06 (watchdog) after 01; 07 (deprecation) last, only after all
clients stop depending on sync work-replies.

## Dependency On Plan 1

This plan REQUIRES `20260916-turn-lifecycle-events` COMPLETE (the
`turn.terminal` topic, its frozen payload, and the WS `agent_progress`
classification). Every leaf here consumes that contract.

## Interface Contracts

### Contract 1: `chat.submit` RPC request/response

```
// File: internal/rpc/chat_submit.go (new; registered alongside "chat")
//
// Request params (map[string]any):
//   message         string  required, non-empty after trim
//   session_id      string  optional
//   conversation_id string  optional (generated when empty)
//   agent_id        string  optional agent override
//   source_client   string  optional
//   parts           []llm.ContentPart optional (JSON-shaped)
//   model           string  optional one-shot model override
//
// Response (IMMEDIATE — never waits on agent work):
// {
//   "turn_id":        "uuid",           // NEW primary handle
//   "conversation_id": "...",
//   "session_id":     "...",
//   "accepted":       true,
//   "note":           ""                 // human-readable when accepted=false
// }
// accepted=false cases: empty message (note explains), session store
// unavailable. HTTP-mapped errors stay 4xx-shaped; never 5xx-on-slow.
//
// Owner: 01-async-rpc-mode.md
// Consumers: 02-bench-client, 03-cli-client, 04-tui-client, 05-gui-client
```

### Contract 2: turn registry (daemon-side, for orphan reaping)

```
// File: internal/agent/turn_registry.go (new)
//
// type TurnRegistry struct { ... }        // mutex + map[turnID]turnRecord
// type turnRecord struct {
//     TurnID         string
//     ConversationID string
//     TaskID         string              // set once a task is dispatched
//     SubmittedAt    time.Time
//     LastProgressAt time.Time           // updated on agent_progress publish
// }
//
// func NewTurnRegistry(logger *slog.Logger) *TurnRegistry
// func (r *TurnRegistry) Register(turnID, conversationID string)
// func (r *TurnRegistry) AttachTask(turnID, taskID string)
// func (r *TurnRegistry) Touch(turnID string)              // progress heartbeat
// func (r *TurnRegistry) Complete(turnID string)           // remove + return record
// func (r *TurnRegistry) Stale(olderThan time.Duration) []turnRecord
//
// Wired in daemon components; ChatHandler touches on worker events;
// the reaper goroutine (06-liveness-watchdog.md) polls Stale().
```

### Contract 3: progress heartbeat wiring

```
// ChatHandler.publishWorkerEvent (handler.go:1341) additionally calls
// h.turnRegistry.Touch(turnID) — Worker gains a turnID field set at
// handleRequest time. Every worker lifecycle event refreshes liveness.
// Owner: 01-async-rpc-mode.md (field+plumbing), 06-liveness-watchdog.md
// (reaper consumption).
```

### Contract 4: client await loop shape (all clients)

```
// All four clients implement the SAME logical loop (language-idiomatic):
// 1. resp := submit(params)            // chat.submit or HTTP /api/v1/chat/submit
// 2. subscribe: turn.terminal filtered to resp.turn_id
// 3. render agent_progress events for resp.conversation_id while waiting
// 4. on terminal event: return Reply/Status/Error to the caller
// 5. liveness: no event of any kind for LivenessTimeout → return
//    ErrTurnStalled (client decides retry/prompt)
// LivenessTimeout default 120s (client config where one exists).
```

### Contract 5: HTTP async endpoint (GUI path)

```
// internal/comm/http: POST /api/v1/chat/submit
// Body: same JSON as /api/v1/chat. Response: 200 + Contract 1 ack JSON.
// The GUI submits here, then consumes the WS turn.terminal
// (agent_progress-classified) for the result. Existing /api/v1/chat is
// unchanged until 07.
// Owner: 01-async-rpc-mode.md
```

### Contract 6: bench Row extension

```
// meept-bench internal/results Row gains:
//   AckSeconds  float64 `json:"ack_seconds,omitempty"`  // submit→ack
//   TurnSeconds float64 `json:"turn_seconds,omitempty"` // ack→terminal
// WallSeconds remains submit→terminal-total. Diff gate keeps comparing
// WallSeconds; ack/turn are additive observability.
// Owner: 02-bench-client.md
```

## Child Document Index

| # | Document | Type | Dependencies | Est. Context | Concurrency |
|---|----------|------|-------------|-------------|-------------|
| 01 | 01-async-rpc-mode.md | leaf | Plan 1 complete | ~70K | A |
| 02 | 02-bench-client.md | leaf | 01 | ~45K | B |
| 03 | 03-cli-client.md | leaf | 01 | ~40K | B |
| 04 | 04-tui-client.md | leaf | 01 | ~55K | B |
| 05 | 05-gui-client.md | leaf | 01 | ~60K | B |
| 06 | 06-liveness-watchdog.md | leaf | 01 | ~50K | C |
| 07 | 07-sync-deprecation.md | leaf | 02,03,04,05,06 | ~40K | D |

## Dispatch Protocol

### Phase 1: Group A — daemon

Dispatch 01-async-rpc-mode.md. Context: full leaf + Contracts 1-3 + INLINED
current source of internal/rpc/proxy.go (RegisterProxyMethods + makeProxy),
internal/agent/handler.go (ChatRequest/ChatResponse, handleRequest head,
publishChatMessage), internal/comm/http server chat route. Include the
no-commit and no-read_file rules. Review in-session: Contract 1 response
shape exact, ack latency (no waiting anywhere on the submit path), existing
`chat` untouched, tests -race green. Commit when APPROVED.

### Phase 2: Group B — four clients in parallel

Dispatch 02-05 together (batch of 4; if provider rate limits hit, fall back
to batches of 2 in order 02,03 then 04,05). Each context: its leaf +
Contracts 1, 4 (+5 for GUI) + the INLINED Plan-1 TurnTerminalEvent struct +
relevant current client source inlined. Review each in-session; commit per
leaf.

### Phase 3: Group C — watchdog

Dispatch 06 after 01 lands. Review: reaper emits failed terminal events for
stale turns; Touch wiring covers worker events; tests inject time.

### Phase 4: Group D — deprecation

Dispatch 07 ONLY after 02-06 all REVIEWED. Review: config default keeps
sync ON; all four clients verifiably use submit; bench gate still green.

### Review/commit loop

Standard: review in-session, re-dispatch on gaps (max 3 cycles), commit per
leaf with exact paths, update the tracking table after every transition.

## Review Checklist

Per child: tasks implemented, contracts satisfied exactly, tests -race green
(-race n/a for Dart: `flutter analyze` + widget tests), no scope creep, no
debug artifacts, no line-number corruption, no commit by the implementer.

Cross-child (integration):
- [ ] All four clients speak Contract 1 and Contract 4's loop
- [ ] Bench Row carries ack/turn seconds; gate runs green end-to-end
- [ ] Stale-turn reaper emits valid Plan-1 payloads
- [ ] `chat` (sync) still works until 07 flips the default
- [ ] AGENTS.md updated: WS/turn invariants + sync-deprecation note (07)

## Coding Conventions

- Go: stdlib+existing deps; errors wrapped %w; two-value map asserts; id.Generate
  for ids; nil-guarded Set*; table-driven -race tests; gofmt+vet clean.
- Dart: flutter analyze clean; no new deps without orchestrator approval;
  widget tests for new widgets; follow existing websocket_service patterns.
- Bench Go: mirror meept Go conventions; keep client stdlib-only.
- All: NEVER time.Now().UnixNano() for ids; never read_file→write_file
  (line-number corruption); write once and stop.

## Completion Tracking Table

| Child | Status | Iterations | Review Notes |
|-------|--------|------------|-------------|
| 01-async-rpc-mode | PENDING | 0 | |
| 02-bench-client | PENDING | 0 | |
| 03-cli-client | PENDING | 0 | |
| 04-tui-client | PENDING | 0 | |
| 05-gui-client | PENDING | 0 | |
| 06-liveness-watchdog | PENDING | 0 | |
| 07-sync-deprecation | PENDING | 0 | |

Status values: PENDING | IN_PROGRESS | IMPLEMENTED | REVIEWED | COMPLETE | BLOCKED

## Integration Test Plan

1. `go build ./... && go vet ./...` clean; `go test -race -count=1 ./internal/... -p 2` green.
2. Bench repo: `go build ./... && go test ./...` green.
3. Live end-to-end (daemon + new binary): bench suite run → rows carry
   ack_seconds/turn_seconds; file-write task PASSES its file_contains check
   (the original #46-adjacent wall-clock failure is gone: no 110s stub).
4. TUI: submit a task-shaped message → progress indicators update live →
   result bubble renders from the terminal event; kill the daemon mid-turn →
   liveness message appears.
5. GUI: same via WS; late errors render on the originating conversation.
6. CLI: oneshot `meept chat "..."` returns the real result (not a stub) for a
   >110s task; `--await off` prints the ack line.
7. Reaper test: inject a stale turn; confirm failed terminal event emitted.
8. `make graphs-check` fresh; AGENTS.md sections updated.

## Structural Completeness Check (Before Dispatch)

```
python3 ~/.hermes/skills/software-development/hierarchical-planning/scripts/check_template_compliance.py docs/plans/20260916-async-turn-migration --strict-leaves
```

Re-run until `ALL TREES COMPLIANT: True`.

## Notes

- The AGENTS.md invariant "sync replies carry the real step result" is
  rewritten by 07 in the same commit that flips the default (repo rule:
  stale agent guidance updated in the same commit, not argued later).
- Idempotency: chat.submit retries carry the same client-generated turn_id;
  the registry dedupes (Register on existing id returns the record — 01
  implements, clients pass turn ids on retry where the transport allows).
- Reattempt safety: the watchdog never blind-retries; it surfaces stalled
  state. Cancellation is future work, noted in 07's non-goals.
- GUI leaves use the existing websocket_service agent_progress filtering
  (websocket_service.dart:646-663) — no new WS machinery.
- meept-bench is a SEPARATE repo (~/git/meept-bench). Leaf 02 works there;
  commit policy differs: orchestrator commits in both repos, explicit paths.
