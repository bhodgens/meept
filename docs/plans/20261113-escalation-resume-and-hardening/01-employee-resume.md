# 01 Employee-Side Resume (Manager.ResumeApprovedPlan) - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. The gate tests ALREADY EXIST and are
> committed - your job is to make them pass; do NOT delete, skip, weaken,
> or rewrite any gate test (report suspected-wrong gate tests to the
> orchestrator instead). Do NOT commit - the orchestrator handles all git
> operations after review. Do NOT use read_file on existing source files -
> explore with search_files or terminal cat. After writing a file, do NOT
> read it back - write once and stop.

## Meta

- **Parent:** master.md
- **Scope:** Add Manager.ResumeApprovedPlan (Contract C1) with once/busy/unknown guards, and record park-resumed plans into the once-map.
- **Dependencies:** 00T gate tests committed (RED)
- **Estimated Context:** 60K
- **Concurrency Group:** A
- **Audit references:** escalation-resume gap (manager.go:2310-2333 false comment)

## Goal

Give the employee Manager the missing entrypoint the daemon subscriber (leaf 02) will call: resume an APPROVED plan on the owning employee's GoalLoop, exactly once per process, skipping cleanly when the loop is busy, the plan is unknown, or the park-episode path already ran it. Fix the false comment at manager.go:2310-2333 so the code no longer claims a driver that does not exist (leaf 02 supplies the real driver).

## Context

- internal/employee/manager.go — Manager holds goals, bots (loops), and planDisposer. ApprovePlan (:2312) stamps goal.SetActivePlan(planID) (:2338) and calls the disposer; it does NOT execute.
- internal/employee/goal_loop.go:2003 — ApproveAndExecute(ctx, planRef) (*bot.BotExecutionResult, GoalHealth, error) exists; called ONLY from internal/employee/park.go:396-405. DO NOT EDIT goal_loop.go.
- internal/employee/park.go:396-405 — parked-episode "approve" phase resume; ignore result/health, warn-logs err.
- Drive model: GoalLoop has no ticker; rounds run via Manager.Trigger -> triggerViaGoalLoop -> loop.Decide. Assess reads the goal store, not the plan store.
- Access to the plan record: internal/employee cannot import internal/plan. The plan-to-goal linkage must resolve through existing seams (the goal store's ActivePlanIDs, internal/employee/goal.go:257/496-527, is authoritative G2) or a pre-existing injected lookup seam. Survey first; if Manager already holds a plan-lookup seam (PlanDisposer or another adapter), use it; otherwise resolve by scanning goals for ActivePlanIDs containment.

## Verified Anchors

- internal/employee/manager.go:2312-2374 - ApprovePlan - disposer call :2322, ActivePlanID stamp :2338, pending constitution patch :2344-2366
- internal/employee/manager.go:1615/1640 - triggerViaGoalLoop - how Manager reaches a live GoalLoop (mirror this resolution for the resume path)
- internal/employee/goal_loop.go:2003-2053 - ApproveAndExecute - AddActivePlan before Execute, RemoveActivePlan + AppendHistory after
- internal/employee/park.go:396-405 - park resume caller - needs once-map recording added here (T4)
- internal/employee/goal.go:496-527 - SetActivePlan (mirror [0]) vs AddActivePlan (dedup+append, authoritative)
- internal/employee/goal_loop_test.go:771-807 - existing ApproveAndExecute tests - keep green

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
func (m *Manager) ResumeApprovedPlan(ctx context.Context, planID, callerID string) error
// Contract C1: deliberate skips (unknown plan, unknown goal owner, no live
// GoalLoop, loop busy, already resumed this process) return nil + INFO log.
// Real failures (store errors, ApproveAndExecute error) return wrapped error.
// Once-per-process map: mutex-guarded map[string]struct{} keyed by planID.
```

Also: a small exported-for-test seam is FORBIDDEN — test through the Manager public API with existing test doubles (the patterns in goal_loop_test.go).

### What This Leaf Consumes

- GoalLoop.ApproveAndExecute — existing, do not change
- Manager goal/bot resolution seams — existing (triggerViaGoalLoop precedent)

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Implement ResumeApprovedPlan happy path: resolve goal owner for planID, resolve live GoalLoop, call ApproveAndExecute(ctx, &PlanRef{ID: planID}), record planID in once-map, log INFO with employee_id | Modify: internal/employee/manager.go (new method near ApprovePlan); Test: gate file internal/employee/manager_resume_test.go (already committed) | `go test -p 2 ./internal/employee/ -run TestManager_ResumeApprovedPlan_Executes` | gate test RED (undefined method) | GREEN | Given approved plan + live loop; When resume; Then ApproveAndExecute called once, nil error |
| T2 | Unknown-plan guard: lookup miss -> INFO + nil | same file | `go test -p 2 ./internal/employee/ -run TestManager_ResumeApprovedPlan_UnknownPlanSkips` | RED | GREEN | Given unknown planID; When resume; Then nil, no execution |
| T3 | Busy guard: loop already mid-round -> INFO + nil, NO queueing (next scheduled Trigger handles normal rounds) | same file | `go test -p 2 ./internal/employee/ -run TestManager_ResumeApprovedPlan_BusyLoopSkips` | RED | GREEN | Given busy loop; When resume; Then nil, no ApproveAndExecute |
| T4 | Once-map + park interplay: add recording at park.go resume success site (park.go:396-405); second resume for same planID -> INFO + nil | Modify: internal/employee/park.go (one recording call); Test: gate _DoubleResumeSkips | `go test -p 2 ./internal/employee/ -run TestManager_ResumeApprovedPlan_DoubleResumeSkips` | RED | GREEN | Given park already executed the plan; When bus resume arrives; Then nil, count stays 1 |
| T5 | Fix the false comment at manager.go:2310-2333: replace "GoalLoop driver picks it up / sees the approved state on its next Assess" with text naming the actual resume seam (ResumeApprovedPlan via the daemon subscriber, leaf 02, and the parked-episode path). Comment-only, no behavior change | Modify: internal/employee/manager.go (comment block only) | `go build ./internal/employee/` | n/a (comment) | builds; comment accurate | Given stale comment; When read; Then it names the real resume paths |

## Self-Verification Checklist

- [ ] All tasks implemented; gate tests T1-T4 GREEN
- [ ] Contract C1 signature matches exactly (ctx, planID, callerID)
- [ ] goal_loop.go UNTOUCHED; only manager.go + park.go modified
- [ ] Skips return nil with INFO logs; errors wrapped with %w
- [ ] No new exported symbols beyond ResumeApprovedPlan
- [ ] mutex not held across I/O (snapshot-under-lock pattern)
- [ ] Existing tests green: `go test -p 2 ./internal/employee/ -run 'TestApproveAndExecute|TestGoalLoop_Tier2'`

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

## Completion Report Format

1. **Files touched:** [paths]
2. **Verify command + output:** [last 20 lines]
3. **Deviations:** [none / list with rationale]

## Review Checklist (For Review Agent)

- [ ] Contract C1 exact; guards all present (unknown/busy/once)
- [ ] park.go recording is exactly one call at the success site; once-guard map delete-vs-retained decision documented in code comment (retained: process-lifetime)
- [ ] goal_loop.go untouched; gate tests unmodified (git diff on gate file empty)
- [ ] Comment fix at :2310-2333 accurate
- [ ] No debug artifacts; no line-number corruption

Output: APPROVED or specific gaps with file + line references.

## Notes

- The busy guard should mirror the DecideSkip/acquireInvokeMutex posture of triggerViaGoalLoop (manager.go:1615) — reuse the same mutex/semantics rather than inventing a second busy concept.
- PlanRef construction: match the shape park.go passes to ApproveAndExecute (*PlanRef); check goal_loop.go:2014 for what fields AddActivePlan reads.
