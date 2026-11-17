# Inbox gate tests - Implementation Leaf

> **For the implementing agent:** Write TESTS ONLY. Do NOT write implementation
> files. Tests must fail RED for missing symbols. Do NOT commit. Do NOT use
> read_file; explore with search_files/terminal cat. Write once and stop.

## Meta

- **Parent:** ./orchestrator.md
- **Scope:** RED tests for bot roster and drainer propagation
- **Dependencies:** none
- **Estimated Context:** 30K
- **Concurrency Group:** A

## Goal

Pin C4: a bot id passes `targetExists`, and `employeeTurnClone` inherits the
template's drainer.

## Context

- `internal/daemon/employee_message_tools_test.go:12-40` - existing wiring test shape
- `internal/daemon/voice_employee.go:458-478` - employeeTurnClone
- `internal/agent/loop_messages_test.go:20-70` - drainer mock
- `internal/agent/loop.go:906-909` - messageDrainer field

## Verified Anchors

- `internal/daemon/employee_message_tools.go:111-133` - targetExists/listTargets closures
- `internal/daemon/voice_employee.go:458` - `func employeeTurnClone`
- `internal/agent/loop_messages.go:42` - `func (l *AgentLoop) SetMessageDrainer`

## Interface Contracts (From Parent)

### What This Leaf Exposes

- `internal/daemon/employee_message_tools_test.go` addition (F5)
- `internal/daemon/voice_employee_clone_test.go` creation (F6)

### What This Leaf Consumes

- C4 `MessageDrainer()` getter (owner: 01 impl leaf)

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|----------------|
| T1 | Bot roster gate | Modify: `internal/daemon/employee_message_tools_test.go` | `go test -p 2 ./internal/daemon/ -run TestRoster_IncludesBotID -count=1` | bot absent from targets | bot id accepted | Given a Components with BotManager holding bot "b1"; When targetExists("b1"); Then true and listTargets contains "b1" |
| T2 | Clone drainer gate | Create: `internal/daemon/voice_employee_clone_test.go` | `go test -p 2 ./internal/daemon/ -run TestClone_InheritsDrainer -count=1` | MessageDrainer getter undefined | clone drains | Given template loop with drainer; When employeeTurnClone; Then clone drains the injected message |

T1 uses a real `bot.Manager` with an in-memory/temp `bot.Store`; do not mock
the predicate. T2 uses the existing `mockMessageDrainer` pattern from
`internal/agent/loop_messages_test.go`.

## Self-Verification Checklist

- [ ] Both tests RED for missing behavior, not syntax
- [ ] No implementation files touched
- [ ] No line-number corruption

**DO NOT COMMIT.**

## Completion Report Format

1. Files touched
2. Verify command + last 20 lines
3. Deviations

## Review Checklist (For Review Agent)

- [ ] T1 exercises real Components wiring, not a hand-built closure
- [ ] T2 asserts drain at turn start, not tool call

Output: APPROVED or specific gaps.

## Notes

None.
