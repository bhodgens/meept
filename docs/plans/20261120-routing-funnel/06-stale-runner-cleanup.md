# Stale runner cleanup - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Update docs and the broken test reference. Do not restore BotRunner.
> Do NOT commit. Do NOT git add.

## Meta

- **Parent:** master.md
- **Scope:** Remove stale BotRunner claims and the missing NewBotRunner call.
- **Dependencies:** none
- **Estimated Context:** 25K
- **Concurrency Group:** A

## Goal

A reader of the docs and the integration test sees GoalLoop as the live executor.

## Tasks

1. `docs/workflows/employees.md` must not say approved work calls `BotRunner.Execute()`. Say GoalLoop.Execute calls the BotExecutor.

2. `docs/workflows/bots.md` must not list `internal/bot/runner.go` as the bot.result publisher. Say GoalLoop publishes bot.result only when the plan stamp is bot.result.

3. `docs/workflows/inter-agent-coordination.md` gets the same publisher correction if it still names BotRunner.

4. `tests/bot_integration_test.go` must not call `bot.NewBotRunner`. Point the test at the goal-loop path, or delete the dead setup if the test no longer has a runner to construct. The package must compile.

5. Grep the repo for `NewBotRunner` and `BotRunner.Execute`. Production and docs hits in the files above must be gone. A mention inside this plan tree may remain.

## Interface Contract

No new production API. This leaf removes false claims.

## Self-Verification Checklist

- [ ] grep NewBotRunner under tests/ and internal/ returns no call
- [ ] employees.md and bots.md name GoalLoop
- [ ] Do NOT commit

## Review Checklist

- [ ] BotRunner was not reintroduced
- [ ] Do NOT commit
