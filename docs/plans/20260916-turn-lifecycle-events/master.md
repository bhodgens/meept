# Turn Lifecycle Events (Plan 1 of 2) - Implementation Orchestrator

> **For the executing agent:** You are the orchestrator for this tree node.
> Your job: (1) dispatch implementation agents, (2) review their work,
> (3) re-dispatch if incomplete, (4) track completion.
> Do NOT implement code yourself. All implementation happens in leaf agents.

## Meta

- **Role:** Root
- **Parent:** none (root)
- **Children:** 1 leaf document under this node
- **Scope:** Emit one canonical, structured `turn.terminal` bus event carrying every turn's final result, so clients can consume turn outcomes as events instead of only via the blocking chat RPC.

## Goal

Every meept turn (inline chat, task dispatch, quota-parked, errored) currently
reaches its caller through exactly one channel: the synchronous
`chat` RPC reply. If that reply carries the "Task X is still running" stub
(internal/agent/handler.go:1939, the 110s sync-wait ceiling), the result is
gone as far as the caller is concerned — it exists only inside
`waitForTaskCompletion`'s scope and is never re-broadcast.

This plan adds the missing primitive: a **canonical terminal event** published
on the message bus for EVERY turn, carrying the final reply text, terminal
status, and full provenance. It is deliberately the smallest slice of the
async-turn migration (sibling plan `20260916-async-turn-migration/`): it
changes no existing behavior, breaks no client, and is immediately useful —
the TUI, GUI, and bench can all start consuming it before any RPC contract
changes.

This is the "highest-value first step" because every later async-migration
step (ack-based chat RPC, client event loops, liveness watchdog, sync
deprecation) consumes this event. It is also independently useful: it closes
the gap where a user whose turn hit the 110s sync ceiling never learns the
task's actual result unless they happen to be watching `task.completed` on
the right surface.

## Architecture

The daemon's `ChatHandler` (internal/agent/handler.go) is the single point
where every turn reaches a terminal state: the `handleRequest` switch assigns
each path a `handlerCase` and produces a reply string. Today that reply goes
exactly two places: the `chat.response` RPC topic and (for message-shaped
paths) the `chat_message` WS push via `publishChatMessage`.

The design: **one new bus topic, `turn.terminal`, published from a single
helper** called on every exit path of `handleRequest` (and on the async
`task.completed`/`task.failed` relays in ChatHandler), carrying a frozen
JSON payload. The WS layer classifies it as `agent_progress` (it is a
lifecycle event, never a chat bubble — AGENTS.md WS classification
invariant). The TUI subscribes via its existing `bus.subscribe` event
stream (internal/tui/events.go). The bench can subscribe the same way
(it already does this for `chat_message` fallback, internal/daemonclient/
daemonclient.go:272).

No RPC contract changes. No client is forced to change. The sync chat path
keeps working byte-identically.

## Interface Contracts

### Contract 1: `turn.terminal` bus topic payload

```
// Topic: "turn.terminal"
// Publisher: ChatHandler (internal/agent/handler.go) via a single helper.
// Message type: models.MessageTypeEvent, source "chat-handler".
//
// Frozen JSON payload (map[string]any keys — must match EXACTLY):

type TurnTerminalEvent struct {
	ConversationID string `json:"conversation_id"` // required, non-empty
	SessionID      string `json:"session_id,omitempty"`
	TurnID         string `json:"turn_id"`         // uuid, one per chat request
	TaskID         string `json:"task_id,omitempty"`   // set when the turn dispatched a task
	IntentType     string `json:"intent_type,omitempty"`
	AgentID        string `json:"agent_id,omitempty"`  // agent that handled the turn
	HandlerCase    string `json:"handler_case"`        // same string as dispatch_log handler_case
	Status         string `json:"status"`              // "completed" | "failed" | "timeout" | "parked"
	Reply          string `json:"reply"`               // the final user-facing reply text (stub included if that is what the user got)
	DurationMS     int64  `json:"duration_ms"`         // request-to-terminal wall time
	ClassifiedBy   string `json:"classified_by,omitempty"` // intent.Method provenance
	Model          string `json:"model,omitempty"`         // model that served the turn, if any
	Error          string `json:"error,omitempty"`         // non-empty iff Status=="failed"
}

// Owner: 01-terminal-event.md
// Consumers: TUI event stream, Flutter GUI (WS agent_progress relay),
// meept-bench (future), sibling plan 20260916-async-turn-migration in full.
```

Status vocabulary is CLOSED: `completed`, `failed`, `timeout`, `parked`.
`timeout` means the turn hit a wait ceiling (e.g. the 110s sync wait) and the
reply may be a stub — consumers can detect stub replies via this status
instead of string-matching "is still running".

### Contract 2: WS classification (internal/comm/http/server.go)

```
// In transformBusEventToWS: add a case BEFORE the default, alongside the
// existing agent.progress cases:
//
//   case strings.HasPrefix(topic, "turn."):
//       eventType = "agent_progress"
//
// Rationale (AGENTS.md WS classification invariant): turn.terminal carries
// lifecycle state, not a chat message. A chat bubble would render blank.
// The Flutter GUI renders agent_progress events as progress indicators on
// the matching session.
//
// Owner: 01-terminal-event.md
// Consumers: Flutter GUI websocket_service.dart
```

### Contract 3: helper signature

```
// File: internal/agent/handler.go (new method on ChatHandler)
//
// publishTurnTerminal is the ONLY sanctioned way to emit turn.terminal.
// Single construction point guarantees the payload contract; callers pass
// the pieces they have.
func (h *ChatHandler) publishTurnTerminal(ev TurnTerminalEvent)

// The struct lives in internal/agent/handler.go next to ChatRequest/
// ChatResponse (same file, same style).
```

## Child Document Index

| # | Document | Type | Dependencies | Est. Context | Concurrency |
|---|----------|------|-------------|-------------|-------------|
| 01 | 01-terminal-event.md | leaf | none | ~45K | A |

Single leaf: the work is one cohesive Go subsystem slice (one new file-sized
unit + targeted handler edits + WS case + tests). Splitting it further would
split `handleRequest` exit paths across documents — the wrong seam.

## Dispatch Protocol

### Phase 1: Dispatch Concurrency Group A

1. **Read** 01-terminal-event.md and dispatch via `delegate_task`:
   - Goal: "Implement all tasks from 01-terminal-event.md"
   - Context: Full leaf document text + Interface Contracts from this
     orchestrator + Coding Conventions below + INLINED current source of:
     internal/agent/handler.go lines 615-900 (handleRequest switch),
     1341-1360 (publishWorkerEvent), 1860-1990 (SetSyncMode,
     waitForTaskCompletion, handleTaskCompleted); internal/comm/http/server.go
     lines 679-740 (transformBusEventToWS). Instruct: "Do NOT commit. Do NOT
     run git add. Write code, run tests, report results only."
   - Include: "Do NOT use read_file on existing source files — explore with
     search_files or terminal cat instead. If you read a file, never feed its
     output into write_file. After writing a file, do NOT read it back."
   - Agent follows TDD per the leaf's instructions.

### Phase 2: Review and Commit

The orchestrator reviews in-session (main model, never a delegated reviewer —
children inherit delegation.model):

1. Verify every `handleRequest` exit path calls `publishTurnTerminal` exactly
   once: grep `publishTurnTerminal(` and map each call site to a handler case;
   the count must equal the number of terminal paths (inline replies, sync
   dispatch, async ack, task-completed relay, task-failed relay, error
   returns, budget-blocked).
2. Verify the payload contract: run the leaf's tests; they must pin every
   frozen key. Check Status vocabulary is the closed 4-value set.
3. Verify the WS case: `turn.` prefix → `agent_progress`, placed before
   default classification. Confirm no path classifies it `chat_message`.
4. Run: `go build ./... && go test -race ./internal/agent/ ./internal/comm/http/ -count=1`
5. If gaps: re-dispatch with specific findings (max 3 cycles).
6. If pass: `git add internal/agent/handler.go internal/agent/handler_terminal_event_test.go internal/comm/http/server.go && git commit -m "feat(agent): canonical turn.terminal event on every chat turn exit path (async-turn-migration step 1)"` — update tracking table to REVIEWED.

### Phase 3: Integration Review

1. Run the full suite: `go test -race -count=1 ./internal/...` with
   `TEST_PACKAGE_PARALLELISM=2` (AGENTS.md: unbounded parallelism exhausts
   ephemeral ports on macOS).
2. Live smoke (daemon must be running): send one inline chat ("what time is
   it?") and one task-shaped chat; confirm via
   `sqlite3 ~/.meept/metrics.db` + a `bus.subscribe turn.terminal` probe
   (or the TUI) that exactly one `turn.terminal` event per turn arrives with
   `status=completed` and the real reply text.
3. Verify `make graphs` output still fresh: `make graphs-check` — if the
   generator picks up the new topic, commit the regenerated
   docs/generated/* in the same commit series.
4. Report COMPLETE.

## Review Checklist

- [ ] All tasks from 01-terminal-event.md implemented
- [ ] `turn.terminal` payload keys match Contract 1 EXACTLY (no extra, no missing)
- [ ] Every `handleRequest` terminal path emits exactly one event (map call sites to cases)
- [ ] `task.completed` / `task.failed` relays emit with `task_id` set and the task's real result
- [ ] WS classification: `turn.` → `agent_progress`, never `chat_message`
- [ ] Status vocabulary closed: completed|failed|timeout|parked
- [ ] Zero behavior change to existing RPC replies (byte-identical chat response path)
- [ ] Tests table-driven, `-race` clean, pinned to the payload contract
- [ ] No scope creep: no client changes, no RPC contract changes, no config knob
- [ ] No debug artifacts, TODOs, or line-number corruption
- [ ] `make graphs-check` passes (or regenerated docs committed)

Output: APPROVED or list of specific gaps.

## Coding Conventions

- **Language:** Go (module github.com/caimlas/meept), stdlib + existing deps only
- **Naming:** exported PascalCase, unexported camelCase; event struct fields match JSON tags
- **Error handling:** wrap with `%w`; never swallow; two-value map assertions on bus payloads
- **Bus messages:** `models.NewBusMessage(models.MessageTypeEvent, "chat-handler", payload)`; publish via `h.bus.Publish` (or `PublishExternalOnly` if mirroring publishWorkerEvent)
- **Testing:** table-driven, `-race` clean, no sleeps — use channels/synchronization
- **Formatting:** `gofmt -w` before reporting; `go vet ./...` clean
- **Typed-nil guard:** any new Set* method nil-guards (project invariant)
- **ID generation:** `pkg/id.Generate()`, never `time.Now().UnixNano()` (predid analyzer)

## Completion Tracking Table

| Child | Status | Iterations | Review Notes |
|-------|--------|------------|-------------|
| 01-terminal-event | PENDING | 0 | |

Status values: PENDING | IN_PROGRESS | IMPLEMENTED | REVIEWED | COMPLETE | BLOCKED

## Integration Test Plan

1. `go build ./...` — clean.
2. `go test -race -count=1 ./internal/agent/ ./internal/comm/http/` — green,
   including the new TestTurnTerminalEvent_* suite.
3. `go test -race -count=1 ./internal/... -p 2` — no cross-package regression.
4. Live smoke against a running daemon:
   - Inline turn: `meept chat "what time is it?"` → exactly one `turn.terminal`
     with `status=completed`, `handler_case` matching dispatch_log, reply text
     equal to the CLI-printed reply.
   - Task turn: a prompt that dispatches a task (e.g. "create a file named
     turn-smoke.txt containing hello") → first a `turn.terminal` with the ack
     (or, under bench-style sync dispatch, the terminal task result), then on
     task completion a second event with `task_id` set and `status=completed`.
   - Stub path: with `syncWaitCeiling` forced low in a test, the event carries
     `status=timeout` and the stub text — consumers can detect it without
     string matching.
5. `make graphs-check` — fresh.

## Structural Completeness Check (Before Dispatch)

Run after authoring, before dispatch:

```
python3 ~/.hermes/skills/software-development/hierarchical-planning/scripts/check_template_compliance.py docs/plans/20260916-turn-lifecycle-events --strict-leaves
```

Re-run until `ALL TREES COMPLIANT: True`.

## Notes

- This tree is deliberately tiny (one leaf). Its sibling,
  `20260916-async-turn-migration/`, is the large one and CONSUMES the event
  this tree produces. Execute this tree first.
- AGENTS.md maintenance: the WS classification section gains one line
  (`turn.` prefix → agent_progress). Add it in the integration commit.
- The bus-topic registration must be checked against
  `scripts/gen-connectivity-graph.py` orphan analysis: `turn.terminal` has a
  live publisher by construction; if the generator flags it as an orphan
  subscriber-less topic, that is expected (subscribers are external) and may
  need an ANNOTATED_ORPHANS entry per the existing pattern.
- Related prior art in-tree: `publishWorkerEvent` (handler.go:1341) already
  publishes lifecycle events to `chat.progress` — the new helper follows the
  same pattern with a frozen payload struct instead of ad-hoc keys.
