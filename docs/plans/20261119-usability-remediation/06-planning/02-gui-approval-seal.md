# GUI Approval + Seal Surface - Implementation Leaf

> **For the implementing agent:** Implement ALL tasks using TDD. Gate tests
> ALREADY EXIST (00T T2) - make them pass; do NOT delete/skip/weaken them.
> Do NOT commit. Do NOT git add. Explore with search_files/terminal cat;
> write once, never read back. Worktree: .hermes/worktrees/usability-remediation.

## Meta

- **Parent:** orchestrator.md
- **Scope:** GUI surfaces the strategic approval gate and the plan-compiler seal flow
- **Dependencies:** 00T-gate.md, 01-safety/01 (C2 HTTP routes), 01-approval-visibility.md (card contract)
- **Estimated Context:** 80K
- **Concurrency Group:** A
- **Audit references:** W16/W17 (GUI half); P1/P2/P3; notes-plan-hitl F1;
  gui-notes Q3; F-NEW-1 (plans REST panic - 01-safety/07 fixes the daemon
  side; this leaf's GUI plans tab must handle the error gracefully too);
  F-NEW-14 (GUI-path completion unobservable - C6 + 05-navigation fix the
  transport; this leaf verifies the plans/approval surface renders)

## Goal

GUI approval card for awaiting_approval tasks (approve/reject via C2 HTTP
twins); seal surface for compiler drafts (show-draft + seal calling
plan.draft/plan.seal, compile problems inline); hostile_skipped/await-seal
render as actionable wait states, not gray badges.

## Context

- ui/flutter_ui/lib/features/plans/plans_tab.dart:499-553 - pending_approval
  buttons ride plans.db REST (DIFFERENT gate); no task.approve/reject callers.
- ui/flutter_ui/lib/services/sdk_client.dart:1750-1800 approvePlan/rejectPlan
  (plans.db); add approveTask/rejectTask (C2 HTTP /api/v1/tasks/{id}/*).
- ui/flutter_ui/lib/providers/plan_provider.dart:77-135; task_provider.dart.
- internal/rpc/plan_seal.go:93-94/224-227 plan.draft/plan.seal; HTTP twins
  may need adding (check internal/comm/http for plan draft/seal routes;
  if absent add GET /api/v1/tasks/{id}/draft + POST /api/v1/tasks/{id}/seal).
- ui/flutter_ui/lib/features/plans/plans_tab.dart:224-240 color map (draft/
  planning/hostile_skipped fall to gray - add wait-state branch).
- W21 build fix prerequisite (05-navigation/02 T5 also verifies).

## Verified Anchors

- ui/flutter_ui/lib/features/plans/plans_tab.dart:499 - approval buttons
- ui/flutter_ui/lib/services/sdk_client.dart:1750 - approvePlan
- internal/rpc/plan_seal.go:93 - plan.draft registration

## Interface Contracts (From Parent)

### What This Leaf Exposes
- GUI approval card (C2 HTTP); seal surface (plan.draft/plan.seal);
  wait-state rendering for hostile_skipped/await_seal/compile-failure.

### What This Leaf Consumes
- C2 (approval RPC/HTTP + rejected relay), C6 (plan.failed), C1 (mode fields).

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | GUI approval card | Modify: sdk_client.dart (approveTask/rejectTask), plan_provider.dart/task_provider.dart, plans_tab.dart (task-gate card branch) | `cd ui/flutter_ui && flutter test test/features/approval_card_test.dart` | gate T2 RED (no callers) | GREEN | Given awaiting_approval task; Then card approve/reject via C2 HTTP; reject shows reason dialog, never executes; rejected terminal updates UI |
| T2 | Seal surface | Modify: sdk_client.dart (getDraft/sealTask + HTTP routes if absent), plans_tab.dart or new seal widget; internal/comm/http (draft/seal routes if missing) | `flutter test test/features/seal_surface_test.dart` | gate T2 RED (no seal) | GREEN | Given compiler draft; Then surface shows draft markdown + seal button; seal returns compile problems inline (line+message); raw scaffold warns before seal |
| T3 | Wait-state rendering | Modify: plans_tab.dart color/state map (hostile_skipped/await_seal/compile-failure branches) | `flutter test test/features/wait_states_test.dart` (add) | gray badge today | GREEN | Given hostile_skipped/await_seal; Then actionable copy ("awaiting your seal" + button; "hostile review skipped - configure model" + hint), never silent gray |

## Self-Verification Checklist

- [ ] Gate T2 GREEN; gate diff empty
- [ ] C2 exact; reject never executes; compile problems inline
- [ ] flutter build web exit 0; dart format clean
- [ ] No scope creep / artifacts / corruption

**DO NOT COMMIT.** Orchestrator commits after review.

## Completion Report Format

1. Files touched. 2. Verify output. 3. Build log. 4. Deviations.

## Review Checklist (For Review Agent)

- [ ] Task-gate card distinct from plans.db gate (no conflation)
- [ ] Seal never auto-seals raw scaffold without explicit user action
- [ ] copyWith discipline; freezed regen consistent
- [ ] Pixel claims operator-pending (Q3)
- [ ] Gate untouched; conventions; no artifacts

Output: APPROVED or gaps with file+line.

## Notes

- If plan draft/seal HTTP routes are absent daemon-side, adding them is in
  scope (T2) - mirror plan_seal.go RPC semantics exactly.
