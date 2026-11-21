# Routing funnel - Implementation Orchestrator

> **For the executing agent:** You are the orchestrator for this tree node.
> Your job: (1) dispatch implementation agents, (2) review their work,
> (3) re-dispatch if incomplete, (4) track completion.
> Do NOT implement code yourself. All implementation happens in leaf agents.
> Full operational detail: skill `hierarchical-plan-execution`.

## Meta

- **Role:** Root
- **Parent:** none (root)
- **Children:** 7 documents under this directory
- **Scope:** One result tier per run, one constitution prompt on the execute path, real token counts, detached text in the employee chat session, specialist turns in the agents-tab history, and live cron and webhook triggers.

## Goal

The 2026-11-20 routing audit found one live execute engine and several disagreeing copies of the same idea. The owner locked these answers on 2026-11-20:

1. A. Stamp the result topic on the plan when the round starts. ApproveAndExecute reads that stamp.
2. A. Pass SynthesizedPrompt into the loop as the system prefix for that turn.
3. A. Return the real token count from the turn and put it on the result event.
4. C and B. Merge detached output into the chat session of that employee. Also write specialist chat turns into execution_runs.
5. B. Wire stored cron and webhook triggers to Manager.Trigger.

BotRunner is already deleted (commit 48655d53). This tree does not restore it. The voice spoken guard stays a separate lane. Do not merge it into the chat reply guard.

## Architecture

GoalLoop.Execute remains the only bot and employee runner. It calls bot.BotExecutor.ExecuteBot. The production executor is employeeBoundAgentLoopExecutor, which clones AgentLoop and calls RunOnce.

The result tier moves off the shared GoalLoop flag and onto the plan. A later bus event must not retarget an approved plan.

Detached rounds stop minting a throwaway `bot-exec` conversation id. Each employee has one standing chat session. The execute turn and the notify push use that session id.

Specialist chat turns stay on turn.terminal for the user reply. A subscriber also appends one execution_runs row so the agents tab shows them. That row is not a second chat bubble.

Cron and webhook rows on a bot definition become Trigger calls. They do not call a deleted runner.

## Interface Contracts

### C1: Plan result topic

```
type PlanRef struct {
    // existing fields unchanged
    ResultTopic string `json:"result_topic,omitempty"`
}
// Empty ResultTopic means agent.TopicEmployeeResult.Name.
// The only other legal value is agent.TopicBotResult.Name.
// Set once, when the PlanRef is built for that round.
// Execute and ApproveAndExecute read plan.ResultTopic.
// They do not read GoalLoop.resultTopic.
// Owner: 01-result-topic-stamp.md
// Consumers: 00T-test-gate.md, 03-detached-chat-session.md
```

The stored plan must keep ResultTopic across process restart. Resume and ApproveAndExecute load it back onto PlanRef. A lookup failure does not invent bot.result. It uses employee.result and logs a warning.

### C2: Turn system prefix

```
// On the per-turn loop clone only. Never on the shared template loop.
func (l *AgentLoop) SetTurnSystemPrefix(prefix string)
// Empty prefix is a no-op. The prefix is prepended to the system prompt
// for the next RunOnce on that clone, then cleared.
// Owner: 02-executor-prompt-and-tokens.md
// Consumers: 00T-test-gate.md
```

employeeBoundAgentLoopExecutor.ExecuteBot passes the systemPrompt argument into SetTurnSystemPrefix before RunOnce. It must not drop the argument.

### C3: Turn token count

```
func (l *AgentLoop) LastTurnTokens() int
// Set at the end of the turn from provider usage. 0 only when the
// provider sent no usage. Not a hardcoded return.
// Owner: 02-executor-prompt-and-tokens.md
// Consumers: 00T-test-gate.md, execution result payload in 01
```

ExecuteBot returns LastTurnTokens() as tokensUsed. ExecutionResultEvent.TokensUsed carries that value. Do not change the RunOnce signature `(string, error)`.

### C4: Standing employee chat session

```
func EmployeeChatSessionID(employeeID string) string
// Returns "emp-sess-" + employeeID. Stable. Not a random bot-exec id.
// Owner: 03-detached-chat-session.md
// Consumers: 00T-test-gate.md, 05-cron-webhook-trigger.md
```

Before Execute, the daemon ensures a session row exists for that id. ExecuteBot uses that id as the conversation id. The assistant text is stored as a chat message on that session, through the same writer the chat path uses. employee.notify push sets SessionIDs to that one id. It does not broadcast.

ExecutionResultEvent.SessionID on a goal or bot round is this chat session id. It is not the raw employee id.

### C5: Specialist execution row

```
tier string = "specialist"
// One execution_runs row per completed specialist chat turn.
// Written from the turn.terminal subscriber in internal/activity.
// Not published on bot.result or employee.result.
// Owner: 04-specialist-execution-runs.md
// Consumers: 00T-test-gate.md
```

The row carries agent_id, status, output, tokens_used when known, stop_reason, and session_id from the turn.terminal event. A missing store is a no-op. It does not fail the chat turn.

### C6: Trigger sources

```
source "cron"     — fired from an enabled BotTrigger of type cron
source "webhook"  — fired from an enabled BotTrigger of type webhook
source "bus_event" — unchanged; still stamps ResultTopic bot.result
// Owner: 05-cron-webhook-trigger.md
// Consumers: 01-result-topic-stamp.md, 03-detached-chat-session.md
```

Both new sources call Manager.Trigger. They do not call BotRunner. A cron or webhook round stamps ResultTopic employee.result unless the payload source is bus_event.

## Child Document Index

| # | Document | Type | Dependencies | Est. Context | Concurrency |
|---|----------|------|-------------|-------------|-------------|
| 00T | 00T-test-gate.md | leaf | none | 40K | T |
| 01 | 01-result-topic-stamp.md | leaf | 00T | 55K | A |
| 02 | 02-executor-prompt-and-tokens.md | leaf | 00T | 60K | A |
| 03 | 03-detached-chat-session.md | leaf | 00T, C4 | 70K | B |
| 04 | 04-specialist-execution-runs.md | leaf | 00T | 45K | A |
| 05 | 05-cron-webhook-trigger.md | leaf | 00T, 01 | 55K | C |
| 06 | 06-stale-runner-cleanup.md | leaf | none | 25K | A |

Group A leaves do not edit the same files. 01 owns goal_loop result-topic reads and the plan store field. 02 owns the executor adapter and the loop prefix and token seams. 04 owns the activity projector. 06 owns docs and the stale integration test. 03 waits for group A so it can use the stamped topic and the executor conversation id. 05 waits for 01 so cron and webhook stamp the topic the same way.

## Dispatch Protocol

For each concurrency group, in dependency order. Full recipe: skill `hierarchical-plan-execution`.

### Phase 0: Test Gate (code branches, before ANY implementation dispatch)

Dispatch 00T-test-gate.md first. Tests only. They must fail RED because the production symbols or fields are missing. Validate in-session against the Test Gate matrix. Do not delete a RED test to make it pass. Commit the gate tests. Set 00T to TESTS_VALIDATED. Then implementation may dispatch.

### Phase 1: Dispatch Concurrency Group A

Dispatch 01, 02, 04, and 06 together after the test gate. 06 may also run in Phase 0 if it only edits docs, but keep it with A so the test file and the docs land after the gate tests exist.

Each dispatch context includes: do not commit, do not git add, write once, and the contract IDs that leaf consumes.

### Phase 2: Review and Commit Each Child

Review in-session. Pass: commit that leaf's files only, with a pathspec. Set REVIEWED. Gaps: re-dispatch, max 3 cycles.

### Phase 3: Dispatch Group B then Group C

Dispatch 03 after 01 and 02 are REVIEWED. Dispatch 05 after 01 and 03 are REVIEWED. Review and commit each the same way.

### Phase 4: Integration Review

After all children are REVIEWED, run the Integration Test Plan. Set children COMPLETE only when those commands pass.

## Review Checklist

The orchestrator (main model) verifies each child in-session:

- [ ] All tasks from the leaf document are implemented
- [ ] Interface contracts satisfied (signatures, types, paths)
- [ ] All specified files created or modified at exact paths
- [ ] Gate tests pass GREEN; git diff shows no weakening of gate tests
- [ ] Tests written and passing
- [ ] Code follows project conventions
- [ ] No scope creep
- [ ] No debug artifacts
- [ ] No line-number corruption in source files
- [ ] Voice spoken guard was not edited

Output: APPROVED or a list of specific gaps.

## Coding Conventions

- **Language:** Go. Toolchain from go.mod. Test sweeps use `go test -p 2`.
- **Naming:** exported identifiers use MixedCaps. JSON tags stay snake_case.
- **Errors:** wrap with context. Do not use `_ = ` on a new non-test line.
- **IDs:** use pkg/id.Generate. Do not use time.Now().UnixNano for ids.
- **Bus:** topic name stays the tier discriminator. Do not add a kind field to ExecutionResultEvent.
- **Tests:** new feature tests go in e2e/suites when they cross a package boundary. A unit pin is allowed only to extend an existing test file for a bug fix.
- **Formatting:** gofmt on touched Go files before report.
- **Commit:** leaf agents do not commit. The orchestrator commits with a pathspec.

## Requirement Traceability

| Req | Source (user words, verbatim when possible) | Plan element | Verified by |
|-----|---------------------------------------------|--------------|-------------|
| R1 | 1. A. Stamp the topic on the plan when the round starts. ApproveAndExecute reads that stamp. | 01-result-topic-stamp.md / C1 | 00T topic tests |
| R2 | 2. A. Pass SynthesizedPrompt into the loop as the system prefix for that turn. | 02-executor-prompt-and-tokens.md / C2 | 00T prefix test |
| R3 | 3. A. Return the real count from RunOnce and put it on the result event. | 02-executor-prompt-and-tokens.md / C3 | 00T token test |
| R4 | 4. C. Merge detached output into the chat session of that employee. | 03-detached-chat-session.md / C4 | 00T session test |
| R5 | 4. B. Also write specialist chat turns into execution_runs. | 04-specialist-execution-runs.md / C5 | 00T specialist row test |
| R6 | 5. B. Wire them to Manager.Trigger, so a stored cron row starts a goal round. | 05-cron-webhook-trigger.md / C6 | 00T trigger test |
| R7 | Stale BotRunner docs and the broken NewBotRunner test reference go in a cleanup leaf. | 06-stale-runner-cleanup.md | doc grep + test compile |

## Completion Tracking Table

| Child | Status | Iterations | Review Notes |
|-------|--------|------------|-------------|
| 00T-test-gate | TESTS_VALIDATED | 1 | Reviewed 2026-11-20. Six tests compile. Stamp, prefix, token, and specialist tests now pass. Session and cron tests stay red until leaves 03 and 05. |
| 01-result-topic-stamp | REVIEWED | 1 | Commit 700bb3ea. Execute publishes plan.ResultTopic. Re-ran TestPlanRefTopicSurvivesLaterBusEvent: pass. |
| 02-executor-prompt-and-tokens | REVIEWED | 1 | Commit a9ccf4a4. Prefix is one-shot. Tokens come from the provider total. Re-ran both gate tests: pass. |
| 03-detached-chat-session | IN_PROGRESS | 0 | Dispatched after 01 and 02 review. |
| 04-specialist-execution-runs | REVIEWED | 1 | Commit fb86f7a9. turn.terminal appends tier specialist. Re-ran the gate test: pass. |
| 05-cron-webhook-trigger | PENDING | 0 | Waits for 03. |
| 06-stale-runner-cleanup | REVIEWED | 1 | Commit bd5def0f. NewBotRunner call removed. Docs name GoalLoop. |

Status values: PENDING | TESTS_IN_PROGRESS | TESTS_VALIDATED | IN_PROGRESS | IMPLEMENTED | REVIEWED | COMPLETE | BLOCKED

Gate rule: implementation children stay PENDING until 00T reaches TESTS_VALIDATED.

## Integration Test Plan

After all leaves are REVIEWED, run:

```
go test -p 2 -count=1 ./internal/employee/ ./internal/daemon/ ./internal/activity/ ./internal/bot/
```

Expected: pass. A package that does not compile is a failed integration.

Then confirm with search:

- GoalLoop.Execute does not read l.resultTopic to choose the publish topic.
- agentLoopBotExecutorAdapter.ExecuteBot passes systemPrompt into the clone.
- ExecuteBot does not return a hardcoded 0 when LastTurnTokens is non-zero.
- No production call mints a conversation id with the prefix bot-exec for an employee turn.
- No production file calls NewBotRunner.

## Test Gate

Test leaves: 00T-test-gate.md. Dispatched in Phase 0. Validated by the orchestrator in-session.

| # | Feature | Source | Unit tests | E2E tests | Status |
|---|---------|--------|------------|-----------|--------|
| F1 | Plan stamp, not loop flag | 01 / C1 | TestPlanRefTopicSurvivesLaterBusEvent | n/a until e2e suite exists; 00T adds one hermetic case if a suite already covers employees | PENDING |
| F2 | System prefix reaches the clone | 02 / C2 | TestExecuteBotPassesSystemPrefix | n/a in this leaf; pin is unit | PENDING |
| F3 | Token count is not hardcoded 0 | 02 / C3 | TestExecuteBotReturnsLastTurnTokens | n/a | PENDING |
| F4 | Detached text uses emp-sess session | 03 / C4 | TestEmployeeTurnUsesStandingSession | e2e if execution-observability suite can host it | PENDING |
| F5 | Specialist turn writes execution_runs | 04 / C5 | TestTurnTerminalAppendsSpecialistRun | n/a | PENDING |
| F6 | Cron and webhook call Trigger | 05 / C6 | TestCronTriggerCallsManager | n/a | PENDING |
| F7 | No NewBotRunner, docs name GoalLoop | 06 | compile of tests/bot_integration_test.go | n/a | PENDING |

Statuses: PENDING | DELTA | VALIDATED

Delta log: none yet.

## Frozen Documents

Leaf documents freeze at dispatch. Corrections ride dispatch context as dated addenda. Never edit a dispatched leaf document mid-execution.

## Open Questions

None. The owner answered on 2026-11-20. One interpretation is frozen here, not left open: "the chat session of that employee" means one standing session per employee, id `emp-sess-` plus the employee id. A later request for many sessions per employee is a new tree.

## Notes

- Do not edit the voice spoken guard.
- Do not add a kind field to ExecutionResultEvent.
- Direct push of unrelated main commits is out of scope. The gate commit ec67c334 is a separate push.
