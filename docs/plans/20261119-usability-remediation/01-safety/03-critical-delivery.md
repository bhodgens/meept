# Critical Event Delivery + TUI Reconnect - Implementation Leaf

> **For the implementing agent:** Implement ALL tasks using TDD. Gate tests
> ALREADY EXIST (00T T5/T6) - make them pass; do NOT delete/skip/weaken them.
> Do NOT commit. Do NOT git add. Explore with search_files/terminal cat;
> write once, never read back. Worktree: .hermes/worktrees/usability-remediation.

## Meta

- **Parent:** orchestrator.md
- **Scope:** Terminals and task/plan outcomes survive event floods and reconnects
- **Dependencies:** 00T-gate.md
- **Estimated Context:** 75K
- **Concurrency Group:** A
- **Audit references:** W5, W26; F-ORCH-26; F-NEW-3 (serial single-session
  runs now lose terminals: p1 490/590, p2 1390/1490 dropped - top
  observability defect); F-NEW-5 (steer terminal unobservable);
  observability top-10 #1/#9, 5.4

## Goal

Critical topics never trim from the poll buffer; task.completed/failed and
plan terminal events publish blocking; TUI re-subscribes after reconnect
and surfaces dropped counters. A disconnected client can always recover
the turn outcome.

## Context

- internal/rpc/proxy.go:443 - MaxEvents:100 drop-oldest trim; busEventRecord
  ring per subscription; poll returns dropped count.
- internal/bus/bus.go:132-145 - Publish non-blocking select-default;
  PublishBlocking 5s timeout; bus.go:249-258 drop paths.
- internal/agent/handler.go:2035 - ONLY turn.terminal uses PublishBlockingT.
- internal/agent/tactical.go:2341-2354 - task.completed publish (non-blocking).
- internal/plan/manager.go:1907 - publishPlanState m.bus.Publish (non-blocking).
- internal/tui/events.go:151 subscribe(), ~161-163 poll loop, ~244-251 error
  swallow + dropped discard; app.go:1532-1535 ConnectSuccessMsg handling.

## Verified Anchors

- internal/rpc/proxy.go:443 - MaxEvents: 100
- internal/agent/handler.go:2035 - PublishBlockingT(TopicTurnTerminal)
- internal/agent/tactical.go:2341 - task.completed publish site
- internal/plan/manager.go:1907 - publishPlanState
- internal/tui/events.go:151 - func (es *EventStream) subscribe()

## Interface Contracts (From Parent)

### What This Leaf Exposes
- C6: critical ring (cap 256, never trims critical topics); blocking
  publishers for task.completed/failed/rejected + plan.completed/rejected/
  failed; TUI re-subscribe on "subscription not found"; dropped>0 surfaced.

### What This Leaf Consumes
- C1 (../master.md) - terminal key set unchanged here.

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Critical ring in poll buffer | Modify: internal/rpc/proxy.go (busSubscription + collector + poll) | `go test ./internal/rpc/ -run TestCriticalRing -count=1` | gate T5 RED (terminal trimmed) | GREEN | Given 300 progress + 1 terminal; When poll; Then terminal present; seq monotonic across rings; dropped counts progress only |
| T2 | Blocking publishers | Modify: internal/agent/tactical.go (task.completed/failed); internal/plan/manager.go (plan terminal states); internal/agent/handler.go (task.rejected relay uses blocking) | `go test ./internal/bus/ ./internal/agent/ -run TestBlockingPublishers -count=1` | gate T5 RED (non-blocking) | GREEN | Given full subscriber buffer; When task.completed published; Then blocking path (5s bound) not silent drop |
| T3 | TUI reconnect + drop surface | Modify: internal/tui/events.go; internal/tui/app.go (ConnectSuccessMsg) | `go test ./internal/tui/ -run TestResubscribe -count=1` | gate T6 RED (error swallowed) | GREEN | Given poll "subscription not found"; Then subscribe() re-run, events flow; dropped>0 renders a system notice once per episode |

## Self-Verification Checklist

- [ ] Gate T5/T6 GREEN; gate diff empty
- [ ] C6 topic list exact; no new undeclared WS keys
- [ ] Live-gate: rig under 3 concurrent tasks, TUI connected; kill daemon mid-turn, restart; TUI shows terminal (no permanent pending)
- [ ] No scope creep / artifacts / corruption

**DO NOT COMMIT.** Orchestrator commits after review.

## Completion Report Format

1. Files touched. 2. Verify output. 3. Live-gate evidence. 4. Deviations.

## Review Checklist (For Review Agent)

- [ ] Critical ring cannot starve progress polling (bounded 256, overflow counted)
- [ ] Blocking publish timeout path logs Error (never silent)
- [ ] Re-subscribe idempotent (no duplicate subscriptions on flapping)
- [ ] Gate untouched; conventions; no artifacts

Output: APPROVED or gaps with file+line.

## Notes

- plan.failed event itself is CREATED by 01-safety/05 (synthesis failure);
  this leaf only makes its publish blocking once it exists - coordinate via
  C6 topic list; if 05 lands later, add the blocking call in 05's commit.
- Terminal outbox/replay RPC (W25 recovery query) is 01-safety/05 T5.
