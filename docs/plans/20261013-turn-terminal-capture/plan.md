# Turn-Terminal Memory Capture (Option C) Implementation Plan

> **For Hermes:** Use subagent-driven-development. Dispatch one subagent per
> task with the task text + Contract section inline. Orchestrator reviews
> in-session and commits. Implementation agents NEVER commit.

**Goal:** Move L0 conversation recording and ambient epistemic extraction off
the session-end hook registry onto `turn.terminal` bus subscribers, so every
turn type — direct, dispatched/task-scoped, relayed — is captured.

**Architecture:** A new daemon-side subscriber (`internal/daemon/memory_capture.go`)
subscribes to `agent.TopicTurnTerminal` via `bus.SubscribeT`. On each terminal
event (status `completed` only), it reads the exchange (user message + final
reply) from the session message store, appends L0 lines via the existing
`l0.Recorder`, enqueues the L1 distill job via the existing `JobEnqueuer`, and
runs ambient extraction via the existing `AmbientExtractor`. The session-end
hook registrations for `l0_record` are deleted; the loop-level epistemic hook
stays (it is a different seam — see Exclusions). Async, panic-recovered, never
blocks the turn.

**Tech stack:** Go 1.26, `internal/bus` typed topics (`bus.SubscribeT` with
`agent.TopicTurnTerminal`), existing `internal/memory/l0`, `AmbientExtractor`,
`PersistentQueue`.

---

## Background (why)

`.github/ISSUES/task-loops-no-hook-registry-l0-ambient.md`: task-scoped agent
loops never receive the primary hook registry, so with `multiagent.enabled=true`
dispatched turns record no L0 and run no ambient extraction. User chose
option (c): move capture to the turn-terminal seam, which every loop type
already emits.

## Key seam facts (verified 2026-10-13 — trust these over re-derivation)

1. **Topic:** `agent.TopicTurnTerminal = bus.NewTopic[TurnTerminalEvent]("turn.terminal")`
   (internal/agent/topics.go:14). Subscriber shape:
   `bus.SubscribeT(b, id, agent.TopicTurnTerminal, func(ev agent.TurnTerminalEvent))`
   (internal/bus/topic.go:80).
2. **Payload** `TurnTerminalEvent` (internal/agent/handler.go:253): has
   ConversationID (required), SessionID (often set), TurnID, Status
   ("completed"|"failed"|"timeout"|"parked"), Reply (final reply text).
   **It does NOT carry the user message** — the subscriber must read it from
   the session store.
3. **All terminal paths funnel through `publishTurnTerminal`** (handler.go:1662,
   single emission point; 11 call sites + the exported `EmitTurnTerminal`
   watchdog seam). A subscriber on the topic sees every turn shape.
4. **User message source:** the session message store. `persistExchange`
   (handler.go:1244, also :2426, :2852) saves both sides via
   `SessionStore.AddMessage`-family calls; `store.GetMessages(sessionID, offset, limit)`
   returns `[]session.Message{Role, Content}` (internal/session/store.go:12).
   The exchange to capture = last `role=="user"` message + `ev.Reply` (the
   terminal event already carries the reply; do not re-derive it).
5. **Existing L0 hook to retire:** `internal/daemon/l0_wiring.go:161`
   registers `l0_record` session-end hook holding `l0RecordHook` (recorder +
   enqueuer + marker logic, lines 191-240). Its Append + enqueue logic moves
   into the new subscriber; the `l0_progress` marker semantics are unchanged.
6. **Ambient extraction today:** loop-level `epistemicHook`
   (internal/agent/loop.go:3333, wired via `SetEpistemicHook` in
   internal/daemon/epistemic_wiring.go:176). This is ALSO loop-local — the
   same dispatch gap applies. Move it too: the subscriber runs the existing
   `AmbientExtractor.Extract + WriteCandidates` over the store-read exchange.
7. **Subscriber precedent:** `internal/daemon/components.go:8023`
   (`h.bus.Subscribe("status-handler", "status.request")`) and
   `wireProjectPromptTier` (prompt_dirs.go:75) show the daemon's subscription
   + pump pattern. Typed flavor: `bus.SubscribeT` per topics.go contract.
8. **Config gates unchanged:** `memory.l0_enabled` and
   `memory.epistemic.ambient_extraction.enabled` (verify exact key in
   internal/config/schema.go before wiring) gate the whole feature. Both
   default false.

## Contract (frozen — leaf implements exactly this)

```go
// internal/daemon/memory_capture.go
type MemoryCaptureConfig struct {
    Enabled      bool   // memory.l0_enabled
    Ambient      bool   // ambient extraction gate (schema key verified at implementation)
    L0Dir        string // <memory.data_dir>/l0
    DataDir      string // memory data dir
}

// NewMemoryCapture builds the turn-terminal subscriber. Nil-safe on every
// dependency; a nil piece disables just that piece. Returns the Subscriber
// (caller may ignore; bus owns lifetime).
func WireMemoryCapture(
    b *bus.MessageBus,
    store session.Store,          // message source (GetMessages)
    recorder *l0.Recorder,        // nil = L0 recording disabled
    enqueuer memory.JobEnqueuer,  // nil = no distill enqueue
    extractor *memory.AmbientExtractor, // nil = ambient disabled
    logger *slog.Logger,
) *bus.Subscriber
```

Subscriber behavior (per event):
1. `ev.Status != "completed"` → return (failed/timeout/parked turns are not
   captured; their replies are error text).
2. `ev.ConversationID == ""` → return. Resolve the capture session ID:
   `ev.SessionID` if non-empty, else `ev.ConversationID`.
3. Read the user message: `store.GetMessages(sessionID, 0, 0)` is NOT the
   API — use the offset/limit form to fetch the LAST page (implementation
   detail: fetch last 20 messages via a large offset or the store's own
   latest-first helper if one exists; find the exact call in
   `internal/services/session_service.go GetMessages` for the pagination
   idiom). Take the last message with `Role == "user"`. Not found → skip L0
   user line (still run ambient over reply-only? NO — skip the whole turn;
   a reply without its prompt is noise).
4. L0: `recorder.Append(sessionID, "user", userMsg, nil)` then
   `recorder.Append(sessionID, "assistant", ev.Reply, nil)` — errors logged,
   never fatal. Same order as the retired hook.
5. Enqueue: reuse the retired hook's enqueue block (since_msg_id = last L0
   record BEFORE this append; marker semantics unchanged).
6. Ambient: run the extractor over `[userMsg, ev.Reply]` (the loop hook
   passed the whole window; the store-read exchange is the equivalent for a
   single turn — window semantics preserved because every turn is captured).
   Best-effort, panic-recovered.
7. ALL of steps 3-6 run inside one `go func` with `defer recover()` —
   subscriber callbacks must never block or panic into the bus pump. Log
   under component `memory-capture`.

Dedup rule: direct-mode turns are captured exactly once by the subscriber;
the retired hook is deleted, so no double-capture. The turn.terminal relay
path (task completion) publishes a SECOND terminal event for the same
exchange with the same ConversationID — dedupe on (ConversationID, ev.Reply)
with a small LRU (512 entries) in the subscriber; duplicates are dropped
with a debug log.

## Task 1: MemoryCapture subscriber (TDD)

**Objective:** Build `WireMemoryCapture` + subscriber logic with unit tests.

**Files:**
- Create: `internal/daemon/memory_capture.go`
- Test: `internal/daemon/memory_capture_test.go`

**Step 1 (RED):** Write tests first: fake session.Store (or use the real
MemoryStore from internal/session — it is in-memory, `NewMemoryStore`),
fake recorder (real `l0.Recorder` on t.TempDir), fake enqueuer (record
payloads), fake extractor. Cases:
- completed turn → 2 L0 lines (user+assistant) with correct session ID
- failed/timeout/parked turn → zero lines
- empty ConversationID → zero lines, no panic
- user message not found in store → zero lines
- duplicate (ConversationID, Reply) → second event dropped
- extractor panic → logged, subscriber survives (next event still processes)
- recorder error → logged, enqueue still attempted (match retired hook's
  independent-error posture; verify against l0_wiring.go:206-215 first)
Run: `go test ./internal/daemon/ -run MemoryCapture -count=1` → fails
(undefined WireMemoryCapture).

**Step 2 (GREEN):** Implement per Contract. Verify RED→GREEN.

**Step 3:** `go vet ./internal/daemon/` clean. Do NOT commit.

## Task 2: Rewire the daemon + retire the hook

**Objective:** Swap the wiring; delete the session-end L0 hook; move ambient
extraction to the subscriber; keep config gates.

**Files:**
- Modify: `internal/daemon/l0_wiring.go` (keep `wireL0Distill` constructing
  recorder/enqueuer/worker — the DistillWorker and queue registration STAY;
  delete only the `RegisterSessionEndHook("l0_record", ...)` block and the
  `l0RecordHook` type; `wireL0Distill` returns the pieces or splits into a
  builder the new wiring calls)
- Modify: `internal/daemon/components.go` — call `WireMemoryCapture` where
  `wireL0Distill` runs today (AFTER queue creation — live-gate lesson,
  commit c941327e), passing the session store (find it: `c.SessionStore` or
  equivalent — verify the exact Components field), recorder, enqueuer,
  extractor (build from epistemic config the same way epistemic_wiring.go
  does today)
- Modify: `internal/daemon/epistemic_wiring.go` — remove `SetEpistemicHook`
  (loop.go:2176, :2530, :3333 stay but become unused by the daemon; do NOT
  delete the loop seam itself — out of scope, note it) and pass the built
  extractor to WireMemoryCapture instead. Verify the epistemic config key
  from internal/config/schema.go.
- Modify: `internal/daemon/daemon.go` or wiring callers if signatures change.

**Steps:** build (`go build ./internal/daemon/`) → run daemon tests
(`go test ./internal/daemon/ -count=1`) → full memory suite
(`go test ./internal/memory/... -count=1`) → vet. Do NOT commit.

## Task 3: e2e proof for a DISPATCHED turn + docs

**Objective:** Prove the case that was broken: a dispatched (multiagent)
turn records L0 + runs ambient extraction.

**Files:**
- Create: `e2e/suites/memory-turn-capture/` (read
  `e2e/suites/memory-persistence/` for harness structure first)
- Modify: `e2e/manifest.json` (path_map + suite entry)
- Modify: `docs/workflows/memory.md` (Layered Distillation section: capture
  is turn-terminal driven, covers dispatched turns; hook seam retired)
- Modify: `.github/ISSUES/task-loops-no-hook-registry-l0-ambient.md` —
  append RESOLVED header pointing at the implementing commit (issue doc
  stays for history)

**Scenario:** seed multiagent.enabled=true config → dispatch a chat turn that
routes to a task loop (follow how existing suites drive dispatched turns; if
none do, use the dispatcher directly with a trivial intent) → assert L0
JSONL gains the exchange lines and a status=auto claim lands after the L1
job. Second scenario: failed turn → no capture.

**Verify:** `make e2e-fast-area AREA=memory-turn-capture && make e2e-fast` (full sweep must stay 57+ ok) && `go vet ./internal/...` scoped to touched pkgs. Do NOT commit.

## Task 4 (orchestrator): review + integration

Orchestrator reviews all diffs, runs `make test` (expect only the known
rotating webfetch/websearch flake class), commits in concern-split commits,
regenerates graphs if any topic surface changed (it does not — no new
topics), runs `make docs-check`.

## Exclusions (explicit)

- The loop-level epistemic hook seam (`SetEpistemicHook`, loop.go:2176/2530/3333)
  stays in the codebase (unused by the daemon after this change); deleting it
  is a separate cleanup leaf after a soak period.
- `session_hooks.go` bus/telemetry hooks are unrelated — untouched.
- No new bus topics. No TurnTerminalEvent payload changes (the store-read
  decision means the frozen payload contract is untouched).
- TUI/GUI: no surface changes.

## Risks

- Relay double-fire: task turns emit TWO terminal events (handler.go:1289
  direct path + handler.go:1847 relay path share the ConversationID);
  the (ConversationID, Reply) LRU dedupes. If the two events carry
  different Reply text (relay rewrites failures — handler.go:1772), the
  dedupe misses but Status gates (relay of a completed task re-sends
  "completed" with the same reply) — verified at implementation; if the
  replies differ for completed tasks, dedupe on ConversationID+TurnID
  instead (TurnID is recovered for relays, handler.go:1828).
