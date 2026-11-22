# 02-ROUTING - Correct Agent, Dir, Session; Visible Evidence; Continuation

> **For the executing agent:** Branch orchestrator. Test gate first,
> validate in-session, then impl leaves; review IN-SESSION; commit after
> review. Do NOT implement code yourself. Worktree:
> .hermes/worktrees/usability-remediation.

## Meta

- **Role:** Branch
- **Parent:** ../master.md
- **Children:** 00T-gate.md + 4 implementation leaves
- **Scope:** Work routes to the correct agent/location/session; routing
  decisions are observable; follow-ups continue prior work; blocked steps
  reschedule.

## Goal

Close W8/W9/W12/W13/W22 + R5/R7 + A2/A3/A4/A5/A6. Maps to F-ORCH-2/3/20/28,
audit-routing R1-R8, session-steering A1-A9. Depends on 01-safety/02 (C3
queue) for continuation and 01-safety/04 (C7) for parked-state awareness.

## Architecture

Four leaves: classifier veto + notice (01, dispatcher.go classification
region), dispatch evidence surface (02, metrics+rpc+http), follow-up
continuation + boot recovery (03, dispatcher task-creation region +
queue_recovery.go), scheduler resilience + dir binding (04, tactical.go +
components.go step-job region + manager.go). 01 and 02 both touch
dispatcher.go but DISJOINT regions (classification arbitration vs
RecordDispatch) - serialize 01 before 02 to be safe (group A then B).
03 touches dispatcher.go task-creation region - group C after 02. 04 is
file-disjoint from all (group A).

## Interface Contracts

Produces C5 (dispatch evidence). Consumes C3 (queue lifecycle, 01-safety/02),
C7 (exempt states, 01-safety/04), C1 (routing_notice key).

## Child Index

| # | Document | Type | Dependencies | Est. Context | Concurrency |
|---|----------|------|--------------|--------------|-------------|
| 00T | 00T-gate.md | leaf (tests) | none | 65K | A |
| 01 | 01-veto-and-notice.md | leaf | 00T | 70K | A |
| 02 | 02-dispatch-evidence.md | leaf | 00T, 01 | 65K | B |
| 03 | 03-followup-continuation.md | leaf | 00T, 01-safety/02, 02 | 85K | C |
| 04 | 04-scheduler-resilience.md | leaf | 00T | 90K | A |

## Dispatch Protocol

Phase 0: 00T gate (RED), validate in-session, commit, TESTS_VALIDATED.
Phase 1: 01 + 04 together (file-disjoint). Phase 2: 02 (after 01 REVIEWED).
Phase 3: 03 (after 02 + 01-safety/02 REVIEWED). Review in-session per leaf;
commit exact paths; max 3 cycles then BLOCKED. Briefs carry worktree path,
consumed contracts, no-commit/no-git-add/no-read_file/write-once rules.
04 STARTS WITH INSTRUMENTATION (I1) - lease counters + controlled
concurrency repro BEFORE any fix; if the leak site is not proven, report
findings and fix only the proven path (bounded retry ticker is safe
regardless).

## Review Checklist

- [ ] All leaf tasks implemented; verify commands pass
- [ ] C5 exact; routing_notice declared in ws schema (C1)
- [ ] Gate tests GREEN and unmodified
- [ ] Frozen p1/p6 prompts route correctly on rig (live-gate); regression
      suite routing tasks still green (no over-veto)
- [ ] No scope creep / artifacts / corruption; gofmt clean

## Coding Conventions

Per ../master.md. Branch specifics: veto changes must keep F41 branch
ORDER (recall matchers first); classification changes run the GEPA
validation gate (tools/gepa-tune --run) if the classifier PROMPT changes
(veto logic is dispatcher-side, not prompt - GEPA not required for 01);
dispatch_log schema migration idempotent ALTER + roundtrip pin.

## Completion Tracking Table

| Child | Status | Iterations | Review Notes |
|-------|--------|------------|--------------|
| 00T-gate | PENDING | 0 | |
| 01-veto-and-notice | PENDING | 0 | |
| 02-dispatch-evidence | PENDING | 0 | |
| 03-followup-continuation | PENDING | 0 | |
| 04-scheduler-resilience | PENDING | 0 | |

## Integration Test Plan

1. `go test -p 2 ./internal/agent/... ./internal/metrics/... ./internal/rpc/...`.
2. e2e dispatcher + dispatch-trace + plan-lifecycle suites.
3. suites/regression.json on rig (routing tasks must stay green).
4. Live rig: p1 naive + p6 control probes - edit prompts route coder not
   committer; dispatch_trace queryable by session_id AND conversation_id
   with turn_id+input_summary; follow-up to parked planning task refines
   draft (no new task); 8 concurrent sessions no semaphore hang.

## Test Gate

Test leaf: 00T-gate.md. Matrix (root F6-F8):

| # | Feature | Source | Unit tests | E2E tests | Status |
|---|---------|--------|-----------|-----------|--------|
| F6 | veto + dir binding + reschedule | 01 T1-T2, 04 T1-T3 | TestGitVetoShapeIndependent, TestNoSingletonDirMutation, TestBlockedStepRetry, TestStepJobDirBinding | e2e/suites/dispatcher (extend) | PENDING |
| F7 | dispatch evidence + notice | 02 T1-T3, 01 T3 | TestDualKeyTrace, TestTraceTurnID, TestNoticePopulated | e2e/suites/dispatch-trace (new) | PENDING |
| F8 | followup continuation + boot recovery | 03 T1-T3 | TestDraftRefinementRoute, TestBootFollowupReenqueue, TestThreadResolveOnce | e2e/suites/plan-lifecycle (extend) | PENDING |

Delta log: (none yet)

## Open Questions

- Q: veto widening may over-veto genuine git requests phrased without a
  git verb ("save my changes"). Guard: inputContainsGitVerb stays the
  positive signal; add regression cases for both directions.

## Notes

- Anchor drift: dispatcher.go moved ~15 lines between 1cee89d7 and
  ff19f10f (veto now ~1775-1796). Re-grep at dispatch.
- researcher capability correction: web_fetch+file_write EXISTS at HEAD;
  the fetch+write bench failure is hint emission (01 T2 covers hint
  validation), not agent capabilities.
