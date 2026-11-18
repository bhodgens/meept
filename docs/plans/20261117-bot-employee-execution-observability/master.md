# Bot + Employee Execution Observability and Inbox (issue #140) - Implementation Orchestrator

> **For the executing agent:** You are the orchestrator for this tree node.
> Your job: dispatch implementation agents, review their work, re-dispatch if
> incomplete, and track completion. Do NOT implement code yourself. All
> implementation happens in leaf agents. Full operational detail: skill
> `hierarchical-plan-execution`.

## Meta

- **Role:** Root
- **Parent:** none
- **Children:** 5 leaves/branches (root test leaf + 4 branches)
- **Scope:** Give bots and employees separate result events, shared turn-state
  edges, inbox addressability, turn-start injection, WS relay, and hermetic e2e coverage.

## Goal

Issue #140 reports that bot executions produce `bot.BotExecutionResult`, log it,
and drop it. Bots also cannot receive `send_agent_message`. Investigation in this
repo shows the result gap applies to employees too: `internal/employee/goal_loop.go:1005-1028`
builds the same struct for every employee round and emits no bus event. The
inbox gap is bot-only in the roster gate, but turn-start injection at
`internal/agent/loop.go:9376` has no production drainer wiring at all.
The operator decision: keep both tiers, and give each tier its OWN result topic.

This tree adds two frozen result events: `bot.result` for bots and
`employee.result` for employees. Both tiers publish turn-state edges on the
shared `agent.turn.state` topic from #139. The messaging roster extends to bots
and turn-start injection becomes live. A hermetic e2e suite proves the full path
over the WS relay.

## Architecture

`internal/agent` hosts cross-cutting coordination wire types (it already hosts
`TurnStateEvent` / `TopicTurnState` from #139). A shared `ExecutionResultEvent`
payload lives beside them, with two typed topic vars: `TopicBotResult`
(`bot.result`) and `TopicEmployeeResult` (`employee.result`). The payload has no
`kind` field; the topic name identifies the tier. Both `internal/bot` and
`internal/employee` import `internal/agent` (verified cycle-free:
`go list -deps ./internal/agent | grep internal/bot` returns nothing).
`internal/comm/http` already imports `internal/agent`, so the WS decoder needs no
new dependency and cannot form a `bot -> comm/http -> bot` cycle.
Publishers use a nil-safe `SetBus(*bus.MessageBus)` seam on `BotRunner`,
`bot.Manager`, and `employee.GoalLoop`. Daemon wiring supplies `c.msgBus`.
Result events carry the execution outcome; turn-state edges use the frozen #139
vocabulary. Inbox work reuses the employee `MessageStore` and the existing
`agent.MessageDrainer` seam.

## Interface Contracts

### C1: ExecutionResultEvent (frozen wire type, shared by two topics)

```
type ExecutionResultEvent struct {
    AgentID    string     `json:"agent_id"`
    Status     string     `json:"status"`            // completed | failed | skipped
    Output     string     `json:"output,omitempty"`
    TokensUsed int        `json:"tokens_used"`
    Error      string     `json:"error,omitempty"`
    DurationMS int64      `json:"duration_ms"`
    StopReason StopReason `json:"stop_reason,omitempty"`
    TriggerRef string     `json:"trigger_ref,omitempty"`
    SessionID  string     `json:"session_id,omitempty"`
    Timestamp  time.Time  `json:"timestamp"`
}
func (ExecutionResultEvent) WSClass() wsclass.WSClass { return wsclass.WSProgress }
var TopicBotResult      = bus.NewTopic[ExecutionResultEvent]("bot.result")
var TopicEmployeeResult = bus.NewTopic[ExecutionResultEvent]("employee.result")
// Owner: 01-wire-contract/01-result-event-type.md
// Consumers: 02-execution-emitters/*, 01-wire-contract/02-ws-relay.md, 00T-e2e-gate.md
```

There is NO `kind` field and NO `ExecutionKind` type. The topic name is the
tier discriminator. Status/stop-reason mapping is frozen:
`completed` -> `end_turn`; `failed` -> `error`; `skipped` (budget or failure cap)
-> `cancelled` with `Status:"skipped"`. This preserves the #139 vocabulary;
no new `StopReason` value is added.

### C2: Turn-state edges (reuse #139, no new type)

```
agent.TopicTurnState  // "agent.turn.state", payload agent.TurnStateEvent
```
At execution entry emit `{AgentID, State:TurnStateRunning, Timestamp:now}`.
At exit emit `{AgentID, State:TurnStateIdle, StopReason:<C1 mapping>}`.
No `Phase` field. Owner: `internal/agent/coordination.go` (existing, unchanged).
Consumers: both emitter leaves, the WS relay leaf, the e2e gate.

### C3: Publisher seam

```
func (r *BotRunner) SetBus(b *bus.MessageBus)      // nil = no-op
func (m *Manager) SetBus(b *bus.MessageBus)        // forwards to runners it creates
func (l *GoalLoop) SetBus(b *bus.MessageBus)       // nil = no-op
```
Bots publish `bus.PublishT(b, agent.TopicBotResult, "bot", ev)`.
Employees publish `bus.PublishT(b, agent.TopicEmployeeResult, "employee", ev)`.
Both also publish `bus.PublishT(b, agent.TopicTurnState, <source>, turnEv)`.
Publish outside every lock.

### C4: Messaging roster and drainer

```
targetExists(id) == true when AgentRegistry.GetSpec(id) exists
                        OR EmployeeManager.GetEmployee(id) succeeds
                        OR BotManager.GetBot(id) succeeds
listTargets() = dedup of agent registry ids + employee ids + bot ids
func (l *AgentLoop) MessageDrainer() MessageDrainer   // new getter
```
`employeeMessageStoreAdapter` gains `DrainInbox(to string, limit int) ([]agent.IncomingAgentMessage, error)`
so it satisfies both `builtin.AgentMessager` and `agent.MessageDrainer`.
`employeeTurnClone` copies the template drainer onto the per-turn clone; the
clone already stamps `agentID = employeeID`, so a message addressed to a bot id
is drained on that bot's next goal round.

### C5: WS relay registration

`internal/comm/http/server_wsclass.go` adds ONE decoder used by BOTH topics:
```
agent.TopicBotResult.Name:      decodeExecutionResultWSClass,
agent.TopicEmployeeResult.Name: decodeExecutionResultWSClass,
```
`internal/comm/http/server.go` subscription list adds `"bot.result"` and
`"employee.result"` (2-segment topics; the single-segment `*` does not match
either). Classify as `agent_progress`, never `chat_message`.

## Child Document Index

| # | Document | Type | Dependencies | Est. Context | Concurrency |
|---|----------|------|-------------|-------------|-------------|
| 00T | 00T-e2e-gate.md | leaf (test) | none | 55K | A |
| 01 | 01-wire-contract/ | branch | none | 70K | A |
| 02 | 02-execution-emitters/ | branch | C1,C2 | 80K | A |
| 03 | 03-inbox/ | branch | C4 | 60K | A |
| 04 | 04-integration/ | branch | 01,02,03 | 65K | B |

Same-letter children have no inter-dependencies and dispatch together. Branch 04
depends on all three implementation branches and runs after them.

## Dispatch Protocol

### Phase 0: Test Gate (before ANY implementation dispatch)

Dispatch all `00T-*` test leaves in parallel (root gate plus one per branch).
Tests only. They must fail RED for missing-implementation reasons, not syntax
errors. Validate in-session against every branch's Test Gate matrix. Apply the
delta protocol (update the lesser artifact; coverage converges upward). Commit
the gate tests with `--no-verify` and a body naming the manual gates run. Set
each branch to TESTS_VALIDATED. Only then dispatch implementation leaves.

### Phase 1: Dispatch Concurrency Group A

Dispatch the implementation leaves of branches 01, 02, and 03 simultaneously
via `delegate_task`. Each brief carries: full leaf text, the contracts above,
exact write-targets, no-commit rule, no-read_file rule, and the verify commands.
Branches 01 and 03 both touch `internal/agent` but in DIFFERENT files
(`execution_events.go` vs `loop_messages.go`); that is allowed.
No two leaves in group A may write the same file.

### Phase 2: Review and Commit Each Child

Review in-session (main model, never a delegated reviewer). Check leaf tasks,
contracts, exact write-targets, gate tests unchanged, test output. Pass:
commit + status REVIEWED. Gaps: re-dispatch with specific findings, max 3 cycles.

### Phase 3: Integration Branch 04

After branches 01-03 reach REVIEWED, dispatch branch 04. It owns the only
`components.go` edit in this tree. Verify `make graphs`, `make test`, and the
e2e suite. Set COMPLETE and report to the operator.

## Review Checklist

- [x] Every leaf task implemented; gate tests pass GREEN and are unmodified
- [x] C1-C5 satisfied exactly (signatures, JSON tags, topic names, sources)
- [x] Bot result uses `bot.result`; employee result uses `employee.result`
- [x] No `kind` field or `ExecutionKind` type exists anywhere
- [x] Both tiers emit turn-state edges on `agent.turn.state`
- [x] `bot.result` and `employee.result` reach WS as `agent_progress`
- [x] Bots appear in `send_agent_message` roster and drain at turn start
- [x] No previous #139 behavior changed (delegate/pair/ACP suites still green)
- [x] `make graphs` regenerated; no new package dir added
- [x] No debug artifacts, TODOs, placeholders, or line-number corruption

## Coding Conventions

- **Language:** Go 1.27 (see go.mod); no new dependencies
- **Typed bus:** stable payloads use `bus.PublishT` / `bus.Topic[T]`; no `map[string]any` in `Topic[T]`
- **Nil safety:** every `Set*` needs a nil guard; nil bus is a no-op
- **Mutex scope:** publish outside locks; no I/O under mutex
- **Errors:** wrap with `fmt.Errorf("...: %w", err)`; never ignore errors
- **Tests:** e2e tier only for new features (`e2e/suites/...`, `//go:build e2e`); gate tests are new files
- **Formatting:** `gofmt` on all Go files before report
- **Git:** leaf agents do NOT commit and do NOT run `git add`

## Requirement Traceability

| Req | Source (user words, verbatim when possible) | Plan element (leaf/task/contract) | Verified by |
|-----|---------------------------------------------|-----------------------------------|-------------|
| R1 | "keep both tiers" | 02-emitters/01-bot-runtime.md + 02-employee-emitters.md | both emit on their own topic |
| R2 | "both bots and employees have the capabilities outlined in issue 140" | C1 (two topics), C2, C4, C5; all impl leaves | e2e execution-observability |
| R3 | result event consumable via WS relay / topology | 01-wire-contract/02-ws-relay.md + 04-integration/02-docs-and-graphs.md | e2e WS frames + `make graphs-check` |
| R4 | `send_agent_message` accepts a bot ID; bot sees message next run | 03-inbox/01-roster-and-drainer.md | gate tests + e2e inbox scenario |
| R5 | same observability surface as delegate/pair/employee | C1 + C2 + C5 | e2e + turn_state_test.go unchanged |
| R6 | e2e coverage in hermetic tier | 00T-e2e-gate.md + manifest entry | `make e2e-fast-area AREA=execution-observability` |
| R7 | "employees" explicitly included | 02-employees emitter + drainer propagation | employee emitter gate tests |
| R8 | "separate" result topics (operator decision) | C1 two Topic vars + C5 two subscriptions | F1/F2 gate + e2e |

## Completion Tracking Table

| Child | Status | Iterations | Review Notes |
|-------|--------|------------|-------------|
| 00T-e2e-gate | COMPLETE | - | e2e execution-observability green 2026-11-18 |
| 01-wire-contract | COMPLETE | - | C1/C5 landed; WS relay + decodeExecutionResultWSClass |
| 02-execution-emitters | COMPLETE | - | bot.result + employee.result emitters live; trigger handler activated |
| 03-inbox | COMPLETE | - | bots in send_agent_message roster; turn-start drainer on shared loop |
| 04-integration | COMPLETE | - | daemon wiring live; graphs regenerated; docs/workflows updated |

## Integration Test Plan

1. `make test` - full unit suite, `-p 2` on macOS.
2. `make e2e-fast-area AREA=execution-observability` - new hermetic suite.
3. `make e2e-fast-area AREA=inter-agent-coord` - #139 regression check.
4. `make graphs && make graphs-check` - `bot.result` AND `employee.result` in topology.
5. `make lint-ci` - analyzers and audit scripts.
6. Manual WS check: subscribe to both result topics and to `agent.turn.state`,
   trigger a bot round and an employee round, assert `type:"agent_progress"`
   frames on the correct topic for each.

## Test Gate

Test leaves: `00T-e2e-gate.md` (root), `01-wire-contract/00T-wire-tests.md`,
`02-execution-emitters/00T-emitter-tests.md`, `03-inbox/00T-inbox-tests.md`,
`04-integration/00T-wiring-tests.md`.

| # | Feature | Source | Unit tests | E2E tests | Status |
|---|---------|--------|-----------|-----------|--------|
| F1 | Frozen shared result payload + two topics | 01-wire-contract/01 | TestExecutionResultEvent_JSONShape, TestResultTopics_Names | execution-observability-01 | COMPLETE |
| F2 | WS decoders + both subscriptions | 01-wire-contract/02 | TestDecodeExecutionResultWSClass | execution-observability-01 | COMPLETE |
| F3 | Bot executes and emits on bot.result | 02-execution-emitters/01 | TestBotRunner_EmitsResultAndTurnState | execution-observability-02 | COMPLETE |
| F4 | Employee executes and emits on employee.result | 02-execution-emitters/02 | TestGoalLoop_EmitsResultAndTurnState | execution-observability-03 | COMPLETE |
| F5 | Bots in send_agent_message roster | 03-inbox/01 | TestRoster_IncludesBotID | execution-observability-04 | COMPLETE |
| F6 | Turn-start drain on bot/employee clone | 03-inbox/01 | TestClone_InheritsDrainer | execution-observability-04 | COMPLETE |
| F7 | Daemon wires bus onto both emitters | 04-integration/01 | TestComponents_WiresExecutionBus | execution-observability-01..04 | COMPLETE |
| F8 | Topology/doc freshness | 04-integration/02 | n/a (generated) | `make graphs-check` | COMPLETE |

Delta log: 2026-11-18 — tree verified complete against HEAD (9aeadb0c): `make e2e-fast-area AREA=execution-observability` ok (18.8s), `make e2e-fast-area AREA=inter-agent-coord` ok (20.6s), `go test -p 2 ./internal/bot ./internal/agent ./internal/comm/http ./internal/daemon` ok, `make graphs-check` ok after regeneration (line-offset drift + one stale turnstate.go entry). Review checklist items all verified; no kind/ExecutionKind present.

## Open Questions

- Resolved: operator confirmed BOTH decisions.
  1. Bot trigger-handler activation (02-execution-emitters/03-trigger-handler.md)
     is IN SCOPE.
  2. Result topics are SEPARATE: `bot.result` and `employee.result`. No `kind`
     discriminator. This is now frozen in C1/C5; do not reintroduce `kind`.

## Frozen Documents

Leaf documents freeze at dispatch. Corrections ride dispatch context as dated
addenda. Never edit a dispatched leaf mid-execution.

## Notes

- Existing `docs/workflows/bots.md` says deprecated. This tree deliberately
  keeps the tier and updates that doc in branch 04.
- Do not add a new package directory; the new files live in existing packages,
  so the pre-commit new-package census stays quiet.
