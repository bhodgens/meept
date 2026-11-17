# Execution emitter gate tests - Implementation Leaf

> **For the implementing agent:** Write TESTS ONLY. Do NOT write implementation
> files. Tests must fail RED for missing symbols. Do NOT commit. Do NOT use
> read_file; explore with search_files/terminal cat. Write once and stop.

## Meta

- **Parent:** ./orchestrator.md
- **Scope:** RED unit tests for bot and employee emission
- **Dependencies:** none
- **Estimated Context:** 35K
- **Concurrency Group:** A

## Goal

Author unit gate tests that subscribe `bus.SubscribeT` to `agent.TopicBotResult` and `agent.TopicEmployeeResult`
and `agent.TopicTurnState`, invoke a runner, and assert both events.

## Context

- `internal/bot/runner_test.go:161-254` - BotRunner test fixture shape
- `internal/employee/goal_loop_test.go` - GoalLoop fixture shape
- `internal/bus/topic.go:37-95` - PublishT/SubscribeT helpers
- `internal/agent/coordination.go:42-56` - frozen turn-state shape

## Verified Anchors

- `internal/bot/runner.go:163` - `func (r *BotRunner) Execute`
- `internal/employee/goal_loop.go:976` - `func (l *GoalLoop) Execute`
- `internal/bus/topic.go:80` - `func SubscribeT`

## Interface Contracts (From Parent)

### What This Leaf Exposes

- `internal/bot/execution_events_test.go` (F3)
- `internal/employee/goal_loop_events_test.go` (F4)

### What This Leaf Consumes

- C3 `SetBus` seams (owner: 01/02 impl leaves)
- C1/C2 typed topics (owner: branch 01)

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|----------------|
| T1 | Bot emission gate | Create: `internal/bot/execution_events_test.go` | `go test -p 2 ./internal/bot/ -run TestBotRunner_EmitsResultAndTurnState -count=1` | `SetBus` undefined | two topic events | Given a bus + fake executor; When Execute succeeds; Then one result event on bot.result status=completed stop_reason=end_turn and running+idle turn-state edges |
| T2 | Employee emission gate | Create: `internal/employee/goal_loop_events_test.go` | `go test -p 2 ./internal/employee/ -run TestGoalLoop_EmitsResultAndTurnState -count=1` | `SetBus` undefined | two topic events | Given a bus + fake executor; When Execute succeeds; Then result event on employee.result and running+idle edges |
| T3 | Failure path gate | Add to F3 file | `go test -p 2 ./internal/bot/ -run TestBotRunner_EmitsResultOnFailure -count=1` | no failure event | failed+error event | Given executor returns error; When Execute; Then status=failed stop_reason=error |

Gate tests use `bus.NewMessageBus()` if available; else follow the bus
construction in existing bus tests. Subscriptions use `bus.SubscribeT` with a
buffered channel or callback and a short timeout.

## Self-Verification Checklist

- [ ] Tests assert both topics, both tiers, failure path
- [ ] RED from missing symbols, not syntax
- [ ] No implementation files touched

**DO NOT COMMIT.**

## Completion Report Format

1. Files touched
2. Verify command + last 20 lines
3. Deviations

## Review Checklist (For Review Agent)

- [ ] Assertions pin the correct topic per tier + status/stop_reason mapping; no kind field
- [ ] No gate test weakened later

Output: APPROVED or specific gaps.

## Notes

None.
