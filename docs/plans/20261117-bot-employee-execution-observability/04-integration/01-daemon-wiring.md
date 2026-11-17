# Daemon bus wiring - Implementation Leaf

> **For the implementing agent:** Implement all tasks. Gate test exists and is
> committed; make it pass. Do NOT edit gate test. Do NOT commit. Do NOT use
> read_file; explore with search_files/terminal cat. Write once, then verify.

## Meta

- **Parent:** ./orchestrator.md
- **Scope:** Connect SetBus seams to c.msgBus in NewComponents
- **Dependencies:** 00T-wiring-tests.md, branches 01-03
- **Estimated Context:** 40K
- **Concurrency Group:** A

## Goal

Make both emitters publish on the daemon bus by wiring `c.msgBus` at construction.

## Context

- `internal/daemon/components.go:5345-5358` - bot manager construction
- `internal/daemon/components.go:6740-6810` - goal-loop construction and wiring
- `internal/daemon/components.go:2656` - shared AgentLoop (from branch 03)

## Verified Anchors

- `internal/daemon/components.go:5350` - `botManager := bot.NewManager(botStore, botRouter)`
- `internal/daemon/components.go:6773-6777` - executor adapter construction

## Interface Contracts (From Parent)

### What This Leaf Exposes

- `botManager.SetBus(c.msgBus)` at construction
- goal loop `SetBus(c.msgBus)` at each loop's construction

### What This Leaf Consumes

- C3 `SetBus` (owner: branch 02)

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|----------------|
| T1 | Wire manager bus | Modify: `internal/daemon/components.go` | `go test -p 2 ./internal/daemon/ -run TestComponents_WiresExecutionBus -count=1` | no result event | event received | Given c.msgBus; When botManager constructed; Then SetBus called |
| T2 | Wire goal loops | Modify: `internal/daemon/components.go` | same + `go build ./internal/daemon/` | no employee events | both tiers emit | Given goal loops; When constructed; Then SetBus called on each |

T1: after `bot.NewManager(...)` at :5350, call `botManager.SetBus(c.msgBus)`
with a nil guard on `c.msgBus`. T2: find each GoalLoop construction site in
the employee wiring block (:6740+) and call `loop.SetBus(c.msgBus)` before any
`StartAll`. Do not edit the trigger handler line owned by branch 02 leaf 03.

## Self-Verification Checklist

- [ ] Both tiers wired; nil bus safe
- [ ] No duplicate SetBus calls; no lock held
- [ ] Gate test passes; gofmt clean

**DO NOT COMMIT.**

## Completion Report Format

1. Files touched
2. Verify command + last 20 lines
3. Deviations

## Review Checklist (For Review Agent)

- [ ] Every goal-loop construction site wired
- [ ] No changes to branch 02's trigger handler line

Output: APPROVED or specific gaps.

## Notes

If branch 02 leaf 03 is still unlanded, leave a clear TODO referencing that
leaf and do not edit its line.

