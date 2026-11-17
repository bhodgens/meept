# Execution Emitters - Implementation Orchestrator

> **For the executing agent:** You are the orchestrator for this branch.
> Dispatch implementation agents, review, track completion. Do NOT implement
> code yourself. Full detail: skill `hierarchical-plan-execution`.

## Meta

- **Role:** Branch
- **Parent:** ../master.md
- **Children:** 4 leaves (test gate, bot runner, employee goal loop, trigger handler)
- **Scope:** Publish result + turn-state events from both execution tiers

## Goal

Wire the C1/C2 events into `BotRunner.Execute` (bot tier) and `GoalLoop.Execute`
(employee tier) on success, failure, and skipped paths. Give bots a live trigger
handler and start the router so `bots.enabled=true` actually executes — the
current tree has a nil handler and never calls `router.Start`, so no standalone
bot runs exist today.

## Architecture

Both packages import `internal/agent`, which is cycle-free. A nil-safe
`SetBus(*bus.MessageBus)` seam on `BotRunner`, `bot.Manager`, and
`employee.GoalLoop` supplies the bus. Publishing happens outside locks.
`BotRunner.Execute` emits at entry (running) and at every return path
(completed/failed/skipped with matching stop reason). `GoalLoop.Execute` does
the same on `employee.result`. The trigger handler is a daemon-side adapter
that builds a `GoalLoop` for the bot definition and calls `Decide`; it is
installed at the single `bot.NewEventActionRouter` construction site.

## Interface Contracts

### C3: Publisher seam

```
func (r *BotRunner) SetBus(b *bus.MessageBus)
func (m *Manager) SetBus(b *bus.MessageBus)
func (l *GoalLoop) SetBus(b *bus.MessageBus)
```
Nil = no-op. `Manager.SetBus` stores the bus and passes it to runners created
in `startBot`.

### C1/C2 emissions

```
bot tier:      bus.PublishT(b, agent.TopicBotResult, "bot", ev)
               bus.PublishT(b, agent.TopicTurnState, "bot", turnEv)
employee tier: bus.PublishT(b, agent.TopicEmployeeResult, "employee", ev)
               bus.PublishT(b, agent.TopicTurnState, "employee", turnEv)
```
There is NO `kind` field. The topic name identifies the tier. Status/stop reason
mapping frozen in master C1.

### Bot trigger handler (new daemon seam)

```
type BotTriggerHandler interface { HandleBotTrigger(ctx, botID, prompt string) error }
```
Already declared in `internal/bot/router.go:17-19`. Implement in `internal/daemon`
and pass to `bot.NewEventActionRouter`. Call `router.Start(ctx)` when the bot
framework starts.

## Child Index

| # | Document | Type | Dependencies | Est. Context | Concurrency |
|---|----------|------|-------------|-------------|-------------|
| 00T | 00T-emitter-tests.md | leaf (test) | none | 35K | A |
| 01 | 01-bot-runtime.md | leaf | 00T | 45K | A |
| 02 | 02-employee-emitters.md | leaf | 00T | 45K | A |
| 03 | 03-trigger-handler.md | leaf | 01,02 | 50K | B |

01 and 02 are file-disjoint (`internal/bot/*` vs `internal/employee/*`) and
dispatch together. 03 owns `internal/daemon/*bot*trigger*` files.

## Dispatch Protocol

### Phase 0: Test Gate

Dispatch `00T-emitter-tests.md`. Validate RED against Test Gate F3/F4. Commit;
set TESTS_VALIDATED.

### Phase 1: Dispatch Group A

Dispatch 01 and 02 in parallel. Each brief carries C1-C3 in full and its exact
write-targets. Neither leaf may edit `internal/agent/*`.

### Phase 2: Trigger Handler

After 01 and 02 REVIEWED, dispatch 03. It reads the `SetBus` seams but does not
edit them. If a needed getter is missing, report back to the orchestrator
rather than editing another leaf's files.

### Phase 3: Integration

Run `go test -p 2 ./internal/bot/... ./internal/employee/... ./internal/daemon/...`.
Set COMPLETE when green and gate tests unmodified.

## Review Checklist

- [ ] Result event emitted on success, failure, and skipped paths for both tiers
- [ ] Turn-state running edge at entry; idle + stop_reason at every exit
- [ ] Correct topic: bot -> `TopicBotResult`, employee -> `TopicEmployeeResult`; source string correct
- [ ] NO `kind` field used anywhere
- [ ] Nil bus is a no-op (no panic)
- [ ] Publish outside all locks
- [ ] Existing delegate/pair/ACP turn-state tests still pass

## Coding Conventions

- Go 1.27; `gofmt`; `bus.PublishT` for typed topics
- Nil guard every setter; publish outside mutex
- Wrap errors with `%w`; no ignored errors
- Leaf agents do NOT commit and do NOT run `git add`

## Requirement Traceability

| Req | Source | Plan element | Verified by |
|-----|--------|--------------|-------------|
| R1 | keep both tiers | 01 + 02 | both emit |
| R2 | result event every execution | 01 + 02 all return paths | gate F3/F4 |
| R5 | same observability surface | C1/C2 | e2e |

## Completion Tracking Table

| Child | Status | Iterations | Review Notes |
|-------|--------|------------|-------------|
| 00T-emitter-tests | PENDING | 0 | |
| 01-bot-runtime | PENDING | 0 | |
| 02-employee-emitters | PENDING | 0 | |
| 03-trigger-handler | PENDING | 0 | |

## Integration Test Plan

`go test -p 2 ./internal/bot/... ./internal/employee/... ./internal/daemon/... -count=1`
plus `go test -tags e2e ./e2e/suites/execution-observability/... -run 'ResultEvent|TurnState'`.

## Test Gate

| # | Feature | Source | Unit tests | E2E tests | Status |
|---|---------|--------|-----------|-----------|--------|
| F3 | Bot result + state emission | 01-bot-runtime.md | TestBotRunner_EmitsResultAndTurnState | execution-observability-02 | PENDING |
| F4 | Employee result + state emission | 02-employee-emitters.md | TestGoalLoop_EmitsResultAndTurnState | execution-observability-03 | PENDING |
| F9 | Bot trigger path live | 03-trigger-handler.md | TestBotTrigger_HandlerInvokesGoalLoop | execution-observability-02 | PENDING |

Delta log: none yet.

## Open Questions

- Resolved: operator confirmed the trigger-handler activation (leaf 03) is IN
  SCOPE, and that result topics are SEPARATE. Use `TopicBotResult` for bots and
  `TopicEmployeeResult` for employees; never a `kind` field.

## Notes

`internal/daemon/components.go:5349` is the ONLY production
`bot.NewEventActionRouter` call; leaf 03 owns changes to that line.

