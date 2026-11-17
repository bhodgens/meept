# Wire Contract + WS Relay - Implementation Orchestrator

> **For the executing agent:** You are the orchestrator for this branch.
> Dispatch implementation agents, review their work, track completion.
> Do NOT implement code yourself. Full detail: skill `hierarchical-plan-execution`.

## Meta

- **Role:** Branch
- **Parent:** ../master.md
- **Children:** 3 leaves (test gate, type, WS relay)
- **Scope:** Define the shared result payload, two typed topics, and WS visibility

## Goal

Create the shared execution-result wire type in `internal/agent`, declare TWO
typed topics (`bot.result`, `employee.result`), then register both with the WS
relay so result frames classify as `agent_progress`. This branch owns all
definition of C1. No other branch may edit the event type or the topic vars.

## Architecture

`internal/agent` is the safe home: both `internal/bot` and `internal/employee`
already import it, and `internal/comm/http` already imports it. The event type
sits beside `TurnStateEvent`. The two topics share ONE payload struct; the topic
name identifies the tier, so there is deliberately NO `kind` field. The WS relay
uses the typed-payload decoder map and TWO explicit subscriptions
(`bot.result`, `employee.result`); the single-segment `*` misses both. Per
`internal/comm/AGENTS.md`, classification comes from the payload's `WSClass()`
marker and must be `agent_progress`, never `chat_message`.

## Interface Contracts

### C1: ExecutionResultEvent + two topics

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
```
NO `kind` field and NO `ExecutionKind` type. Owner: `01-result-event-type.md`.
Consumers: WS relay, both emitter branches, e2e gate.

### C5: WS registration

```
agent.TopicBotResult.Name:      decodeExecutionResultWSClass,
agent.TopicEmployeeResult.Name: decodeExecutionResultWSClass,
```
plus `"bot.result"` AND `"employee.result"` in the `topics` slice at
`internal/comm/http/server.go:740-799`. One decoder function serves both topics.

## Child Index

| # | Document | Type | Dependencies | Est. Context | Concurrency |
|---|----------|------|-------------|-------------|-------------|
| 00T | 00T-wire-tests.md | leaf (test) | none | 30K | A |
| 01 | 01-result-event-type.md | leaf | 00T | 35K | A |
| 02 | 02-ws-relay.md | leaf | 00T,01 | 35K | A |

Same-letter children have no inter-dependencies. Within the branch, 01 and 02
are serialized because 02 declares both topic names from 01.

## Dispatch Protocol

### Phase 0: Test Gate

Dispatch `00T-wire-tests.md` first. Validate RED in-session against Test Gate
F1/F2. Commit gate tests; mark TESTS_VALIDATED.

### Phase 1: Dispatch Group A

Dispatch 01, then 02 after 01 reaches REVIEWED. Each brief carries C1 and C5
in full plus the no-commit rule.

### Phase 2-3: Review and Integration

Review in-session after each leaf. After both REVIEWED, verify both decoder
entries compile and both topics are in the WS subscription list. Set COMPLETE.

## Review Checklist

- [ ] C1 JSON tags and StopReason mapping exactly as frozen
- [ ] TWO topic vars with exact names `bot.result` and `employee.result`
- [ ] NO `kind` field and NO `ExecutionKind` type
- [ ] `WSClass()` returns `WSProgress`
- [ ] Both decoder entries registered; both topics in WS subscription list
- [ ] No `chat_message` classification; no changes to delegate/pair/ACP topics

## Coding Conventions

- Go 1.27; `gofmt`; typed bus via `bus.PublishT`
- Define topics as package vars beside the struct, like `topics.go` precedent
- No new dependencies; no new package
- Leaf agents do NOT commit and do NOT run `git add`

## Requirement Traceability

| Req | Source | Plan element | Verified by |
|-----|--------|--------------|-------------|
| R2/R3/R8 | separate result topics, WS consumable | 01 + 02 | F1/F2 gate tests |

## Completion Tracking Table

| Child | Status | Iterations | Review Notes |
|-------|--------|------------|-------------|
| 00T-wire-tests | PENDING | 0 | |
| 01-result-event-type | PENDING | 0 | |
| 02-ws-relay | PENDING | 0 | |

## Integration Test Plan

`go test -p 2 ./internal/agent/... ./internal/comm/... -count=1` and
`go test -tags e2e ./e2e/suites/execution-observability/... -run ResultEvent`.

## Test Gate

| # | Feature | Source | Unit tests | E2E tests | Status |
|---|---------|--------|-----------|-----------|--------|
| F1 | Shared payload + two topic names | 01-result-event-type.md | TestExecutionResultEvent_JSONShape, TestResultTopics_Names | execution-observability-01 | PENDING |
| F2 | One decoder, two subscriptions | 02-ws-relay.md | TestDecodeExecutionResultWSClass | execution-observability-01 | PENDING |

Delta log: none yet.

## Open Questions

- Resolved: operator chose SEPARATE topics. No `kind` field. Frozen in C1/C5.

## Notes

The result event uses `int64 duration_ms` so the JSON wire shape is an integer,
matching other bus events. The topic name is the ONLY tier discriminator.
