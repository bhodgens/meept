# 06-PLANNING - Visible Approval, Seal Surface, Delineation, Clean Failure

> **For the executing agent:** Branch orchestrator. Test gate first,
> validate in-session, then impl leaves; review IN-SESSION; commit after
> review. Do NOT implement code yourself. Worktree:
> .hermes/worktrees/usability-remediation.

## Meta

- **Role:** Branch
- **Parent:** ../master.md
- **Children:** 00T-gate.md + 3 implementation leaves
- **Scope:** Planning vs direct is visible; approval/seal actionable in both
  clients; plan failures end honestly.

## Goal

Close W16/W17 + P1-P9 (report axis 5): F-ORCH-1/13/14/19, queue-gates
#7/#8/#11, observability 3.x/4.x. Make the strategic task gate visible and
approvable/rejectable in TUI+GUI; give the plan-compiler draft a chat
surface; delineate mode; clean failure copy; evolver document-proposal
render; triviality escape for over-decomposition.

## Architecture

Three leaves: approval visibility + threshold config (01, daemon event
relay + TUI card + config), GUI approval/seal surface (02, Flutter),
compiler draft chat surface + delineation + failure copy (03, dispatcher/
strategic + both clients' mode signal). 01 owns daemon+TUI; 02 owns GUI
(file-disjoint, group A parallel after C2 lands). 03 touches dispatcher.go
plan region + strategic.go - group B after 02-routing/03 (continuation)
and 01. Depends on 01-safety/01 (C2 reject honesty) + 01-safety/04 (C7).

## Interface Contracts

Consumes C2 (approval RPC + rejected relay), C7 (watchdog exemption), C1
(terminal keys), C6 (plan.failed blocking). Produces: approval card
contract (task.pending_approval -> TUI/GUI card with approve/reject calling
C2), seal surface (plan.draft/plan.seal from clients), mode signal
(intent_type/handler_case badge), threshold config key.

## Child Index

| # | Document | Type | Dependencies | Est. Context | Concurrency |
|---|----------|------|--------------|--------------|-------------|
| 00T | 00T-gate.md | leaf (tests) | none | 65K | A |
| 01 | 01-approval-visibility.md | leaf | 00T, 01-safety/01, 01-safety/04 | 85K | A |
| 02 | 02-gui-approval-seal.md | leaf | 00T, 01-safety/01 (C2), 01 | 80K | A |
| 03 | 03-compiler-surface-delineation.md | leaf | 00T, 02-routing/03, 01 | 95K | B |

## Dispatch Protocol

Phase 0: 00T gate RED, validate in-session, commit, TESTS_VALIDATED.
Phase 1: 01 + 02 together (Go vs Dart disjoint; 02 consumes C2 HTTP routes
from 01-safety/01). Phase 2: 03 (after 02-routing/03 + 01 REVIEWED). Review
in-session; commit exact paths; max 3 cycles then BLOCKED. Briefs: worktree
path, consumed contracts, no-commit/no-git-add/no-read_file/write-once.
Q1 (master): threshold key orchestrator.task_approval_step_threshold
default 5, 0 disables - ratify at 01 dispatch.

## Review Checklist

- [ ] All leaf tasks implemented; verify commands pass
- [ ] Gate tests GREEN and unmodified
- [ ] Approval card renders for task.pending_approval in BOTH clients;
      approve executes, reject does NOT (C2 pin)
- [ ] Seal surface shows draft + compile problems; raw scaffold never sealed
- [ ] Mode badge distinguishes plan/quickplan/direct/recall; failure copy actionable
- [ ] No scope creep / artifacts / corruption; gofmt + dart format

## Coding Conventions

Per ../master.md. Branch specifics: approval cards keyed by task_id (C2);
seal calls plan.draft/plan.seal RPCs (existing); compile problems rendered
inline (line+message); evolver 0-phase reuse planCountLine wording.

## Completion Tracking Table

| Child | Status | Iterations | Review Notes |
|-------|--------|------------|--------------|
| 00T-gate | PENDING | 0 | |
| 01-approval-visibility | PENDING | 0 | |
| 02-gui-approval-seal | PENDING | 0 | |
| 03-compiler-surface-delineation | PENDING | 0 | |

## Integration Test Plan

1. `go test -p 2 ./internal/agent/... ./internal/rpc/... ./internal/tui/...`;
   flutter test.
2. e2e plan-critic + plan-lifecycle + tui-flows suites.
3. Live rig: plan request >=5 steps -> approval card visible -> approve ->
   executes; reject -> rejected terminal, no execution; compiler draft ->
   seal surface -> compile problems shown; trivial question -> direct badge.

## Test Gate

Test leaf: 00T-gate.md. Matrix (root F13):

| # | Feature | Source | Unit tests | E2E tests | Status |
|---|---------|--------|-----------|-----------|--------|
| F13a | approval visibility + threshold | 01 T1-T3 | TestPendingApprovalCard, TestThresholdConfig, TestRejectFromCard | e2e/suites/plan-critic (extend) | PENDING |
| F13b | GUI approval + seal | 02 T1-T3 | TestGUIApprovalCard, TestGUISealSurface, TestCompileProblemsInline | widget tests + gui parity | PENDING |
| F13c | compiler surface + delineation + failure copy | 03 T1-T4 | TestDraftChatSurface, TestModeBadge, TestPlanFailureCopy, TestEvolverDocRender, TestTrivialityEscape | e2e/plan-lifecycle + tui-flows | PENDING |

Delta log: (none yet)

## Open Questions

- Q1 threshold key ratification (recommend orchestrator.task_approval_step_threshold).
- Compiler brainstorm auto-fill (03 T1 option B) vs explicit seal card
  (option A): recommend A first (visible human seal), B as follow-up.

## Notes

- P4 over-decomposition: triviality escape (03 T4) caps plan steps for
  trivial inputs BEFORE the gate counts them (gate counted 21 with
  max_plan_steps=4 - truncation happens after gate today; fix ordering).
