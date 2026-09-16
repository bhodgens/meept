# Liveness Watchdog + Turn Reaper - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit — the orchestrator
> handles all git operations after review. Do NOT use read_file on existing
> source files — explore with search_files or terminal cat. After writing
> a file, do NOT read it back to verify — write once and stop. After
> completing, report what you built, what files you touched, and any
> deviations from the spec.

## Meta

- **Parent:** ../master.md
- **Scope:** Daemon-side stall detection: a reaper goroutine that emits honest `turn.terminal` failed events for turns that vanished between ack and dispatch, and Touch wiring completeness so every live turn stays non-stale.
- **Dependencies:** 01-async-rpc-mode.md (registry + Touch plumbing)
- **Estimated Context:** ~50K
- **Concurrency Group:** C

## Goal

Async turns can die silently: daemon crash between ack and dispatch, a
worker goroutine that never emits, or a parked turn whose resume is lost.
Under the sync model the caller's timeout caught these; under ack+events
the client's liveness timeout sees silence but cannot distinguish "slow"
from "dead." This leaf adds the daemon-side half: a periodic reaper that
detects stale tracked turns and emits `turn.terminal` with
`status=failed`, `error="turn reaped: no progress for <D>"` — so every
submitted turn is guaranteed to eventually reach a terminal state. It also
verifies and completes the Touch wiring so healthy turns never go stale.

## Context

Leaf 01 delivered `internal/agent/turn_registry.go`:
Register/AttachTask/Touch/Complete/Stale + injectable clock. Plan 1's
`publishTurnTerminal` is the emission point (handler.go). Worker lifecycle
events flow through `publishWorkerEvent` (handler.go:1341), which Touches
the turn (leaf 01 wiring). The daemon's lifecycle pattern for periodic
goroutines: `internal/agent/dispatcher.go` Stop() and the dispatcher's
background workers (NewDispatcher spawns some; see the goroutines near
construction and the Stop method ~line 649) — the reaper should live in
ChatHandler or a small own type started by daemon composition next to the
ChatHandler wiring (internal/daemon/components.go / daemon.go, wherever
SetTurnRegistry is called).

Key files:
- internal/agent/turn_registry.go — Stale() consumption
- internal/agent/handler.go — publishTurnTerminal, publishWorkerEvent,
  ChatHandler lifecycle (Start/Stop if any; the subscription lifecycle at
  :233-260 is the pattern)
- internal/daemon — where ChatHandler is constructed and wired

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
// internal/agent/turn_watchdog.go (new):
//
// type TurnWatchdog struct{...}
// func NewTurnWatchdog(reg *TurnRegistry, emit func(TurnTerminalEvent), logger *slog.Logger) *TurnWatchdog
//   // emit is the injection seam — production passes ChatHandler.publishTurnTerminal
//   // wrapped; tests pass a capturing func. (Avoids watchdog→ChatHandler import.)
//
// func (w *TurnWatchdog) Start(interval, staleAfter time.Duration)  // spawns goroutine
// func (w *TurnWatchdog) Stop()                                     // stops, waits for exit
// func (w *TurnWatchdog) reapOnce() int                             // one pass; returns reaped count (exported for tests? no — unexported, tested via Start with tiny interval OR via a RunOnce() exported test seam)
//
// Behavior: every pass, for each r in reg.Stale(staleAfter):
//   emit TurnTerminalEvent{ConversationID: r.ConversationID, TurnID: r.TurnID,
//     TaskID: r.TaskID, Status: "failed",
//     Error: fmt.Sprintf("turn reaped: no progress for %s", staleAfter),
//     HandlerCase: "turn_reaped", Reply: "this turn stopped responding and was marked failed"}
//   reg.Complete(r.TurnID)
//
// Config: OrchestratorConfig (internal/config/schema.go) gains
//   TurnWatchdog TurnWatchdogConfig — {Enabled bool (default TRUE),
//   IntervalSeconds int (0→30), StaleAfterSeconds int (0→120)}
//   + defaults in the defaults function + config/meept.json5 template block
//   (comment: liveness-based, workload-independent; replaces the old
//   static task-wait semantics for async turns).
//
// Task 2 wiring: ChatHandler.publishWorkerEvent calls reg.Touch(turnID) for
//   EVERY worker event path (verify each; the leaf-01 wiring may cover only
//   one), AND handleRequest's dispatch paths AttachTask when a task is
//   created (so reaped events carry task_id), AND
//   daemon composition starts/stops the watchdog with the handler.
```

### What This Leaf Consumes

```
// Leaf 01: TurnRegistry (Stale/Complete/Touch), SetTurnRegistry wiring.
// Plan 1: publishTurnTerminal + TurnTerminalEvent (via the emit func seam).
```

## Tasks

### Task 1: TurnWatchdog type + config

**Objective:** The reaper with injected clock/emit, config knob, template.

**Files:**
- Create: `internal/agent/turn_watchdog.go`
- Modify: `internal/config/schema.go` (OrchestratorConfig + defaults)
- Modify: `config/meept.json5` (template block next to the other orchestrator keys)
- Test: `internal/agent/turn_watchdog_test.go` + config template test extension

**Step 1: Failing tests**

- reapOnce with two stale + one fresh turn → two emits with exact payload
  (Status failed, Error text, HandlerCase "turn_reaped", ConversationID/
  TurnID/TaskID carried), both Complete()d, fresh one untouched.
- Start/Stop: tiny interval (5ms) + injected emit; Stop terminates within
  a bounded wait (no goroutine leak — assert via channel close).
- Emit panic does not kill the loop (recover per pass, log).
- Config: defaults function yields Enabled=true, 30/120; json5 template
  parses (extend the existing template parse test).
- Disabled: Start with Enabled=false is a no-op at the composition site
  (test the composition helper's guard).

**Steps 2-4:** standard cycle.

### Task 2: Touch/AttachTask wiring completeness

**Objective:** Every turn with live progress never goes stale; task turns
carry task_id in reaped events.

**Files:**
- Modify: `internal/agent/handler.go` (publishWorkerEvent Touch; AttachTask
  at task-creation points in handleRequest; Complete already from Plan 1)
- Test: extend handler tests

**Step 1: Failing tests**

- Worker event for a tracked turn → registry Turn's LastProgressAt advanced
  (inject clock).
- Sync-dispatch task creation → AttachTask called; a reaped event for that
  turn carries the task_id.
- Untracked turns (legacy chat, empty TurnID) → no Touch, no panic, no
  registry churn.

**Steps 2-4:** standard cycle. Enumerate the worker-event sites by grepping
publishWorkerEvent callers — every caller must Touch (or the Touch inside
publishWorkerEvent itself covers all — prefer the single point).

### Task 3: Daemon composition + lifecycle

**Objective:** Watchdog constructed, started, and stopped with the daemon.

**Files:**
- Modify: the daemon wiring site where SetTurnRegistry is called (locate via
  leaf-01's composition; likely internal/daemon/components.go or daemon.go)
- Test: composition-level test if a pattern exists (components tests);
  otherwise verify via Stop-lifecycle unit test + manual smoke note.

**Step 1: Failing test** — components construction with the config enabled
exposes a started watchdog (expose a getter or verify via emitting on a
stale turn after construction); daemon Stop stops it (no leak: the existing
daemon shutdown test pattern — extend if present).

**Steps 2-4:** standard cycle. Disabled config → not started (explicit test).

### Task 4: metrics (observability)

**Objective:** Reaps are countable.

**Files:**
- Modify: the emit wrapper to also log a Warn line "turn reaped" with
  turn_id, conversation_id, stale duration (the metrics.db llm_calls /
  dispatch_log integration is OUT of scope — the log + terminal event
  suffice; do not widen schema).

**Step 1: Failing test** — emit wrapper logs (capture via a slog handler
test or the existing log-capture pattern in handler tests).

**Steps 2-4:** standard cycle.

## Self-Verification Checklist

- [ ] All tasks implemented; build/gofmt/vet clean
- [ ] Reaper emits valid Plan-1 payloads; Complete()s every reaped turn (no double-reap: assert Stale→Complete idempotency across passes)
- [ ] Healthy turns never reaped (Touch covers all worker-event sites — enumerate them in the report)
- [ ] Disabled config = zero goroutines started
- [ ] Injected clock everywhere; no real sleeps in tests; -race clean
- [ ] Config knob + template + defaults + template-parse test
- [ ] Panic-in-emit survives the loop

**DO NOT COMMIT.** Orchestrator commits after review.

**Deviations from spec:** [none / list any with rationale]

## Review Checklist (For Review Agent)

- [ ] Tasks + tests present/passing (-race)
- [ ] Contract 2 registry API used as-is (no registry changes except what 01 already shipped)
- [ ] No blind retry/reattempt anywhere (reaper marks failed; it does NOT re-dispatch — per the master plan's reattempt-safety rule)
- [ ] Config defaults: enabled=true, 30s interval, 120s stale-after; template comment accurate
- [ ] Daemon lifecycle: started with handler wiring, stopped on shutdown, disabled path inert
- [ ] No scope creep: no client changes, no cancellation implementation

Output: APPROVED or specific gaps with file+line.

## Notes

- Reaped ≠ cancelled: in-flight work is NOT killed (cancellation is a
  documented non-goal for this tree). The reaper only marks the turn failed
  so clients stop waiting. If the work later completes, the normal
  turn.terminal still fires — clients must treat a post-reaped completion
  as valid (the GUI/TUI leaves' "late terminal after stalled renders"
  tests cover the client side).
- If leaf 01's Touch wiring already covers publishWorkerEvent, Task 2 is
  verification + tests only — say so in Deviations.
