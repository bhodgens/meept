# Goal Transition Fence + Retire Semantics - Implementation Leaf

> **For the implementing agent:** Implement ALL tasks with TDD. Gate tests exist and fail RED. Do NOT commit. Do NOT use read_file on existing sources.

## Meta

- **Parent:** ../orchestrator.md
- **Scope:** Goal state transitions enforced at the store (M-10); blocked->active only via the human-gated path; retirement consistent (M-11); paused goals skip assessment writes (F16).
- **Dependencies:** none (00T provides tests)
- **Estimated Context:** 45K
- **Concurrency Group:** C
- **Audit references:** M-10, M-11, F16, goal F5

## Goal

The "durable blocked phase" is enforced by the store, not convention;
retired goals always have retired_at; deleting an employee soft-deletes
goals (Q3 default) instead of hard-DELETE.

## Context

- internal/employee/goal.go: GoalState enum :41-50, Update :890 (retired_at from g.RetiredAt), Retire :920 (DEAD CODE - zero callers), retireStmt :1023, ParseGoalState (unknown->active+error), IsRetired :543
- internal/employee/goal_tools.go:459-476 (goal_update tool: resume/complete/blocked actions; requireDirectHuman gate)
- internal/employee/goal_round_driver.go:437 (isArmed fence), :483 (BlockGoal), :543 (UpdateGoal caller)
- internal/employee/manager.go:1361-1368 (employee Retire hard-DELETEs goals + audit findings)
- internal/employee/intake/linkage.go:209 (UpdateGoal from intake write-back)

## Verified Anchors

Re-grep at dispatch. NOTE: internal/employee files are dirty in a sibling
session - this leaf runs against committed main in the isolation worktree;
re-anchor at dispatch and re-check at merge time.

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
var ErrGoalTransition = errors.New("employee: invalid goal state transition")
// validFrom map in goal.go:
//   active -> {paused, blocked, retired}
//   paused -> {active, retired, blocked}
//   blocked -> {active, retired}         // active ONLY via human-gated resume path
//   retired -> {}                        // terminal
// GoalStore.Update validates transitions; a bypass flag exists ONLY for the goal_update tool's human-gated resume/complete actions:
func (s *GoalStore) UpdateWithTransition(ctx context.Context, g *Goal, allowHumanBypass bool) error
```

### What This Leaf Consumes

- Existing goal store Update; keep it as the internal raw write (unexported callers) or validate inside Update and add UncheckedUpdate for migration paths - survey call sites and pick the shape that touches fewest files.

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|----------------|
| T1 | Transition fence | Modify: internal/employee/goal.go | gate TestGoalTransitionFence GREEN | RED | GREEN: Update blocked->active returns ErrGoalTransition; tool resume path (bypass) succeeds; retired->anything rejected | Given blocked goal; When internal caller Updates to active; Then error. When goal_update tool resumes (human-gated); Then active |
| T2 | Tool path uses the fence explicitly | Modify: internal/employee/goal_tools.go (:459-476) | same test | - | resume/complete are the ONLY bypass callers; grep-provable | - |
| T3 | retired_at consistency | Modify: goal.go Update + goal_tools complete action (set RetiredAt) | gate TestRetireSetsRetiredAt GREEN | RED | GREEN: every retired-state row has retired_at; IsRetired true | Given tool "complete"; When goal retired; Then retired_at set, IsRetired true |
| T4 | Employee delete -> soft-delete | Modify: manager.go (:1368 region): goal DELETE -> retire-all (state=retired, retired_at=now, preserve rows); keep audit findings delete? NO - also soft-handle: mark findings stale, keep rows (Q3 default: audit trail preserved) | `go test -p 2 ./internal/employee/` | existing behavior (hard delete) | GREEN; old Retire dead code REMOVED (goal.go:920-932, :1023) per repo dead-code rule | Given employee with 2 goals; When manager.Retire; Then goals remain, state=retired, retired_at set |
| T5 | Paused-goal write fence | Modify: goal_round_driver / assessment write path (F16: OnRoundSettled + intake write-back skip non-active goals) | gate TestPausedGoalSkipsAssessmentWrites GREEN | RED | GREEN: paused/blocked goals receive NO assessment writes (health/findings/counters); round admission already fenced | Given paused goal; When round settles; Then row unchanged |

## Self-Verification Checklist

- [ ] Gate tests GREEN, untouched
- [ ] Dead code removed (Retire :920) - repo rule
- [ ] ParseGoalState unknown->active+error: check callers HANDLE the error; if any ignore it, tighten to reject (report if risky)

**DO NOT COMMIT.**

## Completion Report Format

Files; verify tail; deviations.

## Review Checklist (For Review Agent)

- [ ] validFrom table matches audited machine; no hidden bypass callers (grep Update( in employee)
- [ ] Soft-delete keeps audit rows; nothing hard-DELETEs goals anymore
- [ ] T5 does not block legitimate state-change writes (only assessment/health writes)

## Notes

Sibling-session collision risk is high in this package. If merge conflicts
appear at integration, the orchestrator reconciles against landed main -
the fence contract (C-P3) is the fixed point.
