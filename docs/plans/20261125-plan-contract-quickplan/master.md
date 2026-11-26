# Master — Plan-Contract Quickplan (2026-11-25)

## Meta

- Tree: 20261125-plan-contract-quickplan
- Source: bughunt 2026-11-24 findings H6, M6, M11 (.hermes/audits/2026-11-24-bughunt.md)
  + owner rulings of 2026-11-25 (DECISIONS section below).
- Predecessor tree: docs/plans/quickplan-mode/ — its Contracts 1-7 stay;
  Contract 2's execution semantics are AMENDED by C-2 below.
- Backend: direct dispatch on main with explicit-path commits (deviation
  from the worktree convention, recorded deliberately: 24 commits this
  campaign landed the same way with zero cross-session collisions, and
  sibling sessions hold the shared tree; leaves still commit per-concern).

## Goal

quick_plan becomes an actionable execution with a recorded contract —
not a zombie plan row plus an unlinked ad-hoc task. One dispatch round-
trip; no approval pause; the plan row drives completion as a rollup of
its task(s). Compound "plan then implement" intents ride the same lane.
Phrasing-based vetoes move from regex to model adjudication.

## DECISIONS (owner rulings, 2026-11-25)

- D1 (M11 approval semantics): AUTO-APPROVE + BIND. The brief plan is
  the recorded contract. Execution starts immediately. Review validates
  steps against the plan.
- D2 (M11 linkage shape): "A plan can have 1 or more tasks associated
  with it. Task progress is tracked within the task and plan completion
  is tracked as a rollup of tasks." — this IS the existing full-plan
  machine (Synthesize -> per-phase tasks; PlanManager rollup).
  quick_plan must run THROUGH it, not beside it.
- D3 (M6): model adjudication, A+B. Primary: the intent analyzer gains
  an approval_requested attribute. Fallback: a small-model adjudication
  call when a veto-like phrase is suspected or the analyzer degraded.
  The regex dies.
- D4 (H6/compound): "this is one task, 'plan and execute' — aka
  quickplan. It should oneshot it." Compound intents route to the
  quickplan lane. No approval gate by design. The dead AutoSeal wiring
  is deleted.

## Ruling reconciliation (how D3 and D4 coexist)

Oneshot is the DEFAULT posture for quickplan/compound. D3 provides the
single exception: when the adjudication says the USER explicitly asked
to approve before execution ("implement only after I approve"), the
plan lands in pending_approval for human sign-off instead of auto-
approving. The gate is therefore intent-driven, never regex-driven, and
never blocks the plain "plan and do it" case.

## Interface Contracts

### C-1 SubmitAndAutoApprove seam (internal/plan)

    func (m *PlanManager) SubmitAndAutoApprove(ctx, planID string) error
    // SubmitPlan CAS (draft->pending_approval) + ApprovePlan CAS
    // (pending_approval->approved, by="system", signoff recorded) +
    // Synthesize, in one call, each step CAS-guarded so a concurrent
    // TUI approve loses cleanly. Reuses the existing auto-approve body
    // (manager.go ~:592) — no state-table changes.
    // Owner: 01-plan-machine.md. Consumers: dispatcher quick_plan branch.

### C-2 quick_plan dispatch rewrite (internal/agent)

    // dispatcher.go routeToPlan quick_plan branch: DELETE the separate
    // d.createTask(...) orphan (the current :2353-2371 shape). Call
    // SubmitAndAutoApprove; the plan's synthesized task(s) ARE the
    // execution. DispatchResult keeps Task = the plan's PRIMARY task so
    // the async ack + orchestrator.plan event carry the brief plan for
    // the user to see/cancel/steer. Ack text changes from "plan created"
    // to naming the brief plan is in flight.
    // AMENDS quickplan-mode Contract 2: the strategic quick_plan mode
    // remains for decomposition semantics only; task CREATION moves to
    // the plan machine. RequiresPlanning stays true.
    // Owner: 01-plan-machine.md + 02-compound-oneshot.md.

### C-3 approval_requested adjudication (internal/agent)

    // TrueIntentAnalysis gains: ApprovalRequested bool
    // `json:"approval_requested,omitempty"` (intent_analyzer.go:17).
    // Prompt field + parse. Default false (absent = legacy oneshot).
    // Suspected-veto narrow trigger -> one bounded small-model adjudication
    // (reuse the classifier chatter slot; ctx-bounded; failure = keep
    // false = oneshot; NEVER let adjudication failure park a turn).
    // Consumed: C-1 call site — ApprovalRequested true => SubmitPlan
    // (human pending_approval) instead of SubmitAndAutoApprove.
    // DELETED: PlanAutoSealFromIntent + autoSealNegatedExecute regexes
    // (strategic.go ~:2489) and the handler.go AutoSeal wiring
    // (~:2158 — sibling holds handler.go dirt; sequence after it lands).
    // Owner: 03-approval-adjudication.md.

### C-4 compound routing merge

    // IntentCompound / IsCompound routes to the quickplan lane (C-2).
    // spec_pair pair-session machinery: kept available as a step
    // EXECUTOR concern inside Synthesize-produced tasks, not as a
    // dispatch lane that bypasses the plan machine. If a compound
    // request needs spec+implement agents, that is what the planner
    // phases/steps express.
    // Owner: 02-compound-oneshot.md.

### C-5 quick_plan adopt-failure posture (parent ruling D5, 2026-11-25)

    // AdoptTask transient failure at dispatch: dispatcher retries once; on
    // second failure records SynthesisError on the row ("adopt task
    // failed: <err>") and the row stays approved. The reclaimer's
    // approved-scan must SKIP approved rows carrying a quick-plan
    // SynthesisError (never re-drive them through Synthesize: the
    // zero-phase guard would falsely complete the brief while the real
    // task runs). The dispatched task proceeds unchanged — the contract
    // row is degraded, not the turn. Leaf 02 carries the plan-side guard.

## Leaves (dispatch order; groups are file-disjoint)

1. 01-plan-machine.md — C-1, C-2 quickplan half. Wave A.
2. 02-compound-oneshot.md — C-4 + C-2 compound half. Wave B (after 01).
3. 03-approval-adjudication.md — C-3. Wave A (parallel to 01; disjoint
   files except the C-1 call site, which 01 leaves a boolean branch for).
4. 04-surfaces-docs-e2e.md — ack copy, TUI/GUI quickplan visibility,
   docs/workflows/plans.md + quickplan-mode contract amendment note,
   e2e gates (plan row reaches executing->completed; no zombie rows;
   approval_requested holds a plan pending). Wave C.

## Execution notes

- 01/03 touching dispatcher.go overlap: 03 lands the ANALYZER attribute
  only; the call-site wiring is in 01. Sequence 01 then 03 call-site
  patch, or one fixer owns both seams. Parent arbitrates.
- handler.go is sibling-dirty at plan authoring time; the AutoSeal
  deletion leg of C-3 waits for that landing.
- Bench/e2e: the quickplan acceptance lives in e2e/suites/plan-lifecycle
  (existing suite, new scenarios) — a dispatched quick_plan turn must
  end with the plan row StateCompleted and its task rollup consistent.

## Audit linkage

- Closes bughunt 2026-11-24: H6 (D4+D3), M6 (D3), M11 (D1+D2).
- M13-adjacent behavior change note: quick_plan turns no longer create
  two independent task rows (plan lane + orphan) — one primary task per
  brief plan (plus per-phase tasks as Synthesize expands).
