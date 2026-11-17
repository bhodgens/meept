# Wiring gate test - Implementation Leaf

> **For the implementing agent:** Write TESTS ONLY. Do NOT write implementation
> files. Test must fail RED. Do NOT commit. Do NOT use read_file; explore with
> search_files/terminal cat. Write once and stop.

## Meta

- **Parent:** ./orchestrator.md
- **Scope:** RED test for daemon bus wiring on emitters
- **Dependencies:** none
- **Estimated Context:** 30K
- **Concurrency Group:** A

## Goal

Pin F7: `NewComponents` / goal-loop construction calls `SetBus(c.msgBus)` on
bot manager and goal loops.

## Context

- `internal/daemon/components.go:5345-5358` - bot store/router/manager construction
- `internal/daemon/components.go:6740-6810` - goal-loop construction
- `internal/daemon/employee_message_tools_test.go:12-40` - Components test shape

## Verified Anchors

- `internal/daemon/components.go:5350` - `botManager := bot.NewManager(...)`
- `internal/daemon/components.go:6765-6778` - executor + speak router wiring

## Interface Contracts (From Parent)

### What This Leaf Exposes

- `internal/daemon/execution_bus_wiring_test.go`

### What This Leaf Consumes

- C3 `SetBus` seams (owner: branch 02)

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|----------------|
| T1 | Wiring assertion | Create: `internal/daemon/execution_bus_wiring_test.go` | `go test -p 2 ./internal/daemon/ -run TestComponents_WiresExecutionBus -count=1` | SetBus not observable | bus wired | Given a Components with msgBus; When constructed; Then a trigger emits a result event on that bus |

T1 should drive the real wiring path with a scratch `bot.Manager` and verify
a result event lands on the wired bus. Prefer a behavioral assertion (event
received) over source grep.

## Self-Verification Checklist

- [ ] Test RED for missing wiring, not syntax
- [ ] No implementation files touched

**DO NOT COMMIT.**

## Completion Report Format

1. Files touched
2. Verify command + last 20 lines
3. Deviations

## Review Checklist (For Review Agent)

- [ ] Behavioral assertion, not source inspection
- [ ] No gate test weakened later

Output: APPROVED or specific gaps.

## Notes

None.
