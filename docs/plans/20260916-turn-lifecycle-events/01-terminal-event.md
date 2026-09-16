# Turn Terminal Event Emission - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit — the orchestrator
> handles all git operations after review. Do NOT use read_file on existing
> source files — explore with search_files or terminal cat. After writing
> a file, do NOT read it back to verify — write once and stop. After
> completing, report what you built, what files you touched, and any
> deviations from the spec.

## Meta

- **Parent:** ../master.md
- **Scope:** Publish a canonical `turn.terminal` bus event on every chat turn's terminal path and classify it as `agent_progress` on the WS relay.
- **Dependencies:** none
- **Estimated Context:** ~45K (exploration ~12K, generation ~15K, iteration ~8K, overhead ~10K)
- **Concurrency Group:** A
- **Audit references:** async-turn-migration step 1 (sibling plan 20260916-async-turn-migration/master.md §Dependency On This Tree)

## Goal

Every chat turn reaches a terminal state inside `ChatHandler.handleRequest`
(internal/agent/handler.go). Today the reply travels only to the synchronous
`chat.response` RPC topic. When the 110s sync-wait ceiling fires
(`waitForTaskCompletion`, handler.go:1939), the caller receives a
"Task X is still running" stub and the eventual real result is never
re-broadcast to that caller's conversation.

This leaf adds `turn.terminal`: a single, frozen-payload bus event published
from ONE helper called on every terminal exit path, so any subscriber (TUI
event stream, Flutter GUI over WS, meept-bench) can learn every turn's final
outcome with provenance. It changes zero existing behavior: the RPC reply
path is untouched; the event is purely additive.

## Context

meept is a Go daemon (module github.com/caimlas/meept) where chat turns flow:
RPC `chat` → `internal/rpc/proxy.go` (makeProxy, 120s cap) → bus topic
`chat.request` → `ChatHandler` (internal/agent/handler.go, subscribed at
line 248) → dispatcher/loop → reply published to `chat.response` + relayed
to WS as `chat_message`.

`ChatHandler.handleRequest` (handler.go:615) is a large switch assigning
`handlerCase` strings ("sync_dispatch", "async_dispatch", "route_to_agent",
"budget_blocked", "skill_execution", ...) and building a reply string. A
separate relay exists: `handleTaskCompleted` (handler.go:1369) subscribes to
`task.completed` and pushes the task's result into the conversation as a new
chat message via `sendResponse` — but only as a chat message, not a
structured, consumable event.

The WS layer (internal/comm/http/server.go, `transformBusEventToWS`:679)
maps bus topics to frontend event types with a prefix-classification switch.
AGENTS.md invariant: only `chat_message`/`chat.message.received` produce
`chat_message` events; lifecycle topics MUST classify `agent_progress`
(blank bubbles otherwise).

The TUI consumes bus events through `internal/tui/events.go` (bus.subscribe +
poll loop) and already handles `task.completed`/`task.failed`
(internal/tui/app.go:1495).

Key files:
- internal/agent/handler.go — ChatHandler, handleRequest, waitForTaskCompletion, publishWorkerEvent (the pattern to mirror), handleTaskCompleted
- internal/agent/handler.go:187-231 — ChatRequest/ChatResponse structs (style reference for the new event struct)
- internal/comm/http/server.go:679-740 — transformBusEventToWS classification switch
- internal/daemon/events.go:226 — PublishTaskNotification (existing notification path, NOT to be confused with this one)
- internal/tui/events.go — TUI bus.subscribe consumer (no change needed; new topic flows if subscribed — see Task 5)

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
// File: internal/agent/handler.go (new types + method on ChatHandler)

// TurnTerminalEvent is the frozen payload published on the "turn.terminal"
// bus topic when a chat turn reaches its terminal state. Field set is
// CLOSED: add nothing, remove nothing, rename nothing without updating
// every consumer (TUI, GUI, bench) and the contract in
// docs/plans/20260916-turn-lifecycle-events/master.md.
type TurnTerminalEvent struct {
	ConversationID string `json:"conversation_id"` // required, non-empty
	SessionID      string `json:"session_id,omitempty"`
	TurnID         string `json:"turn_id"`                 // uuid per chat request
	TaskID         string `json:"task_id,omitempty"`       // set iff the turn dispatched a task
	IntentType     string `json:"intent_type,omitempty"`
	AgentID        string `json:"agent_id,omitempty"`
	HandlerCase    string `json:"handler_case"`            // matches dispatch_log handler_case values
	Status         string `json:"status"`                  // completed | failed | timeout | parked  (CLOSED set)
	Reply          string `json:"reply"`                   // final user-facing reply text (stub included if that is what was returned)
	DurationMS     int64  `json:"duration_ms"`
	ClassifiedBy   string `json:"classified_by,omitempty"` // intent.Method provenance
	Model          string `json:"model,omitempty"`
	Error          string `json:"error,omitempty"`         // non-empty iff Status=="failed"
}

// publishTurnTerminal is the ONLY sanctioned emission point for the topic.
// It constructs the bus message (models.MessageTypeEvent, source
// "chat-handler"), marshals the event, and publishes on "turn.terminal".
// A nil bus is a no-op (test seam). It must never panic and never block.
func (h *ChatHandler) publishTurnTerminal(ev TurnTerminalEvent)
```

### What This Leaf Consumes

```
// Existing (no change):
// internal/bus — MessageBus.Publish / PublishExternalOnly
// internal/models — NewBusMessage(MessageTypeEvent, source, payload)
// internal/comm/http — transformBusEventToWS (modified in Task 4)
// pkg/id — Generate() for turn_id (NEVER time.Now().UnixNano — predid)
```

## Tasks

### Task 1: TurnTerminalEvent struct + publishTurnTerminal helper

**Objective:** Add the frozen payload struct and the single emission helper.

**Files:**
- Modify: `internal/agent/handler.go` (add after ChatResponse struct, ~line 231)
- Test: `internal/agent/handler_terminal_event_test.go` (create)

**Step 1: Write failing test**

```go
func TestPublishTurnTerminal_PublishesFrozenPayload(t *testing.T) {
	h := newTestChatHandlerWithBus(t) // helper: ChatHandler with a real bus; see existing handler tests for the pattern

	sub := h.bus.Subscribe("test-turn-terminal", "turn.terminal")
	defer func() { _ = sub.Unsubscribe() }()

	h.publishTurnTerminal(TurnTerminalEvent{
		ConversationID: "conv-1",
		TurnID:         "turn-1",
		TaskID:         "task-1",
		IntentType:     "code",
		AgentID:        "coder",
		HandlerCase:    "sync_dispatch",
		Status:         "completed",
		Reply:          "done",
		DurationMS:     1500,
		ClassifiedBy:   "llm",
		Model:          "local/lfm-8b-q4",
	})

	select {
	case msg := <-sub.Channel():
		if msg.Type != models.MessageTypeEvent {
			t.Errorf("type = %v, want MessageTypeEvent", msg.Type)
		}
		if msg.Source != "chat-handler" {
			t.Errorf("source = %q, want chat-handler", msg.Source)
		}
		var got map[string]any
		if err := json.Unmarshal(msg.Payload, &got); err != nil {
			t.Fatalf("payload unmarshal: %v", err)
		}
		for _, key := range []string{"conversation_id", "turn_id", "task_id",
			"intent_type", "agent_id", "handler_case", "status", "reply",
			"duration_ms", "classified_by", "model", "session_id", "error"} {
			if _, ok := got[key]; !ok {
				t.Errorf("payload missing key %q", key)
			}
		}
		if len(got) != 13 {
			t.Errorf("payload has %d keys, want 13 (frozen contract)", len(got))
		}
		if got["status"] != "completed" {
			t.Errorf("status = %v", got["status"])
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no turn.terminal event received")
	}
}

func TestPublishTurnTerminal_NilBusNoOp(t *testing.T) {
	h := &ChatHandler{logger: testLogger()} // bus nil
	h.publishTurnTerminal(TurnTerminalEvent{ConversationID: "c", TurnID: "t", Status: "completed"})
	// must not panic
}
```

(Adapt `newTestChatHandlerWithBus` to the existing test-helper patterns in
internal/agent/handler_test.go / dispatcher_test.go — do not invent a new
convention.)

**Step 2: Run test to verify failure**

Run: `go test ./internal/agent/ -run TestPublishTurnTerminal -v`
Expected: FAIL — TurnTerminalEvent/publishTurnTerminal undefined.

**Step 3: Write minimal implementation**

Add the struct (contract above) and:

```go
// publishTurnTerminal is the single emission point for the turn.terminal
// topic (async-turn-migration step 1). Payload contract is frozen — see
// docs/plans/20260916-turn-lifecycle-events/master.md Interface Contracts.
func (h *ChatHandler) publishTurnTerminal(ev TurnTerminalEvent) {
	if h.bus == nil {
		return
	}
	msg, err := models.NewBusMessage(models.MessageTypeEvent, "chat-handler", ev)
	if err != nil {
		h.logger.Error("failed to build turn.terminal event", "error", err)
		return
	}
	h.bus.Publish("turn.terminal", msg)
}
```

**Step 4: Run test to verify pass**

Run: `go test ./internal/agent/ -run TestPublishTurnTerminal -v`
Expected: PASS

### Task 2: Emit on every handleRequest terminal path

**Objective:** One `publishTurnTerminal` call per terminal exit of
`handleRequest`, with the reply/handlerCase each path produced.

**Files:**
- Modify: `internal/agent/handler.go` (handleRequest, ~lines 615-900; and its error return paths)
- Test: `internal/agent/handler_terminal_event_test.go`

**Step 1: Write failing test**

```go
func TestHandleRequest_EmitsTurnTerminalOnInlineReply(t *testing.T) {
	// Build a ChatHandler whose dispatcher classifies a simple chat input
	// (reuse the existing inline-chat test wiring; e.g. the short-message
	// guard path needs no classifier at all — a 2-word greeting suffices).
	h := newInlineChatTestHandler(t) // existing pattern
	sub := h.bus.Subscribe("test-turn-terminal", "turn.terminal")
	defer func() { _ = sub.Unsubscribe() }()

	h.handleRequest(&models.BusMessage{ID: "m1", Payload: mustJSON(t, ChatRequest{
		Message:        "hi",
		ConversationID: "conv-it",
	})})

	select {
	case msg := <-sub.Channel():
		var ev TurnTerminalEvent
		if err := json.Unmarshal(msg.Payload, &ev); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if ev.ConversationID != "conv-it" || ev.Status != "completed" {
			t.Fatalf("event = %+v", ev)
		}
		if ev.TurnID == "" {
			t.Error("turn_id must be set")
		}
		if ev.DurationMS < 0 {
			t.Errorf("duration_ms = %d, want >= 0", ev.DurationMS)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no turn.terminal event")
	}
}

func TestHandleRequest_EmitsTurnTerminalOnError(t *testing.T) {
	// Force the nil-result error path (handlerCase "nil_result_error"):
	// dispatch returns no actionable result. Reuse the existing
	// dispatcher_error test wiring.
	// Assert: exactly one event, Status=="failed", Error non-empty,
	// Reply documents the failure for the user.
}
```

**Step 2: Run to verify failure.** Expected: FAIL — no events published.

**Step 3: Implementation approach**

- At the TOP of handleRequest: `turnID := id.Generate()` and
  `start := time.Now()`; carry both through the function.
- At the BOTTOM (single funnel where the reply is sent — the same place
  `sendResponse` is invoked), emit one event using the accumulated
  handlerCase, reply, intent provenance, and `Status: "completed"`.
- For ERROR returns (there are several `return` sites that send an error
  response): emit with `Status: "failed"`, `Error` set. Prefer refactoring
  each error site to set local `reply`/`errStr` variables and fall through
  to the single funnel; if a site cannot fall through safely, emit inline —
  but the test must prove NO path emits twice and NO path emits zero times.
- Sync-dispatch path (`waitForTaskCompletion`): after the wait resolves,
  emit with the reply it produced. If the reply is the still-running stub,
  use `Status: "timeout"`; the stub text stays in `Reply`.
- `waitForTaskCompletion` itself is NOT modified — the caller inspects its
  return value (`strings.Contains(reply, "is still running")` is FORBIDDEN;
  instead have waitForTaskCompletion's caller detect the ceiling: check the
  task state via `h.taskStore.GetByID` — `StateFailed`/pending-after-ceiling
  maps to "timeout").
- Async-ack path: emit with `Status: "completed"` (the ack IS the terminal
  state of the RPC turn), `task_id` set, reply = ack text. The eventual task
  result arrives via Task 3's relay event.
- Guard against double emission: a package-level test asserts the count of
  `turn.terminal` events == 1 for each of: inline, error, async-ack,
  sync-completed, sync-timeout. If a natural funnel is impossible, add
  `turnTerminalEmitted bool` local + emit-once wrapper.

**Step 4: Run to verify pass.** Also:
`go test ./internal/agent/ -run TestHandleRequest -count=1` (existing suite)
must stay green — zero behavior change to replies.

### Task 3: Relay task.completed / task.failed as terminal events

**Objective:** When a task finishes AFTER its originating RPC turn already
returned (async ack or sync timeout), emit a `turn.terminal` carrying the
real result with the original conversation/turn ids.

**Files:**
- Modify: `internal/agent/handler.go` (handleTaskCompleted ~1369, and the
  task.failed subscription alongside the task.completed one at ~254)
- Test: `internal/agent/handler_terminal_event_test.go`

**Step 1: Write failing test**

```go
func TestHandleTaskCompleted_EmitsTerminalEventWithResult(t *testing.T) {
	// Wire ChatHandler, subscribe to turn.terminal, publish a task.completed
	// bus event whose payload carries task_id, linked_sessions (one session
	// mapping to conv-async), result summary, status "completed".
	// Assert: one turn.terminal event with Status=="completed",
	// TaskID set, Reply containing the result summary text,
	// HandlerCase=="task_completed_relay".
}
```

Note on correlation: `task.completed` payloads carry `linked_sessions`
(tactical.go:1632) — the relay must map session→conversation using the same
mapping the handler already maintains for workers (see the Worker map and
`sendResponse`'s conversation handling). If a task has no linked session/
conversation (headless), emit with `ConversationID` = the task's originating
conversation recorded at dispatch time; if genuinely unknown, emit with the
task_id as `conversation_id` prefix `task:<id>` — consumers key on task_id.

**Steps 2-4:** standard TDD cycle. Also mirror for `task.failed` →
`Status: "failed"`.

### Task 4: WS classification

**Objective:** `turn.` topics classify as `agent_progress` on the WS relay.

**Files:**
- Modify: `internal/comm/http/server.go` (transformBusEventToWS switch, ~line 696)
- Test: `internal/comm/http/server_turn_events_test.go` (create, or extend the existing transform test file)

**Step 1: Write failing test**

```go
func TestTransformBusEventToWS_TurnTerminalIsAgentProgress(t *testing.T) {
	msg := &models.BusMessage{Topic: "turn.terminal", Payload: mustJSON(t, map[string]any{
		"conversation_id": "c1", "turn_id": "t1", "status": "completed", "reply": "x",
	})}
	out := transformBusEventToWS(msg)
	if out["type"] != "agent_progress" {
		t.Errorf("type = %v, want agent_progress", out["type"])
	}
}
```

**Steps 2-4:** standard cycle. Implementation: add
`case strings.HasPrefix(topic, "turn."):` → `eventType = "agent_progress"`
with a comment citing this plan and the AGENTS.md invariant. Placement:
before the default case, next to the agent.quota case.

### Task 5: TUI subscription (one-line topics list)

**Objective:** The TUI event stream subscribes to `turn.terminal`.

**Files:**
- Modify: `internal/tui/events.go` (the `es.topics` list) — find the topics
  slice; add "turn.terminal".
- Modify: `internal/tui/handlers/task_events.go` (or the app.go event
  switch at app.go:1495): on a `turn.terminal` event with
  `status=="timeout"` and a stub reply, update the matching conversation's
  pending-indicator with "task still running — result will arrive" (minimal;
  full surfacing is the sibling migration plan's scope).
- Test: extend the events/topics test if one exists; otherwise a
  table-driven test asserting the topics list contains "turn.terminal" and a
  handler unit test for the timeout-status branch.

**Verify:** `go test ./internal/tui/ -count=1`.

## Self-Verification Checklist

Before reporting completion, verify:

- [ ] All 5 tasks implemented; `go build ./...` clean
- [ ] `go test -race ./internal/agent/ ./internal/comm/http/ ./internal/tui/ -count=1` green
- [ ] Payload contract: 13 keys, exact names (Task 1's key-count assertion)
- [ ] Every handleRequest terminal path emits exactly once (map call sites in the report)
- [ ] Status vocabulary closed: completed|failed|timeout|parked — grep for any other literal
- [ ] No existing reply byte changed (existing handler tests untouched expectations pass)
- [ ] No string-matching of "is still running" anywhere new (the stub is detected via task state)
- [ ] gofmt clean; go vet clean; no TODO/debug prints
- [ ] No line-number corruption (`grep -rcE '^\s+[0-9]+\|' internal/agent/ internal/comm/ internal/tui/` = 0)

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

**Deviations from spec:** [none / list any with rationale]

## Review Checklist (For Review Agent)

- [ ] Every Task implemented with its tests present and passing under -race
- [ ] TurnTerminalEvent matches the parent contract exactly (13 fields, JSON tags)
- [ ] Single emission per terminal path; no double-emission on any test path
- [ ] task.completed/task.failed relays carry real results + task_id
- [ ] WS: turn.* → agent_progress, never chat_message
- [ ] TUI subscribes and handles the timeout status minimally
- [ ] No scope creep: no RPC contract change, no client redesign, no config knob
- [ ] Conventions: gofmt, vet, id.Generate for turn_id, nil-guarded bus access
- [ ] AGENTS.md note for the orchestrator: WS classification section needs the turn. prefix line (orchestrator adds it at integration commit)

Output: APPROVED or list of specific gaps with file + line references.

## Notes

- The 110s stub detection: prefer task-state inspection over reply sniffing.
  If `waitForTaskCompletion` returns because its `done` channel fired with
  the task still non-terminal, that is `status=timeout`. The cleanest seam is
  to have waitForTaskCompletion return (reply, timedOut bool) — a private
  signature change is acceptable; its public behavior is unchanged.
- Do NOT touch: internal/rpc/proxy.go timeouts, syncWaitCeiling semantics,
  the meept-bench repo, the Flutter GUI (WS classification makes the event
  flow automatically; GUI enrichment is the sibling plan's scope).
- The bus message ID: models.NewBusMessage generates its own; turn_id is a
  payload field, independent of it.
- metrics.db dispatch_log rows already carry handler_case — the event's
  handler_case must use the SAME vocabulary so the two can be joined.
