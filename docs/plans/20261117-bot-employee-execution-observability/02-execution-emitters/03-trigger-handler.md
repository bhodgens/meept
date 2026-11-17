# Bot trigger handler activation - Implementation Leaf

> **For the implementing agent:** Implement all tasks. Gate test exists and is
> committed; make it pass. Do NOT edit the gate test. Do NOT commit. Do NOT use
> read_file; explore with search_files/terminal cat. Write once, then verify.

## Meta

- **Parent:** ./orchestrator.md
- **Scope:** Install a real trigger handler and start the bot router
- **Dependencies:** 01-bot-runtime.md, 02-employee-emitters.md
- **Estimated Context:** 50K
- **Concurrency Group:** B

## Goal

Close the dead seam: `bot.NewEventActionRouter(msgBus, nil)` currently has no
handler and `router.Start` is never called. Make `bots.enabled=true` actually
run bots on bus triggers through the employee GoalLoop.

## Context

- `internal/daemon/components.go:5349` - only `NewEventActionRouter` call site
- `internal/daemon/components.go:6450` - `BotManager.StartAll(ctx)`
- `internal/bot/router.go:17-19,87-101` - handler interface + Start
- `internal/employee/manager.go:1712-1745` - `triggerViaGoalLoop` pattern
- `internal/employee/manager.go:1796-1820` - `SteerEmployee` push path

## Verified Anchors

- `internal/bot/router.go:33` - `func NewEventActionRouter(msgBus, handler)`
- `internal/daemon/components.go:5349-5350` - router + manager construction
- `internal/daemon/components.go:6450-6457` - bot framework start block

## Interface Contracts (From Parent)

### What This Leaf Exposes

- a daemon-side `BotTriggerHandler` implementation
- `router.Start(c.ctx)` called when bots start

### What This Leaf Consumes

- C3 `Manager.SetBus` / `GoalLoop.SetBus` (owner: leaves 01/02)
- `bot.BotTriggerHandler` (existing interface)

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|----------------|
| T1 | Implement handler | Create: `internal/daemon/bot_trigger_handler.go` | `go build ./internal/daemon/ && go test -p 2 ./internal/daemon/ -run TestBotTrigger_HandlerInvokesGoalLoop -count=1` | handler missing | compiles + passes | Given trigger; When HandleBotTrigger; Then target bot's GoalLoop is invoked |
| T2 | Wire handler + start router | Modify: `internal/daemon/components.go` | `go build ./internal/daemon/ && grep -n 'router.Start' internal/daemon/components.go` | no Start call | present | Given bots.enabled; When daemon starts; Then router listens on bot trigger topics |

T1 steps:
1. Define a struct holding `*employee.Manager` (or a minimal interface with
   `Trigger`/`Steer`), plus logger.
2. `HandleBotTrigger(ctx, botID, prompt)` builds the payload map with
   `source:"bus_event"`, `prompt:prompt`, then calls the employee trigger path
   for `botID`. Mirror `triggerViaGoalLoop`'s payload/TriggerEvent shape at
   `internal/employee/manager.go:1712-1745`.
3. Return errors unwrapped so the router logs them.
4. T2: at `components.go:5349`, pass the handler instead of `nil`. Store the
   router on `Components` if needed (add a field beside `BotManager` at :656).
5. In the bot start block at :6450, after `StartAll` succeeds, call the
   router's `Start(ctx)` once. Guard nil router/bus. Do NOT start twice.
6. `gofmt -w`; run both commands.

## Self-Verification Checklist

- [ ] Handler non-nil; router Start called exactly once
- [ ] No employee path changed; no new bus topics
- [ ] Nil router/bus safe; `go build ./internal/daemon/` passes

**DO NOT COMMIT.**

## Completion Report Format

1. Files touched
2. Verify command + last 20 lines
3. Deviations

## Review Checklist (For Review Agent)

- [ ] Trigger actually reaches a goal loop (not a stub)
- [ ] No double-start of router on reload/restart
- [ ] No import cycle introduced

Output: APPROVED or specific gaps.

## Notes

Operator CONFIRMED this leaf is in scope. The nil handler must be replaced and
the router started; do not leave the dead seam in place.
