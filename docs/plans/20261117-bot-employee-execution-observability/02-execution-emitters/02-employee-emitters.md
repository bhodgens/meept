# Employee execution emitters - Implementation Leaf

> **For the implementing agent:** Implement all tasks. Gate tests exist and are
> committed; make them pass. Do NOT edit gate tests. Do NOT commit. Do NOT use
> read_file; explore with search_files/terminal cat. Write once, then verify.

## Meta

- **Parent:** ./orchestrator.md
- **Scope:** Add bus seam + result/turn-state emission to GoalLoop.Execute
- **Dependencies:** 00T-emitter-tests.md
- **Estimated Context:** 45K
- **Concurrency Group:** A

## Goal

Publish C1/C2 from every employee round on `employee.result` with
`source:"employee"`.

## Context

- `internal/employee/goal_loop.go:976-1028` - Execute method + result construction
- `internal/employee/goal_loop.go:1788,2066,2126` - other Execute call sites
  (emit inside `Execute` itself so all sites are covered)
- `internal/employee/goal_loop.go:388-431` - GoalLoop struct + mutex fields
- `internal/agent/coordination.go:42-56` - frozen turn-state shape

## Verified Anchors

- `internal/employee/goal_loop.go:1005` - `output, tokens, err := runner.ExecuteBot(...)`
- `internal/employee/goal_loop.go:1008-1028` - result build + success/failure
- `internal/employee/goal_loop.go:596-626` - `notifySpeakRound` (do not replace)

## Interface Contracts (From Parent)

### What This Leaf Exposes

- `func (l *GoalLoop) SetBus(b *bus.MessageBus)` (C3)

### What This Leaf Consumes

- C1/C2 typed topics; `agent.TopicEmployeeResult`
- `agent.StopReasonEndTurn`, `agent.StopReasonError`

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|----------------|
| T1 | Add bus seam | Modify: `internal/employee/goal_loop.go` | `go test -p 2 ./internal/employee/ -run TestGoalLoop_EmitsResultAndTurnState -count=1` | SetBus undefined | compiles | Given nil bus; When SetBus; Then no-op |
| T2 | Emit running + idle + result | Modify: `internal/employee/goal_loop.go` | same as T1 | no events | gate passes | Given a bus; When Execute returns; Then one result event on employee.result + running/idle edges |

T2 steps:
1. Confirm gate RED.
2. Add a `bus *bus.MessageBus` field to `GoalLoop` (guard with existing `mu`
   if the struct already guards wiring fields; snapshot under lock, publish
   outside lock). Add nil-safe `SetBus`.
3. Emit running turn-state at Execute entry (`AgentID: l.employeeID`).
4. After the existing result construction, build `ExecutionResultEvent` with
   `AgentID: l.employeeID`, `Status:
   completed|failed`, `TokensUsed: result.TokensUsed`, `Error: result.Error`,
   `DurationMS: int64(result.Duration/time.Millisecond)`, `StopReason` mapped
   per master C1. Publish via `bus.PublishT(l.bus, agent.TopicEmployeeResult,
   "employee", ev)`.
5. Emit idle turn-state with the same stop reason.
6. Keep `notifySpeakRound` and Reflect flow unchanged.
7. `gofmt -w`; run verify command.

## Self-Verification Checklist

- [ ] Published on TopicEmployeeResult only (employee.result); source="employee"; bot runtime untouched
- [ ] Publish outside lock; nil bus no-op
- [ ] `employee.notify` and Reflect paths unchanged
- [ ] Gate test unmodified

**DO NOT COMMIT.**

## Completion Report Format

1. Files touched
2. Verify command + last 20 lines
3. Deviations

## Review Checklist (For Review Agent)

- [ ] Result emitted even when executor fails
- [ ] No duplicate emission from Decide/ApproveAndExecute call sites
- [ ] No mutex held during publish

Output: APPROVED or specific gaps.

## Notes

Emitting inside `Execute` avoids duplicating publication across the three
call sites at lines 1788/2066/2126.

