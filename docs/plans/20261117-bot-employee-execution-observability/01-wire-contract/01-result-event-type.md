# ExecutionResultEvent wire type - Implementation Leaf

> **For the implementing agent:** Implement all tasks. Gate tests exist and are
> committed; make them pass. Do NOT edit or weaken gate tests. Do NOT commit -
> the orchestrator handles git. Do NOT use read_file; explore with search_files
> or terminal cat. Write once, then verify.

## Meta

- **Parent:** ./orchestrator.md
- **Scope:** Define shared payload and two typed topic vars in internal/agent
- **Dependencies:** 00T-wire-tests.md
- **Estimated Context:** 35K
- **Concurrency Group:** A

## Goal

Add the C1 struct, constants, WSClass marker, and TWO topic vars as the single
source of truth. There is NO `kind` field and NO `ExecutionKind` type.

## Context

- `internal/agent/coordination.go` - sibling turn-state types; match its style
- `internal/agent/topics.go` - topic declaration precedent
- `internal/comm/wsclass/wsclass.go` - WSProgress constant

## Verified Anchors

- `internal/agent/coordination.go:12-56` - AgentTurnState, StopReason, TurnStateEvent, TopicTurnState
- `internal/agent/topics.go:14` - `var TopicTurnTerminal`

## Interface Contracts (From Parent)

### What This Leaf Exposes

`agent.ExecutionResultEvent`, `agent.TopicBotResult` (`bot.result`),
`agent.TopicEmployeeResult` (`employee.result`) - full definition in parent C1.

### What This Leaf Consumes

- `agent.StopReason` - existing in `coordination.go`
- `wsclass.WSProgress` - existing

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|----------------|
| T1 | Define payload + two topics | Create: `internal/agent/execution_events.go` | `go test -p 2 ./internal/agent/ -run 'TestExecutionResultEvent_JSONShape\|TestResultTopics_Names' -count=1` | symbols absent | gate tests pass | Given C1; When compiled; Then exact tags, WSClass=WSProgress, and two topic names; NO kind field |

T1 steps:
1. Confirm gate tests fail RED for missing symbols.
2. Write `execution_events.go` with the exact C1 definition. Include a doc
   comment naming issue #140: the topic name is the tier discriminator and
   `kind` was deliberately omitted per operator decision.
3. Declare BOTH topic vars:
   `var TopicBotResult = bus.NewTopic[ExecutionResultEvent]("bot.result")`
   `var TopicEmployeeResult = bus.NewTopic[ExecutionResultEvent]("employee.result")`
4. `gofmt -w internal/agent/execution_events.go`.
5. Run verify commands. Expect PASS.

## Self-Verification Checklist

- [ ] Exact JSON tags from C1; no `kind` key; `WSClass()` returns `WSProgress`
- [ ] `TopicBotResult.Name == "bot.result"`; `TopicEmployeeResult.Name == "employee.result"`
- [ ] NO `ExecutionKind` type anywhere
- [ ] Gate tests unmodified; package tests pass

**DO NOT COMMIT.**

## Completion Report Format

1. Files touched
2. Verify command + last 20 lines
3. Deviations

## Review Checklist (For Review Agent)

- [ ] C1 matches exactly (tags, topic names); no extra fields
- [ ] No other topics changed
- [ ] No `kind`/`ExecutionKind` introduced

Output: APPROVED or specific gaps.

## Notes

Keep `DurationMS` as `int64`. Status is a plain string for
`completed|failed|skipped`.
