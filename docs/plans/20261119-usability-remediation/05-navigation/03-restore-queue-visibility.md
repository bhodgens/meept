# Restore Policy + Queue Parked Visibility - Implementation Leaf

> **For the implementing agent:** Implement ALL tasks using TDD. Gate tests
> ALREADY EXIST (00T T6) - make them pass; do NOT delete/skip/weaken them.
> Do NOT commit. Do NOT git add. Explore with search_files/terminal cat;
> write once, never read back. Worktree: .hermes/worktrees/usability-remediation.

## Meta

- **Parent:** orchestrator.md
- **Scope:** Both clients restore sessions consistently; queue status shows parked/planning work
- **Dependencies:** 00T-gate.md, 01-tui-navigation.md, 02-gui-navigation.md, 03-context/01 (resume contract)
- **Estimated Context:** 60K
- **Concurrency Group:** B
- **Audit references:** C6/C8 report axis-1; B5 queue_status; notes-plan-hitl F2/F3

## Goal

Align TUI/GUI restore policy on the server resume contract (03-context/01
T4): restore last session on relaunch (or explicit empty per auto_resume),
rebuild pending-turn awareness from non-terminal task state; queue_status
reports parked/planning/awaiting_approval tasks (not just running loops).

## Context

- ui/flutter_ui/lib/features/home/home_screen.dart:388-415 reuse-or-create
  empty session; sidebar_home_screen.dart:226-245 autoResume gate (top-tabs
  never reads it); providers/providers.dart:62 activeSessionProvider null.
- internal/tui - resumes most-recent (F-ORCH-7 stale state); session.get_
  most_recent RPC.
- internal/rpc/queue.go:140-146 queue_status {steering_depth,followup_depth,
  is_active,generation} - zeros for parked/planning (B5).
- internal/agent/registry.go GetActiveQueue (C3 resolution now landed).
- 03-context/01 T4 resume contract: last session + pending state + boot-id
  interrupted marker.

## Verified Anchors

- ui/flutter_ui/lib/features/home/home_screen.dart:388 - _onConnectionChanged
- internal/rpc/queue.go:140 - queue_status payload
- internal/tui/models/sessions.go - most-recent resume (re-grep)

## Interface Contracts (From Parent)

### What This Leaf Exposes
- Aligned restore: both clients honor session.auto_resume + server resume
  contract; pending turns rebuilt from non-terminal tasks (interrupted
  marker for stale); queue_status gains task_states []{task_id,state,name}.

### What This Leaf Consumes
- C3 (queue resolution), C4/C7 (resume contract, exempt states), 03-context/01 T4.

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Restore alignment | Modify: home_screen.dart (read autoResume + resume contract), sidebar_home_screen.dart, internal/tui resume path; providers | `go test ./internal/tui/ -run TestRestorePolicyAligned -count=1`; `flutter test test/providers/restore_test.dart` | gate T6 RED (divergent) | GREEN | Given relaunch with last session; Then both clients restore it (auto_resume=true) or explicit empty (false); stale non-terminal tasks show interrupted, pending awareness rebuilt |
| T2 | Queue parked visibility | Modify: internal/rpc/queue.go queue_status (+ task store lookup keyed on unwrapped conv); TUI/GUI queue views render task_states | `go test ./internal/rpc/ -run TestQueueStatusParked -count=1` | gate T6 RED (zeros) | GREEN | Given task parked planning/awaiting_approval; When queue_status; Then task_states lists it (is_active may be false but work visible) |

## Self-Verification Checklist

- [ ] Gate T6 GREEN; gate diff empty
- [ ] Restore never orphans an in-flight turn silently (interrupted marker)
- [ ] queue_status bounded lookup (no full task scan per poll)
- [ ] No scope creep / artifacts / corruption; gofmt + dart format

**DO NOT COMMIT.** Orchestrator commits after review.

## Completion Report Format

1. Files touched. 2. Verify output. 3. Deviations.

## Review Checklist (For Review Agent)

- [ ] auto_resume honored in BOTH layouts (top-tabs + sidebar)
- [ ] Rebuilt pending turns resolve when late terminals arrive
- [ ] queue_status task_states scoped to the session (never cross-session)
- [ ] Gate untouched; conventions; no artifacts

Output: APPROVED or gaps with file+line.

## Notes

- Turn-resume across DAEMON restart (server outbox W25) is 01-safety/05;
  this leaf rebuilds CLIENT awareness from task state, not a turn replay.
