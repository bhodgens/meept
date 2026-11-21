# Test gate - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Write tests ONLY. Do not implement production behavior. Tests must fail
> RED because the production field or function is missing, or because the
> current code does the old thing. Do NOT commit. Do NOT git add.

## Meta

- **Parent:** master.md
- **Scope:** Failing tests that pin contracts C1 through C6.
- **Dependencies:** none
- **Estimated Context:** 40K
- **Concurrency Group:** T

## Goal

Land tests that fail for the right reason before any implementation leaf starts.

## Tasks

1. Add `TestPlanRefTopicSurvivesLaterBusEvent` in `internal/employee`. Build a PlanRef with `ResultTopic` set to `bot.result`. Change the loop flag to `employee.result`. Call the execute publish helper or Execute with a fake bus. Assert the published topic is `bot.result`. RED today if Execute still reads `l.resultTopic`.

2. Add `TestExecuteBotPassesSystemPrefix` in `internal/daemon`. Call ExecuteBot with a non-empty systemPrompt on a fake loop that records SetTurnSystemPrefix. Assert the prefix equals the argument. RED today because the adapter drops systemPrompt.

3. Add `TestExecuteBotReturnsLastTurnTokens` in `internal/daemon`. Fake loop LastTurnTokens returns 17. Assert ExecuteBot's tokensUsed is 17. RED today because the adapter returns 0.

4. Add `TestEmployeeTurnUsesStandingSession` in `internal/daemon` or `internal/employee`. Assert the conversation id passed to RunOnce equals `emp-sess-` plus the employee id. RED today because the adapter uses `id.Generate("bot-exec")`.

5. Add `TestTurnTerminalAppendsSpecialistRun` in `internal/activity`. Publish a turn.terminal event for agent id `coder`. Assert the execution store has one row with tier `specialist`. RED today because the projector does not subscribe to turn.terminal.

6. Add `TestCronTriggerCallsManager` in `internal/daemon` or `internal/bot`. Register a bot with an enabled cron trigger. Fire the scheduler tick or the registration callback. Assert Manager.Trigger was called with source `cron`. RED today because no caller fires cron rows.

Do NOT commit. Do not skip a test to make the package pass. A compile error because `ResultTopic` or `SetTurnSystemPrefix` does not exist yet is an acceptable RED. Prefer a test that compiles against a new unexported test seam only if the production symbol cannot be referenced yet. Document that seam in the test comment.

## Interface Contract

This leaf exposes the test names in the master Test Gate table. It does not add production exports.

## Self-Verification Checklist

- [ ] Each new test fails for a missing behavior, not for a syntax error
- [ ] No production function was changed
- [ ] Do NOT commit was followed

## Review Checklist

- [ ] Test names match the master Test Gate table
- [ ] No test was skipped
- [ ] Do NOT commit
