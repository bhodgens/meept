# Hook Pipeline Wiring (PrepareNextTurn) - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit — the orchestrator
> handles all git operations after review. Do NOT use read_file on existing
> source files — explore with search_files or terminal cat. After writing
> a file, do NOT read it back to verify — write once and stop. After
> completing, report what you built, what files you touched, and any
> deviations from the spec.

## Meta

- **Parent:** ../master.md
- **Scope:** Fire the registered PrepareNextTurn hooks in the agent
  loop's reasoning cycle and APPLY the returned TurnModification — the
  prerequisite infrastructure every later leaf of this tree assumes.
- **Dependencies:** 01-spec-config.md
- **Estimated Context:** 55K
- **Concurrency Group:** B (re-letters the old group B; subsequent
  leaves shift to C/D — see master Child Index)
- **Decision references:** D1, D2; ARCHITECTURE AUDIT B1 (2026-09-01)
- **Audit reference:** docs/plans/llm-resilience-forest/ARCH-AUDIT-2026-09-01.md B1

## Goal

**The hook pipeline this tree builds on is INERT.** `HookRegistry`
(internal/agent/hooks.go) has `RunPrepareNextTurn` (:267) with ZERO
production callers — the only invocation is its own test
(hooks_test.go:89). The loop calls only `RunSessionStart` (loop.go:2059)
and `RunSessionEnd` (loop.go:2123). The verification hook is registered
(loop.go:1771) but never fired, and nothing in loop.go reads
`TurnModification.ExtraMessages` (only `ModelOverride` via the separate
`GetModelOverride` seam, loop.go:3219). Today's "escalate to user"
behavior at verification_hook.go:119-170 never executes.

This leaf makes the pipeline live:

1. In the loop's reasoning cycle (the point where the next model call
   is about to be made — locate via the RunSessionStart/RunSessionEnd
   call sites and the turn-construction path; do NOT guess: trace from
   loop.go:2059 to find the per-turn seam), invoke
   `hookRegistry.RunPrepareNextTurn(ctx, turnState)` once per turn.
2. Apply the returned `TurnModification`:
   - `ExtraMessages` non-empty → prepend them to the next LLM call's
     message slice (they are llm.ChatMessage already).
   - `ModelOverride` non-empty → route through the EXISTING
     `SetModelOverride` seam (loop.go:1334; read loop.go:3219-3246 for
     how overrides are consumed and cleared) — do not invent a second
     override channel.
   - `Modified` false → no-op.
3. Errors/panics from hooks must not kill the turn: log + continue
   (mirror how RunSessionStart failures are handled — find and copy its
   guard pattern).

## Context

Key files (line anchors per the 2026-09-01 architecture audit, HEAD
90fd2afb):
- `internal/agent/hooks.go:267` — `RunPrepareNextTurn` (the orphan).
- `internal/agent/hooks_test.go:89` — the only existing invocation;
  shows the expected call shape.
- `internal/agent/loop.go:2059` (RunSessionStart), `:2123`
  (RunSessionEnd) — where the loop already talks to the hook registry;
  the new call site belongs near the per-turn seam these imply.
- `internal/agent/loop.go:1771` — hook registration (already fires at
  construction; only the RUN call is missing).
- `internal/agent/loop.go:1334` (`SetModelOverride`), `:3219-3246`
  (GetModelOverride consumption + one-shot clearing) — the override
  seam this tree's later leaves build on. READ these lines first: the
  one-shot clearing semantics (cleared after first application) are
  exactly what tree 01 leaf 04 must handle (see leaf 04's Notes).
- `internal/agent/verification_hook.go:109` (`PrepareNextTurn` hook
  method) — the registered hook that starts working once this leaf
  lands.
- IMPORTANT: components.go registers security/secret hooks (:1245,
  :1263) that are EQUALLY dead. This leaf wires the pipeline generally
  (all registered PrepareNextTurn hooks run), so those hooks come alive
  too — note this side effect in the report and flag it in Deviations;
  do not attempt to gate or disable them here.

## Interface Contracts (From Parent)

### What This Leaf Exposes

```go
// internal/agent/loop.go (modifications; no new exported API — the
// pipeline is internal):
// 1. Per-turn hook invocation: before each LLM call in the reasoning
//    cycle, call h.registry.RunPrepareNextTurn(ctx, turnState) (the
//    loop's existing hook registry field).
// 2. Modification application (helper for testability):
// applyTurnModification(loop *AgentLoop, mod TurnModification) error
//   - ExtraMessages → prepend to the pending call's messages
//   - ModelOverride → loop.SetModelOverride(...) (existing seam)
//   - idempotent for zero-value modifications
// Behavior guard: hook errors are logged and swallowed (turn proceeds);
// hook panics recovered the same way RunSessionStart guards.
```

### What This Leaf Consumes

```go
// Existing: HookRegistry.RunPrepareNextTurn, TurnModification,
// SetModelOverride — all present; this leaf adds the CALL SITES.
```

## Tasks

### Task 1: Trace + choose the per-turn seam

**Objective:** One documented decision: where the hook fires.

**Files:**
- No code change yet. Read loop.go around 2059/2123 and the turn
  construction path; identify the single point where (a) turn messages
  are finalized and (b) the model ref is resolved — the hook must run
  BEFORE both, once per turn.
- Record the chosen site in a code comment + the leaf report with
  file:line evidence.

### Task 2: Invoke + apply (TDD)

**Objective:** The pipeline is live and observable.

**Files:**
- Modify: `internal/agent/loop.go` (call site + applyTurnModification)
- Test: `internal/agent/loop_hooks_test.go` (new; or extend the loop's
  existing hook test file if one exists — locate first)

**Step 1:** Failing tests (fake hook registry injecting a hook that
returns a modification): (a) ExtraMessages appear in the next call's
messages (assert on the message slice passed to a stubbed LLM client);
(b) ModelOverride changes the model ref of the next call (assert via
the model the stub receives); (c) a hook returning zero-value
modification changes nothing; (d) a hook that panics → turn completes
normally, panic logged; (e) a hook that errors → same.
**Step 2:** FAIL. **Step 3:** Implement per the contract.
**Step 4:** PASS + `go test ./internal/agent/ -count=1` fully green
(this is the loop package — run the WHOLE suite; any pre-existing
failure must be enumerated BEFORE edits and excluded with evidence).

### Task 3: Legacy-behavior confirmation

**Objective:** The verification hook's existing nudge path works
end-to-end now that the pipeline is live.

**Files:**
- Test: `internal/agent/loop_hooks_test.go` extension — register a
  VerificationAutoTrigger-shaped stub (or the real one if construction
  is feasible in tests) that returns the nudge modification; assert the
  nudge message lands in the next call.

**Verify:** `go build ./...`; `go test ./internal/agent/... -count=1`.

## Self-Verification Checklist

- [ ] RunPrepareNextTurn fires once per turn at the documented seam
- [ ] ExtraMessages + ModelOverride both applied (tested)
- [ ] Hook panic/error cannot kill a turn (tested)
- [ ] No second override channel (SetModelOverride reused)
- [ ] Security/secret-hook side effect flagged in Deviations
- [ ] gofmt/vet/analyzers clean; full agent suite green

**DO NOT COMMIT.**

**Deviations from spec:** [none / list any with rationale]

## Review Checklist (For Review Agent)

- [ ] Every task implemented; tests present and passing
- [ ] The seam decision is documented with file:line evidence
- [ ] Zero-value modifications are true no-ops (no message duplication)
- [ ] No behavioral change when NO hooks are registered (regression)

Output: APPROVED or specific gaps with file + line references.

## Notes

- This leaf is PURE INFRASTRUCTURE: it must not change behavior for any
  agent whose hooks return no modifications. The verification hook's
  auto_trigger config gates whether IT modifies turns (existing code);
  this leaf only makes the fire-and-apply mechanism real.
- loop.go is ~6800 lines. Keep the diff surgical: one call site + one
  helper. Do not refactor surrounding code.
