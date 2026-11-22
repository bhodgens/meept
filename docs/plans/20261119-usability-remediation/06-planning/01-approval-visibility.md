# Approval Visibility + Threshold Config - Implementation Leaf

> **For the implementing agent:** Implement ALL tasks using TDD. Gate tests
> ALREADY EXIST (00T T1) - make them pass; do NOT delete/skip/weaken them.
> Do NOT commit. Do NOT git add. Explore with search_files/terminal cat;
> write once, never read back. Worktree: .hermes/worktrees/usability-remediation.

## Meta

- **Parent:** orchestrator.md
- **Scope:** The strategic approval gate is visible and actionable in the TUI; threshold configurable
- **Dependencies:** 00T-gate.md, 01-safety/01 (C2), 01-safety/04 (C7)
- **Estimated Context:** 85K
- **Concurrency Group:** A
- **Audit references:** W16; F-ORCH-13/19; P2; queue-gates #11; observability top-10 #3

## Goal

Subscribe task.pending_approval in the TUI chat layer; render an approval
card (task name, step count, approve/reject buttons calling C2 RPCs);
expose the threshold as config (0 disables); make the plans view show
task-gate plans (not just plans.db).

## Context

- internal/agent/strategic.go:2230 - task.pending_approval{task_id,
  session_id, steps, total} (orphan today).
- internal/tui/app.go:1899 - plan event handler family (add task gate);
  models/plans.go:189 plan.list_by_session; models/chat.go:3105-3118
  planCountLine + renderPlanNotification pattern to mirror.
- internal/rpc/task_approval.go - task.approve/task.reject (C2, RejectFunc
  landed by 01-safety/01); internal/tui/rpc.go:917-930 ApproveTask/RejectTask
  (exist, zero TUI callers - wire them).
- internal/agent/strategic.go:323-324 - threshold clamp; StrategicPlannerConfig
  .ApprovalStepThreshold; components.go:4606 construction (never sets it).
- internal/config/schema.go - orchestrator section (add task_approval_step_threshold).

## Verified Anchors

- internal/agent/strategic.go:2230 - pending_approval publish
- internal/agent/strategic.go:323 - clamp
- internal/tui/app.go:1899 - plan event family
- internal/tui/rpc.go:921 - ApproveTask
- internal/daemon/components.go:4606 - StrategicPlannerConfig construction

## Interface Contracts (From Parent)

### What This Leaf Exposes
- Approval card contract: task.pending_approval -> TUI card keyed by
  task_id; approve -> task.approve, reject -> task.reject{reason} (C2);
  orchestrator.task_approval_step_threshold int (default 5, 0 disables).

### What This Leaf Consumes
- C2 (approval RPC + rejected relay), C7 (exempt states), C1 (terminal).

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | TUI approval card | Modify: internal/tui/app.go (subscribe task.pending_approval + card msg), models/chat.go (card render + approve/reject keys), rpc.go wiring | `go test ./internal/tui/ -run 'TestPendingApprovalCard|TestRejectFromCard' -count=1` | gate T1 RED (orphan) | GREEN | Given pending_approval event for active session; Then card with name/steps/approve/reject; reject calls task.reject (C2, no execution); approve resumes |
| T2 | Threshold config | Modify: internal/config/schema.go; internal/daemon/components.go:4606 region; internal/agent/strategic.go clamp (0 = disabled, not clamp-to-5) | `go test ./internal/agent/ -run TestThresholdConfig -count=1` | gate T1 RED (hardcoded) | GREEN | Given threshold=0; Then no approval gate; =N gates >=N; default 5 preserved; plans.approval.require_approval still controls plans.db path only |
| T3 | Plans view task-gate rows | Modify: internal/tui/models/plans.go (merge task-gate awaiting tasks into list) | `go test ./internal/tui/ -run TestPlansViewTaskGate -count=1` (add) | 0 rows today | GREEN | Given awaiting_approval task; Then plans view shows it with approve/reject affordance (not empty) |

## Self-Verification Checklist

- [ ] Gate T1 GREEN; gate diff empty
- [ ] C2 exact; reject from card never executes (live-gate)
- [ ] Live-gate: rig plan >=5 steps -> TUI card visible -> approve executes / reject terminal
- [ ] No scope creep / artifacts / corruption; gofmt clean

**DO NOT COMMIT.** Orchestrator commits after review.

## Completion Report Format

1. Files touched. 2. Verify output. 3. Live-gate evidence. 4. Deviations.

## Review Checklist (For Review Agent)

- [ ] Card scoped to owning session (never cross-session flood)
- [ ] Threshold 0 truly disables (no clamp resurrection)
- [ ] Reject reason dialog passes reason through C2
- [ ] Gate untouched; conventions; no artifacts

Output: APPROVED or gaps with file+line.

## Notes

- GUI card is 06-planning/02 (consumes C2 HTTP routes from 01-safety/01).
- Q1 ratification: key name orchestrator.task_approval_step_threshold.
