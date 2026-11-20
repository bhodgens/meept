# Reject Honesty + Rejected Relay - Implementation Leaf

> **For the implementing agent:** Implement ALL tasks using TDD. Gate tests
> ALREADY EXIST (00T T1/T2) - make them pass; do NOT delete/skip/weaken them.
> Do NOT commit. Do NOT git add. Explore with search_files/terminal cat, not
> read_file on source; write once, never read back. Worktree:
> .hermes/worktrees/usability-remediation.

## Meta

- **Parent:** orchestrator.md
- **Scope:** task.reject must reject (not approve); rejection reaches the user as a terminal
- **Dependencies:** 00T-gate.md
- **Estimated Context:** 60K
- **Concurrency Group:** A
- **Audit references:** W1; F-ORCH-12; queue-gates #9/#10; audit-routing top-10 #1

## Goal

Wire the existing StrategicPlanner.RejectPlan into the RPC handler via a
RejectFunc seam, add HTTP twins, and relay task.rejected to turn.terminal
so a rejected plan ends the user's turn with the reason - never executes.

## Context

- internal/rpc/task_approval.go - handler has only ApproveFunc; handleReject
  (line ~55-73) calls it and returns status:rejected. THE bug.
- internal/agent/strategic.go:2336 - RejectPlan(ctx, taskID, reason) exists:
  sets StateRejected, publishes task.rejected{task_id, reason}. Zero callers.
- internal/daemon/daemon.go:2163 - NewTaskApprovalHandler wiring (pass RejectPlan).
- internal/agent/handler.go:359-360 - subscribes task.completed/task.failed;
  publishTaskTerminalRelay (~2173) recovers originating turn id via
  turnRegistry.TurnIDForTask. Mirror for task.rejected.
- internal/comm/http/server.go - route registration; api_handlers.go - REST
  handlers (mirror plans approve/reject REST shape).
- schemas/ws_events.schema.json - declare new terminal keys (C1).

## Verified Anchors

- internal/rpc/task_approval.go:21 - ApproveFunc field
- internal/rpc/task_approval.go:69 - h.ApproveFunc call inside handleReject
- internal/agent/strategic.go:2336 - RejectPlan signature (ctx, taskID, reason string)
- internal/daemon/daemon.go:2163-2165 - approvalHandler construction+registration
- internal/agent/handler.go:2035 - PublishBlockingT terminal funnel
- internal/agent/handler.go:2202-2205 - TurnIDForTask recovery in relay

## Interface Contracts (From Parent)

### What This Leaf Exposes
- C2: task.reject{task_id,reason} -> {task_id,status:"rejected"};
  POST /api/v1/tasks/{id}/approve + /reject; task.rejected relayed to
  turn.terminal status=failed handler_case=task_rejected_relay.
- C1 keys: rejection_reason (declare in ws_events.schema.json).

### What This Leaf Consumes
- C1 (../master.md) - terminal payload extension shape.

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | RejectFunc seam + handler fix | Modify: internal/rpc/task_approval.go; internal/daemon/daemon.go:2163 region | `go test ./internal/rpc/ -run TestReject -count=1` | gate T1 RED (undefined RejectFunc) | GREEN | Given handler with RejectFunc=RejectPlan; When task.reject; Then RejectPlan(ctx,id,reason) called, ApprovePlan NOT called |
| T2 | task.rejected relay | Modify: internal/agent/handler.go (subscribe + relay case); schemas/ws_events.schema.json | `go test ./internal/agent/ -run TestRejectedRelay -count=1` | gate T2 RED (no subscription) | GREEN | Given tracked turn attached to task; When task.rejected; Then terminal failed/task_rejected_relay/error=reason under originating turn id; completeTurn after emit |
| T3 | HTTP twins | Modify: internal/comm/http/server.go + api_handlers.go (or task handler file) | `go test ./internal/comm/http/ -run TestTaskApproval -count=1` (add test if gate lacks) | routes absent | GREEN | Given awaiting_approval task; When POST /tasks/{id}/reject{reason}; Then 200 + task rejected; approve twin works; unknown id 404 |

## Self-Verification Checklist

- [ ] Gate T1/T2 GREEN; gate diff empty
- [ ] C1/C2 exact; schema declaration present (TestWSSchemaDrift green)
- [ ] Live-gate: rig boot, create plan >=5 steps, task.reject -> task state rejected, ZERO step jobs enqueued, terminal failed with reason
- [ ] No scope creep / artifacts / corruption

**DO NOT COMMIT.** Orchestrator commits after review.

## Completion Report Format

1. Files touched. 2. Verify output (last 20 lines). 3. Live-gate evidence path. 4. Deviations.

## Review Checklist (For Review Agent)

- [ ] Reject never calls ApproveFunc (grep + test)
- [ ] Relay uses originating turn id (never fresh); completeTurn ordering correct
- [ ] Reason reaches user text; HTTP 404 on unknown id
- [ ] Gate tests untouched; conventions; no artifacts

Output: APPROVED or gaps with file+line.

## Notes

- RejectPlan already publishes task.rejected - do NOT duplicate the publish;
  only add the subscriber/relay.
- GUI affordance for these routes is 06-planning/02 (not here).
