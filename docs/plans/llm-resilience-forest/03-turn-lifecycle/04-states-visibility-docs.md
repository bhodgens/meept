# Parked-Turn States, Surfaces, and Docs - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit — the orchestrator
> handles all git operations after review. Do NOT use read_file on existing
> source files — explore with search_files or terminal cat. After writing
> a file, do NOT read it back to verify — write once and stop. After
> completing, report what you built, what files you touched, and any
> deviations from the spec.

## Meta

- **Parent:** ../master.md
- **Scope:** Agent-state representation for parked (throttle) turns, bus
  events, TUI + Flutter GUI parity surfaces, workflow docs.
- **Dependencies:** 01, 02, 03 (all merged)
- **Estimated Context:** 60K
- **Concurrency Group:** C
- **Decision references:** D9

## Goal

Quota waits already have `agent.StateQuotaWait` ("quota_wait") with a
full transition-table entry (internal/agent/agent_state.go:268-282) and
TUI/GUI surfaces from the quota plan. Throttle parking (leaf 02) reuses
StateQuotaWait with reason "throttle_wait" — this leaf decides and
implements the OBSERVABILITY story:

1. **State naming.** EITHER keep StateQuotaWait for both classes
   (reason metadata distinguishes them) OR add StateThrottleWait.
   Default recommendation: KEEP StateQuotaWait + reason metadata — zero
   new transition-table entries, surfaces keep working, the machine
   truth ("parked on a provider wait") is identical. Implement the
   recommendation unless review finds a surface that cannot carry a
   reason; document the decision in the workflow doc.
2. **Bus events.** Park and resume emit on the EXISTING quota-wait
   lifecycle topics (they exist — the quota plan shipped them) with
   `reason` payloads extended to carry the class. WS classification
   must remain agent_progress (verify the topic-prefix match covers the
   emitted topic names; AGENTS.md shows the `agent.quota` prefix rule —
   if throttle emits a differently-prefixed topic, extend the prefix
   match to cover it).
3. **Surfaces (TUI + GUI parity).** The agents list already renders
   quota_wait. Extend the label to show the wait reason
   ("quota_wait · reset 14:05" / "quota_wait · throttle retry 15:00")
   using TurnParker.Next(class). Both surfaces.
4. **Docs.** `docs/workflows/llm-management.md` (the LLM workflow page)
   gains the universal-parking section: classes, schedules,
  give-up semantics, state semantics; AGENTS.md Critical Invariants
  gains the "no turn hangs" invariant line.

## Context

Key files:
- `internal/agent/agent_state.go:268-282` — transition table;
  StateQuotaWait reachable from all active states + Idle.
- `internal/comm/http/server.go` — transformBusEventToWS; the
  `strings.HasPrefix(topic, "agent.quota")` case (AGENTS.md quotes it).
- TUI: `internal/tui/` agents-tab quota rendering (grep quota_wait in
  internal/tui/).
- GUI: `ui/flutter_ui/` agents-tab quota detail lines (the repo log
  shows recent quota episode detail work — locate the widget).
- `internal/agent/parked_turn.go` — TurnParker.Next(class).

## Interface Contracts (From Parent)

### What This Leaf Exposes

```go
// internal/agent/parked_turn.go (add)
// ParkWaitInfo describes a class's next resume for surfaces:
//   {Class llm.FailureClass, Next time.Time, Pending int}
func (p *TurnParker) WaitInfo() []ParkWaitInfo

// Bus payloads (existing topics, extended):
//   park:     {reason: "quota_wait"|"throttle_wait", class, resume_at, agent_id}
//   resume:   {reason, class, waited, agent_id}
//   give_up:  {reason: "throttle_give_up", waited, agent_id}
// All classified agent_progress by the existing agent.quota prefix
// match (extend the prefix list in transformBusEventToWS ONLY if a new
// topic prefix was introduced — default plan emits on existing topics).
```

### What This Leaf Consumes

```go
// From 01: TurnParker.Next/Pending
// From 02/03: the park/resume/give-up call sites (to attach events)
```

## Tasks

### Task 1: WaitInfo + event payloads

**Objective:** Surfaces have one query; events carry class.

**Files:**
- Modify: `internal/agent/parked_turn.go`
- Modify: park/resume/give-up sites (loop.go, goal_loop.go) to publish
  the payloads above (reuse the existing publish helper the quota path
  uses — locate it first).
- Test: `internal/agent/parked_turn_test.go` (WaitInfo across classes);
  bus-stub assertions on payload keys.

**Step 1:** Failing tests. **Step 2:** FAIL. **Step 3:** Implement.
**Step 4:** PASS.

### Task 2: WS classification check

**Objective:** No park/resume event can become chat_message.

**Files:**
- Modify: `internal/comm/http/server.go` ONLY if a new topic prefix
  slipped in; otherwise add a comment documenting why the existing
  prefix match covers park events.
- Test: `internal/comm/http/` server test for the emitted topic names →
  expected WS types (extend the existing classification test).

**Verify:** `make graphs` regenerates; no orphan topics in
docs/generated/bus-topology.md.

### Task 3: TUI surface

**Objective:** The agents tab distinguishes wait reasons.

**Files:**
- Modify: the TUI agents-tab quota rendering (locate via search_files
  "quota_wait" in internal/tui/)
- Test: TUI model test for the label rendering (follow existing TUI
  tests' pattern)

**Step 1:** Failing test: quota wait renders "quota_wait · reset HH:MM";
throttle wait renders "quota_wait · throttle retry HH:MM"; no waits →
unchanged base status. **Step 2:** FAIL. **Step 3:** Implement
(lowercase text per repo UI rule). **Step 4:** PASS.

### Task 4: Flutter GUI surface (parity)

**Objective:** Same labels in the GUI agents tab.

**Files:**
- Modify: `ui/flutter_ui/` agents-tab quota detail widget (locate via
  search_files "quota" in ui/flutter_ui/lib/)
- Test: the repo's Flutter widget test pattern (recent quota detail
  wiring tests exist — mirror them)

**Verify:** `cd ui/flutter_ui && flutter test` (or `make build-gui` if
tests are wired through make); labels match TUI strings exactly.

### Task 5: Docs

**Files:**
- Modify: the park/quota workflow doc in docs/workflows/ — universal
  parking section (classes, schedules, give-up, state semantics, the
  StateQuotaWait-reuse decision + rationale).
- Modify: `AGENTS.md` — extend the quota invariants block with the
  universal-parking invariant (all turn types park; throttle waits
  reuse quota_wait state with reason metadata; give-up surfaces
  ThrottleGiveUpError).

**Verify:** docs accurate against tests; AGENTS.md same-commit rule
honored.

## Self-Verification Checklist

- [ ] WaitInfo correct under mixed-class parking (tested)
- [ ] Every park/resume/give-up event classified agent_progress (tested)
- [ ] TUI + GUI labels identical; lowercase
- [ ] flutter test green (or build-gui verified if tests unwired)
- [ ] `make graphs` clean; docs + AGENTS.md updated
- [ ] gofmt/vet/analyzers clean

**DO NOT COMMIT.**

**Deviations from spec:** [none / list any with rationale]

## Review Checklist (For Review Agent)

- [ ] Every task implemented; tests present and passing
- [ ] Contract payloads match master exactly (key names)
- [ ] No new agent state added without a transition-table entry (or none added, per recommendation)
- [ ] Surface parity verified both directions

Output: APPROVED or specific gaps with file + line references.

## Notes

- If the quota surfaces already render a reason field, extending it is
  a two-line change — prefer that over new widgets.
- GUI runs on web too: any time formatting must be timezone-safe
  (render absolute HH:MM from the daemon-provided timestamp; do not
  compute "in X minutes" client-side against wall clocks).
