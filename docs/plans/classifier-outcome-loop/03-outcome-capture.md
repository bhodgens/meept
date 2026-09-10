# Outcome Capture - Implementation Leaf

## DISPATCH INSTRUCTION

> **Implementing agent:** Implement ALL tasks below using TDD. Do NOT
> commit. Do NOT use read_file on existing source files — explore with
> search_files or terminal cat. After writing a file, do NOT read it back
> to verify — write once and stop.

## Meta

- **Parent:** docs/plans/classifier-outcome-loop/master.md
- **Scope:** Write outcome/corrected_agent from two mechanical signals:
  re-route detection (Signal A) in recordDispatch and failure/replan
  hooks (Signal B) at Escalate + quickplan fallback.
- **Dependencies:** 01-persist-privacy.md (outcome/corrected_agent/
  turn_no columns exist)
- **Estimated Context:** ~60K
- **Concurrency Group:** B (parallel with 02-margin-capture.md —
  disjoint files)

## Goal

The dispatch_log row for turn N currently stays 'pending' forever. This
leaf resolves it: when the next dispatch in the same session lands on a
DIFFERENT agent within 3 turns, the prior classification was probably
wrong (user re-asked) — mark it corrected. When a task fails and the
system re-plans, mark it failed_replan. This is the data that makes
in-loop accuracy measurement possible.

## Context

ChatHandler calls RecordDispatch AFTER the handler switch resolves
handlerCase (handler.go:853-854) — so recordDispatch
(dispatcher.go:3024) already knows the FINAL agent for the current
dispatch (result.AgentID at :3036). That makes Signal A a pure SQL
pattern: look back one pending row for the session, compare.

Key files:
- internal/agent/dispatcher.go — recordDispatch (:3015-3085)
- internal/metrics/store.go — add UpdateDispatchOutcome +
  RecordDispatchWithTurnNo support (or extend RecordDispatch)
- internal/agent/tactical.go — Escalate call (:1440), task_id at hand
- internal/agent/strategic.go — quickplan fallback (:419-427)

## Interface Contracts (From Parent)

### What This Leaf Exposes

```go
// internal/metrics/store.go (new methods):
// ResolvePendingOutcome(sessionID string, turnNo int, agentID string,
//     window int) error
//   Finds the most recent row for sessionID WHERE outcome='pending'
//   AND classifier_method != '' AND turn_no < :turnNo. If found:
//     IF agent_id != :agentID AND (:turnNo - row.turn_no) <= :window
//       -> outcome='corrected', corrected_agent=:agentID
//     ELSE -> outcome='ok'
//   (Exactly one prior row is resolved per dispatch — the latest one.
//   Older pending rows remain pending by design; the nightly harvest
//   treats stale pending as excluded-from-denominator.)
//
// MarkTaskFailedReplan(taskID string) error
//   UPDATE dispatch_log SET outcome='failed_replan'
//   WHERE task_id=:taskID AND outcome='pending'
```

### What This Leaf Consumes

```go
// leaf 01 columns: outcome, corrected_agent, turn_no, task_id (exists)
// Call sites: dispatcher.recordDispatch (Signal A),
// tactical Escalate path (:1440), strategic quickplan fallback (:419-427)
```

## Tasks

### Task 1: Store methods

**Objective:** ResolvePendingOutcome + MarkTaskFailedReplan in
internal/metrics/store.go, following the existing method patterns
(RecordDispatch :855-864).

Failing tests (mirror llm_calls_test.go setup):
1. Two dispatches, same session, same agent, 1 turn apart ->
   prior row outcome='ok', corrected_agent=''.
2. Two dispatches, same session, DIFFERENT agents, 1 turn apart ->
   prior outcome='corrected', corrected_agent=second agent.
3. Different agents but 5 turns apart (window 3) -> prior 'ok'.
4. Prior row classifier_method='' (short_message_guard) -> stays
   'pending' (non-classified paths are not correction sources).
5. MarkTaskFailedReplan flips only the matching task_id's pending row.
6. No prior row -> ResolvePendingOutcome is a no-op (no error).

Implementation notes: single UPDATE with a subquery for "most recent
pending before turn_no", or SELECT then UPDATE in one transaction
(prefer the explicit SELECT+UPDATE — clearer, and volume is tiny).
Both classified and non-classified CURRENT dispatches resolve the
prior row (any follow-up message means the session continued) — but a
non-classified current dispatch can never mark 'corrected' (its agent
comparison is meaningless): if current.classifier_method == '' and
there IS a prior pending row, set prior 'ok' only.

### Task 2: Signal A in recordDispatch

**Objective:** call ResolvePendingOutcome for every dispatch with a
session (after the INSERT of the current row; turn_no computed as in
leaf 01).

Failing test: dispatcher-level with wired metrics store — sequence of
three dispatches (llm/code -> llm/review -> llm/code): first resolves
'corrected' when the second lands on a different agent; second
resolves 'ok' when the third lands on the same agent.

Implementation: one line after the INSERT (the turn_no computation
from leaf 01 is reused — pass the same number). Window constant:
`reRouteWindow = 3` (package const, cited to design.md S2).

### Task 3: Signal B hooks

**Objective:** MarkTaskFailedReplan at the two failure/replan sites.

Sites (cite-verified in design.md):
- internal/agent/tactical.go:1440 — ts.escalationManager.Escalate on
  step failure. The TacticalScheduler needs the metrics store
  (grep: does it have one? If not, add a SetMetricsStore setter with
  the standard nil guard — the daemon wiring point is the same one
  leaf 02 of the allotment tree used for SetContextWindowProvider).
- internal/agent/strategic.go:419-427 — quickplan fallback to fallback
  steps. The StrategicPlanner needs the same setter treatment.

Failing tests: fake store records MarkTaskFailedReplan calls with the
right task_id when Escalate fires on a failed step; and when the
quickplan plan path degrades.

Implementation: nil-guarded — no store, no call (preserves every
existing no-metrics invariant).

## Self-Verification Checklist

- [ ] All 6 store tests pass (ok/corrected/window/non-classified/
      failed_replan/no-op)
- [ ] Signal A fires for every dispatch with a session, after INSERT
- [ ] Non-classified current dispatches resolve prior to 'ok' only
- [ ] Signal B hooks nil-guarded; default behavior unchanged without
      metrics store
- [ ] gofmt/vet clean; ASCII; no TODOs

**DO NOT COMMIT.** Orchestrator handles git operations.

**Deviations from spec:** document any (expected: exact setter names
may differ to match existing field naming on
TacticalScheduler/StrategicPlanner).

## Review Checklist (For Review Agent)

- [ ] Store methods per Contract 3; window = 3; classified-only
      correction sources
- [ ] Signal A after INSERT, same turn_no as the current row
- [ ] Signal B at both sites; nil-guarded
- [ ] Stale pending rows remain pending (excluded from denominators)
- [ ] No changes to direct-mode or non-plan paths beyond the hook
- [ ] Daemon wiring for the two new setters present (mirror
      SetMetricsStore call sites)

Output: APPROVED or specific gaps.

## Notes

- design.md S2 is authoritative. Signal A is deliberately mechanical:
  agent switch within 3 turns is a PROXY for "user re-asked", not a
  proof. The adjudication sheet is where humans confirm.
- "no I meant X" free-text parsing is explicitly out of scope.
- Volume check: ResolvePendingOutcome runs once per dispatch — one
  indexed SELECT + one UPDATE. Negligible at meept traffic.
