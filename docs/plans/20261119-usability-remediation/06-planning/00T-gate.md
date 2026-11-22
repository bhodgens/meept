# 06-PLANNING Test Gate - Implementation Leaf (TESTS ONLY)

> **For the implementing agent:** Write tests ONLY for this branch's three
> implementation leaves. No implementation code. Every test fails RED for a
> missing-implementation reason. Do NOT commit. Do NOT git add. Explore
> with search_files/terminal cat; write once, never read back. Worktree:
> .hermes/worktrees/usability-remediation.

## Meta

- **Parent:** orchestrator.md
- **Scope:** Gate suite for F13a/F13b/F13c (branch Test Gate matrix)
- **Dependencies:** none
- **Estimated Context:** 65K
- **Concurrency Group:** A
- **Audit references:** W16/W17; F-ORCH-1/13/14/19; queue-gates #7/#8/#11; observability 3.x/4.x

## Goal

Pin approval visibility, threshold config, GUI approval/seal, compiler
draft surface, mode delineation, failure copy, evolver render, and
triviality escape BEFORE implementation.

## Context

- internal/agent/strategic.go:2194-2239 awaitUserApproval publishes
  task.pending_approval (ORPHAN - zero subscribers); :323-324 threshold
  clamps <=0 to 5; max_plan_steps truncation :1673 AFTER gate count.
- internal/tui/app.go:1899 - handles only plan.submitting/completed/
  confirmed/rejected (plans.db family); models/plans.go:189
  plan.list_by_session (0 rows for task-gate).
- ui/flutter_ui/lib/features/plans/plans_tab.dart:499-553 - pending_approval
  buttons ride plans.db REST; NO task.approve/reject callers; no seal.
- internal/rpc/plan_seal.go:93-94/224-227 - plan.draft/plan.seal RPCs;
  plan_draft.go:483 seedDraftFromRequest (raw scaffold).
- internal/agent/handler.go:2392-2426 modeToLabel/FormatEnhancedAsyncTaskAck
  (only mode signal); chat_message_bubble.dart:130-135 mode-chip TODO.
- internal/tui/models/chat.go:3105-3118 planCountLine (evolver fix exists,
  not reused in plans tab :380-386/777-780).
- internal/plan/manager.go failPlan ~1629 (no event); strategic.go
  failTaskWithReason :1905-1929 (honest compile failure exists).

## Verified Anchors

- internal/agent/strategic.go:2230 - task.pending_approval publish
- internal/agent/strategic.go:323 - threshold clamp
- internal/tui/app.go:1899 - plan event handler family
- ui/flutter_ui/lib/features/plans/plans_tab.dart:499 - approval buttons
- internal/rpc/plan_seal.go:93 - plan.draft registration

## Interface Contracts (From Parent)

### What This Leaf Exposes
Gate tests per branch matrix (Go + Dart halves).

### What This Leaf Consumes
- C2 (approval RPC + rejected relay), C7 (exemption), C1 (mode fields),
  C6 (plan.failed).

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Approval card pins (Go) | Create: internal/tui/approval_card_test.go; internal/agent/threshold_config_test.go | `go test ./internal/tui/ ./internal/agent/ -run 'TestPendingApprovalCard|TestThresholdConfig|TestRejectFromCard' -count=1` | orphan topic; hardcoded threshold | GREEN after 01 | Given task.pending_approval; Then TUI card with approve/reject; reject calls C2 (no execution); threshold config 0 disables gate |
| T2 | GUI approval+seal pins (Dart) | Create: ui/flutter_ui/test/features/approval_card_test.dart; seal_surface_test.dart | `cd ui/flutter_ui && flutter test test/features/approval_card_test.dart` | no task.approve callers; no seal | GREEN after 02 | Given awaiting_approval task; Then GUI card approve/reject via C2 HTTP; seal surface shows draft + compile problems inline |
| T3 | Compiler surface pins (Go) | Create: internal/agent/compiler_surface_test.go | `go test ./internal/agent/ -run TestDraftChatSurface -count=1` | raw scaffold parks forever | GREEN after 03 | Given compiler draft seeded; Then chat surface shows draft status + seal action keyed by task_id; raw scaffold never auto-sealed |
| T4 | Mode badge + failure copy pins (Go+Dart) | Create: internal/tui/mode_badge_test.go; internal/agent/plan_failure_copy_test.go; ui/flutter_ui/test/features/mode_chip_test.dart | `go test ./internal/tui/ ./internal/agent/ -run 'TestModeBadge|TestPlanFailureCopy|TestEvolverDocRender|TestTrivialityEscape' -count=1` | no badge; internal failure vocab; literal zeros; over-decomposition | GREEN after 03 | Given plan/direct/recall turn; Then badge distinguishes; plan failure copy actionable (never "max ralph loop"); evolver 0-phase renders "document proposal"; trivial input capped before gate |
| T5 | e2e extensions | Modify: e2e/suites/plan-critic/, plan-lifecycle/, tui-flows/; e2e/manifest.json | `go test -tags e2e ./e2e/suites/plan-critic/... -count=1` | scenarios absent | GREEN after branch | Given harness stack; When approval/seal/failure scenarios; Then contract assertions hold |

## Self-Verification Checklist

- [ ] Every test RED for documented reason (record output)
- [ ] No implementation code; no existing tests weakened
- [ ] C2/C7/C1 shapes exact; reject-never-executes pin present
- [ ] manifest rows complete; matrix F13a-c covered

**DO NOT COMMIT.** Orchestrator handles git after review.

## Completion Report Format

1. Files touched. 2. RED output per task. 3. Deviations.

## Review Checklist (For Review Agent)

- [ ] Tests-only; RED reasons = orphan topic/missing surface/current defect
- [ ] Triviality escape pins gate-count ordering (cap BEFORE gate)
- [ ] Evolver render reuses planCountLine (no new wording fork)
- [ ] No artifacts / corruption

Output: APPROVED or gaps with file+line.

## Notes

- GUI pixel claims operator-pending (Q3); widget tests are the floor.
