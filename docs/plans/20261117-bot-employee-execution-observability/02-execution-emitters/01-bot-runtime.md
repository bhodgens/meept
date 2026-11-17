# Bot runtime emitters - Implementation Leaf

> **For the implementing agent:** Implement all tasks. Gate tests exist and are
> committed; make them pass. Do NOT edit gate tests. Do NOT commit. Do NOT use
> read_file; explore with search_files/terminal cat. Write once, then verify.

## Meta

- **Parent:** ./orchestrator.md
- **Scope:** Add bus seam + result/turn-state emission to BotRunner and Manager
- **Dependencies:** 00T-emitter-tests.md
- **Estimated Context:** 45K
- **Concurrency Group:** A

## Goal

Publish C1/C2 on every `BotRunner.Execute` return path. Forward the bus from
`bot.Manager` to runners it creates.

## Context

- `internal/bot/runner.go:55-256` - BotRunner struct, Execute, return paths
- `internal/bot/lifecycle.go:143-169` - startBot constructs the runner
- `internal/bot/runner_test.go` - existing fixture

## Verified Anchors

- `internal/bot/runner.go:163-174` - Execute signature + skipped early return
- `internal/bot/runner.go:228-255` - result construction + success/failure branches
- `internal/bot/lifecycle.go:144` - `runner := NewBotRunner(def)`

## Interface Contracts (From Parent)

### What This Leaf Exposes

- `func (r *BotRunner) SetBus(b *bus.MessageBus)` (C3)
- `func (m *Manager) SetBus(b *bus.MessageBus)` (C3)

### What This Leaf Consumes

- C1 `agent.ExecutionResultEvent` / `agent.TopicBotResult`
- C2 `agent.TopicTurnState` / `agent.TurnStateEvent`
- `agent.TopicBotResult`, `agent.StopReasonEndTurn`, `agent.StopReasonError`, `agent.StopReasonCancelled`

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|----------------|
| T1 | Add bus field + setter | Modify: `internal/bot/runner.go` | `go test -p 2 ./internal/bot/ -run TestBotRunner_EmitsResultAndTurnState -count=1` | SetBus undefined | compiles | Given nil bus; When SetBus; Then no panic |
| T2 | Emit at entry + all exits | Modify: `internal/bot/runner.go` | same as T1 + `-run TestBotRunner_EmitsResultOnFailure` | no events | gate passes | Given a bus; When Execute returns; Then result + turn-state edges on every path |

T2 steps:
1. Confirm gate RED.
2. Add `bus *bus.MessageBus` field (guarded by existing struct usage; no mutex needed
   if set before execution) and nil-safe `SetBus`.
3. At `Execute` entry after the `ShouldRun`/executor checks, emit a running
   `TurnStateEvent` (`AgentID: bot id`, `Timestamp: time.Now().UTC()`).
4. For the skipped early return, emit a result event with `Status:"skipped"`,
   `StopReason:Cancelled`, `Error` = the existing skip text, and an idle
   turn-state with the same stop reason. Return the original result unchanged.
5. At the success and failure branches, build an `ExecutionResultEvent` from
   the existing `BotExecutionResult` (`DurationMS: int64(result.Duration/time.Millisecond)`),
   set `Status: completed|failed`, map `StopReason`,
   and publish on `agent.TopicBotResult` (NOT the employee topic). Emit the matching idle turn-state.
6. `gofmt -w`; run both verify commands.

T3: Add `Manager.SetBus` in `internal/bot/lifecycle.go`; store bus on Manager and
pass to `NewBotRunner(def)` runners in `startBot` via a `WithBus` option or direct
field set (choose the smallest change). Nil-safe.

## Self-Verification Checklist

- [ ] All three return paths emit result + idle turn-state
- [ ] Published on TopicBotResult only (bot.result); source="bot"; nil bus no-op
- [ ] No changes to `BotExecutionResult` struct (other tiers use it)
- [ ] `gofmt` clean; gate tests unmodified

**DO NOT COMMIT.**

## Completion Report Format

1. Files touched
2. Verify command + last 20 lines
3. Deviations

## Review Checklist (For Review Agent)

- [ ] Every `return` in Execute has a matching emit path
- [ ] Stop-reason mapping matches master C1
- [ ] No publish under a lock

Output: APPROVED or specific gaps.

## Notes

Use `int64(result.Duration / time.Millisecond)`, not `time.Duration.Milliseconds()`
if the Go version in go.mod predates it; check go.mod first.

