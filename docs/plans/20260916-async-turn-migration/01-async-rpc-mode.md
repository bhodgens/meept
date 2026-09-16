# Async Chat RPC Mode (Daemon) - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit — the orchestrator
> handles all git operations after review. Do NOT use read_file on existing
> source files — explore with search_files or terminal cat. After writing
> a file, do NOT read it back to verify — write once and stop. After
> completing, report what you built, what files you touched, and any
> deviations from the spec.

## Meta

- **Parent:** ../master.md
- **Scope:** Add the `chat.submit` RPC method (immediate ack), the turn registry, and the HTTP `/api/v1/chat/submit` endpoint. Zero change to the existing `chat` method.
- **Dependencies:** Plan 1 (`20260916-turn-lifecycle-events`) COMPLETE — the `turn.terminal` topic and `TurnTerminalEvent` struct exist.
- **Estimated Context:** ~70K
- **Concurrency Group:** A

## Goal

Give clients an asynchronous entry point: submit a chat turn, get an ack
(turn id + conversation id) in milliseconds, and let the existing
`chat.request` → ChatHandler pipeline do the work. The result reaches
clients via Plan 1's `turn.terminal` event. The existing blocking `chat`
RPC is untouched — deprecation is leaf 07, not this leaf.

## Context

Today the `chat` RPC (internal/rpc/proxy.go:54,
`p.makeProxy("chat.request", "chat.response", 120*time.Second)`) pairs a
request topic with a response topic and blocks up to 120s. ChatHandler
subscribes `chat.request` (handler.go:248), runs the turn, and
`sendResponse` publishes `chat.response`.

`handleRequest` already supports an async-dispatch path (handler.go:837-868:
"send ack immediately, let orchestrator handle it") but only when
`ShouldDispatchAsync(result)` is true — the ack comes AFTER classification,
and inline turns still block. `chat.submit` is different: it acks BEFORE
any agent work, always.

Idempotency primitive: `internal/effects` is the existing external-effect
idempotency ledger; this leaf builds a lighter in-memory registry
(TurnRegistry, master Contract 2) — sufficient because the registry only
needs to survive the daemon process; a restart orphans turns that the
watchdog leaf (06) reaps as failed.

Key files:
- internal/rpc/proxy.go — RegisterProxyMethods, makeProxy (the pattern for
  request/response over the bus; chat.submit is a DIRECT handler, not a
  proxy: it publishes chat.request and returns without waiting)
- internal/rpc/server.go — RegisterHandler, handler signature
- internal/agent/handler.go — ChatRequest struct, handleRequest entry,
  sendResponse; the worker map
- internal/comm/http/ — the /api/v1/chat route (GUI parity endpoint lives
  here; find it via search for "/api/v1/chat")
- pkg/id — Generate()

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
// 1. New RPC method "chat.submit" (registered in a new
//    internal/rpc/chat_submit.go, registered from the same place "chat"
//    is registered):
//    params: message (required), session_id, conversation_id, agent_id,
//            source_client, parts, model — all optional except message.
//    response: {"turn_id","conversation_id","session_id","accepted","note"}
//    NEVER blocks on agent work. Target latency: <50ms.
//
// 2. internal/agent/turn_registry.go (new):
//    type TurnRegistry struct{...}; NewTurnRegistry(logger) *TurnRegistry
//    (r) Register(turnID, conversationID string) (existing bool)
//    (r) AttachTask(turnID, taskID string)
//    (r) Touch(turnID string)
//    (r) Complete(turnID string)
//    (r) Stale(olderThan time.Duration) []TurnRecord  // TurnRecord exported
//    type TurnRecord struct{ TurnID, ConversationID, TaskID string; SubmittedAt, LastProgressAt time.Time }
//    Concurrency-safe (mutex); Register on an existing turnID returns
//    existing=true WITHOUT overwriting (idempotent retry dedupe).
//
// 3. ChatHandler wiring: handleRequest accepts a turn_id (new field on
//    ChatRequest: TurnID string `json:"turn_id,omitempty"`); the handler
//    Touch()es the registry on every worker lifecycle event
//    (publishWorkerEvent gains the call); Complete() is called where
//    Plan 1's publishTurnTerminal fires.
//
// 4. HTTP: POST /api/v1/chat/submit → same ack JSON (501-style pass-through
//    to the RPC handler via the existing HTTP-RPC bridge; if the bridge
//    pattern doesn't support generic methods, register the route with a
//    thin handler that calls the same Go function the RPC handler calls).
```

### What This Leaf Consumes

```
// Plan 1: TurnTerminalEvent, publishTurnTerminal (handler.go) — Complete()
// is called adjacent to that emission point.
// internal/bus: Publish/Subscribe. pkg/id: Generate().
```

## Tasks

### Task 1: TurnRegistry

**Objective:** Concurrency-safe turn registry with idempotent Register.

**Files:**
- Create: `internal/agent/turn_registry.go`
- Test: `internal/agent/turn_registry_test.go`

**Step 1: Failing tests** — table-driven: Register/Get-returns-existing,
AttachTask, Touch updates LastProgressAt (inject clock via a
`now func() time.Time` field), Complete removes+returns, Stale filters by
LastProgressAt, concurrent Register/Touch/Complete from 16 goroutines
(-race), Register-twice-same-id is idempotent (second returns existing=true,
record preserved).

**Step 2:** verify fail. **Step 3:** implement per contract (mutex-guarded
map; `now` injectable, default time.Now). **Step 4:** pass.

### Task 2: chat.submit RPC handler

**Objective:** Direct RPC handler that validates, registers, publishes
chat.request (with TurnID), returns the ack. No waiting.

**Files:**
- Create: `internal/rpc/chat_submit.go`
- Modify: the file registering proxy methods (proxy.go RegisterProxyMethods
  call site) — one line registering the new handler
- Test: `internal/rpc/chat_submit_test.go`

**Step 1: Failing tests**

- Happy path: handler with a mock bus → returns accepted=true, turn_id and
  conversation_id non-empty (conversation generated when omitted), and a
  chat.request message was published whose payload carries `turn_id`.
- Empty/whitespace message → accepted=false, note non-empty, NO publish.
- Idempotent retry: same explicit turn_id twice → same turn_id returned,
  existing=true, ONE chat.request published.
- Registry unavailable/nil → handler still works (registry optional in
  handler struct; skip Touch/Complete when nil).

**Steps 2-4:** standard cycle. Implementation notes: the handler publishes
`chat.request` with a ChatRequest-shaped payload (import internal/agent? NO
— rpc must not import agent (check import direction; agent imports metrics,
rpc is standalone). Publish a `map[string]any` payload with the same JSON
keys as agent.ChatRequest — the wire contract is the JSON, exactly as
makeProxy already passes opaque payloads). Response construction is
synchronous.

### Task 3: ChatHandler consumes turn_id + Touch wiring

**Objective:** ChatRequest gains TurnID; handler touches/registers/
completes the registry around the turn.

**Files:**
- Modify: `internal/agent/handler.go` (ChatRequest struct; handleRequest
  head; publishWorkerEvent; the Plan-1 publishTurnTerminal call sites)
- Test: extend `internal/agent/handler_terminal_event_test.go`

**Step 1: Failing tests**

- handleRequest with ChatRequest.TurnID set → registry Contains turnID
  after start; Touch observed after a worker event fires; after the turn's
  publishTurnTerminal, registry no longer contains it (Complete).
- TurnID empty (legacy `chat` path) → registry untouched (legacy turns are
  NOT tracked; only explicit-submit turns are reapable).

**Steps 2-4:** standard cycle. The registry is a field on ChatHandler set
via `SetTurnRegistry(*TurnRegistry)` — nil-guarded (typed-nil too, project
invariant), tests use the setter.

### Task 4: HTTP /api/v1/chat/submit

**Objective:** GUI-parity endpoint returning the same ack JSON.

**Files:**
- Modify: the internal/comm/http route registration file that serves
  /api/v1/chat (locate via search_files)
- Test: `internal/comm/http/chat_submit_http_test.go`

**Step 1: Failing tests** — POST valid body → 200 + ack JSON with turn_id;
POST empty message → 200 + accepted=false + note (or 400 if the existing
/api/v1/chat uses 400 for validation — MATCH the existing convention);
GET → 405. If the HTTP layer reaches handlers via an RPC bridge, reuse it
("chat.submit" method call) — do not duplicate validation logic; if it
can't, extract the validation+ack into a small shared func in
internal/rpc/chat_submit.go and call it from both.

**Steps 2-4:** standard cycle.

### Task 5: registry → reaper seam (data only)

**Objective:** Expose Stale() with a stable shape for leaf 06 (no reaper
here — 06 owns the goroutine).

**Files:**
- Modify: `internal/agent/turn_registry.go` (only if Task 1 didn't already
  cover Stale; otherwise this task is a no-op verification)
- Test: included in Task 1's table

**Verify:** `go test -race ./internal/agent/ ./internal/rpc/ ./internal/comm/http/ -count=1`.

## Self-Verification Checklist

- [ ] All tasks implemented; `go build ./...` clean; gofmt/vet clean
- [ ] chat.submit never blocks on agent work (ack path has no bus-subscribe)
- [ ] Existing `chat` RPC byte-identical (existing proxy tests untouched pass)
- [ ] Registry idempotent on duplicate turn_id; -race clean under 16 goroutines
- [ ] ChatRequest.TurnID optional; legacy path untracked
- [ ] HTTP endpoint matches existing route conventions
- [ ] No line-number corruption; no TODOs/debug prints
- [ ] Wire payload keys for chat.request unchanged (existing consumer parses it)

**DO NOT COMMIT.** Orchestrator commits after review.

**Deviations from spec:** [none / list any with rationale]

## Review Checklist (For Review Agent)

- [ ] All tasks + tests present, passing under -race
- [ ] Contract 1 (ack JSON) and Contract 2 (registry API) match exactly
- [ ] ack latency: no subscribe/wait anywhere on the submit path (read the handler top-to-bottom)
- [ ] Import direction respected (rpc does not import agent)
- [ ] Wire JSON keys for chat.request unchanged; TurnID additive
- [ ] HTTP endpoint follows existing route/auth conventions
- [ ] No scope creep (no reaper, no client changes, no sync changes)

Output: APPROVED or specific gaps with file+line.

## Notes

- The rpc→agent import check matters: if internal/rpc importing
  internal/agent would create a cycle (agent → ... → rpc), keep payloads as
  map[string]any in the rpc layer and let agent.ChatRequest stay the
  consumer-side shape. The wire contract is the JSON keys.
- Do NOT modify the 120s proxy timeout or the `chat` method registration.
- Touch() must be cheap (mutex + map write) — it fires per worker event.
