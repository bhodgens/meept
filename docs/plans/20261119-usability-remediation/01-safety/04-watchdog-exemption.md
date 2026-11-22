# Watchdog Park Exemption - Implementation Leaf

> **For the implementing agent:** Implement ALL tasks using TDD. Gate tests
> ALREADY EXIST (00T T7) - make them pass; do NOT delete/skip/weaken them.
> Do NOT commit. Do NOT git add. Explore with search_files/terminal cat;
> write once, never read back. Worktree: .hermes/worktrees/usability-remediation.

## Meta

- **Parent:** orchestrator.md
- **Scope:** Human-approval waits are never falsely reaped; activity heartbeats liveness
- **Dependencies:** 00T-gate.md
- **Estimated Context:** 50K
- **Concurrency Group:** A
- **Audit references:** W6; queue-gates #7/#11; audit-routing top-10 #9;
  F-ORCH-13 correlate; F-NEW-6 (10m false-reap reproduced 3x in GUI rerun)

## Goal

Exclude planning/awaiting_approval/awaiting_seal turns from watchdog
staleness; Touch liveness on draft/seal/approval activity. Eliminates the
parked -> failed(reaped) -> completed wrong-story sequence.

## Context

- internal/agent/turn_watchdog.go - tick loop reaps tracked turns stale
  > stale_after (600s default, schema.go:4952); emits turn.terminal failed
  handler_case=turn_reaped.
- internal/agent/handler.go - turn registry edges (AttachTask/TurnIDForTask);
  liveness Touch sites (~1987-1991 worker lifecycle, ~2631 task.progress).
- internal/agent/strategic.go:2194-2239 - awaitUserApproval sets
  StateAwaitingApproval; plan_draft.go SaveDraft/SealDraft activity sites.
- internal/task states: StatePlanning, StateAwaitingApproval (+ seal wait
  represented by planning+draft metadata).

## Verified Anchors

- internal/agent/turn_watchdog.go:129-150 - reap loop + terminal emit (audit base lines; re-grep)
- internal/config/schema.go:4952 - stale_after default 600
- internal/agent/strategic.go:2217 - SetState(StateAwaitingApproval)
- internal/agent/plan_draft.go:115 - SaveDraft

## Interface Contracts (From Parent)

### What This Leaf Exposes
- C7: exemption states planning|awaiting_approval|awaiting_seal; Touch on
  draft/seal/approval activity.

### What This Leaf Consumes
- C2 (01-reject-honesty) - rejected tasks terminalize via relay, not watchdog.

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Exemption in reap loop | Modify: internal/agent/turn_watchdog.go (+ registry edge state accessor if needed) | `go test ./internal/agent/ -run TestApprovalWaitNotReaped -count=1` | gate T7 RED (reaps today) | GREEN | Given tracked turn, task awaiting_approval, stale 700s; When tick; Then not reaped; executing task still reaped normally |
| T2 | Activity Touch | Modify: internal/agent/plan_draft.go (SaveDraft/SealDraft); strategic.go approval activity; handler.go Touch wiring | `go test ./internal/agent/ -run TestSealActivityTouch -count=1` | gate T7 RED (no touch) | GREEN | Given awaiting_seal turn; When draft saved; Then liveness refreshed |

## Self-Verification Checklist

- [ ] Gate T7 GREEN; gate diff empty
- [ ] C7 states exact; quota/throttle park semantics unchanged
- [ ] Live-gate: rig plan turn waits >600s (raise stale_after to 30s for the test), no reap; approve later -> single completed terminal
- [ ] No scope creep / artifacts / corruption

**DO NOT COMMIT.** Orchestrator commits after review.

## Completion Report Format

1. Files touched. 2. Verify output. 3. Live-gate evidence. 4. Deviations.

## Review Checklist (For Review Agent)

- [ ] Exemption reads state via registry edge (no per-tick store query storm)
- [ ] Truly stalled executing turns STILL reaped (no blanket disable)
- [ ] Late-after-reap double terminal impossible for exempt states
- [ ] Gate untouched; conventions; no artifacts

Output: APPROVED or gaps with file+line.

## Notes

- Goal-gate park stranding (W27) is 01-safety/05 T4 - different mechanism
  (gate_park release pump), not watchdog.
