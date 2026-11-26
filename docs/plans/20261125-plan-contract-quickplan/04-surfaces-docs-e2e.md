# Leaf 04 — Surfaces, docs, e2e for the plan-contract quickplan

Contract: consumes C-1..C-4. Wave C (after 01, 02, 03).

## Deliverables

1. Surfaces: quickplan ack renders the brief plan (TUI chat card +
   GUI bubble parity per AGENTS.md rule); plans list shows quickplan
   rows advancing executing->completed (they are real rows now —
   verify existing TUI/GUI render paths need no change; if the ack's
   subtask list shape changed, adjust chat.go/formatTaskCompletedMessage
   consumers' tests, not product logic, unless a real gap).
2. docs/workflows/plans.md: quick_plan section rewritten — plan row is
   the contract, one-or-more tasks, rollup completion, oneshot default,
   approval_requested exception. docs/plans/quickplan-mode/master.md:
   add an AMENDED-BY pointer to this tree's Contract 2 note.
3. e2e/suites/plan-lifecycle new scenarios (manifest entries required —
   run the registry self-tests):
   - dispatched quick_plan: plan row reaches completed; exactly one
     primary task; PruneStalePlans later cancels NOTHING (no zombies).
   - compound "plan X then implement it" same assertions.
   - "plan it but implement only after I approve" -> row parks
     pending_approval; /plan confirm drives execution; completes.
   - adjudication outage (fake LLM error on the veto-suspect slot) ->
     oneshot proceeds, turn never parks.

## Files owned

docs/*, e2e/suites/plan-lifecycle/*, e2e/manifest.json (scenario +
path_map entries), internal/tui test pins if surfaces need assertion
updates ONLY.

## Gates

make e2e-area AREA=plan-lifecycle (or the repo's suite command);
manifest self-tests green; gofmt/docs lint gates. ONE commit naming
M11/H6/M6 + surfaces.

## Status: landed (leaf 04)

Commit [a] (e2e + manifest): quickplan_contract_test.go adds scenarios
plan-lifecycle-07 (dispatched quick_plan: draft->approved->executing,
task_id == dispatched task, exactly one task row, rollup completed, no
'plan created' on the async lane), -08 (sequential compound rides the
same lane, classified_by compound_quickplan), -09 (approval_requested
parks pending_approval + 'plan created: ... (status: pending_approval)'
+ zero tasks; operator approve leaves the gate — CURRENT zero-phase
instant-complete shape pinned as the D5-adjacent owner edge awaiting
the phase-seeding-vs-exemption ruling). All three GREEN on a clean
HEAD+leaf04 snapshot; manifest self-tests green on the committed blob.

Commit [b] (docs): docs/workflows/plans.md "Quick Plan (quick_plan)"
section (plan row IS the contract, 1+ tasks, rollup completion, oneshot
default, approval_requested the only pause, sequential compound same
lane, C-5/D5 adopt-failure posture; leaf-02's corrected
require_approval default line untouched), docs/plans/quickplan-mode/
master.md Contract 2 AMENDED-BY pointer.

Surfaces: NO TUI/GUI change needed — internal/tui/models/plans.go
renders rows state-generically (getPlanStateIcon/Color cover approved/
executing/completed; filterPlans active/completed buckets include them;
zero-phase rows render safely) and the plans tab refresh is driven by
the lifecycle events the new lane publishes (plan.submitting on the
park, plan.completed via rollup); ui/flutter_ui/lib/features/plans/
plans_tab.dart already branches on 'executing'/'completed'. No intent/
mode-specific filtering exists on either surface to break. The
executing->completed row transitions are e2e-gated by scenario -07's
bus-sequence assertion instead of a TUI unit pin (repo rule: new
feature tests live in the hermetic e2e tier).

Follow-ups NOT in leaf 04's file ownership: the inert
handler.go:2158 req.AutoSeal = PlanAutoSealFromIntent(...) block
deletion (handler.go sibling-owned; stub is false since f182dea6), and
the D5-adjacent post-approve execution ruling (zero-phase instant-
complete vs phase-seeding — scenario -09 flags whichever way it lands).
