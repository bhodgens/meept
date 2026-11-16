# Inter-Agent Coordination: Turn State, Stop Reasons, Typed Progress - Implementation Orchestrator

> **For the executing agent:** You are the orchestrator for this tree node.
> Your job: (1) dispatch implementation agents, (2) review their work,
> (3) re-dispatch if incomplete, (4) track completion.
> Do NOT implement code yourself. All implementation happens in leaf agents.
> Full operational detail: skill `hierarchical-plan-execution`.

## Meta

- **Role:** Root
- **Parent:** none (root)
- **Children:** 4 (2 branches + 2 leaves)
- **Scope:** Define and adopt a shared inter-agent coordination vocabulary (turn state, stop reasons, typed progress, structured inbox payloads) across the delegate, ACP-bridge, pair/team, and employee-messaging surfaces. Closes #139.

## Goal

Four execution surfaces speak four vocabularies today: delegate publishes an ad-hoc map on `agent.delegate.result` (delegate.go:866), the ACP bridge collapses `session/update` to message_chunk + tool_call title (session.go:392-403), pair turns are free text with verdict strings (pair_channel.go:20), and the employee inbox is untyped free text (messaging.go:35). None of them reports turn state (running/idle/requires_action) or stop reason (end_turn/cancelled/max_tokens/refusal) on the wire. A parent cannot distinguish "child needs input" from "child failed" from "child quit early" without parsing prose.

This tree defines the shared vocabulary once (branch 01: the contract types + turn-state emitter), then adopts it at each producing/consuming surface (02: ACP bridge stopReason + plan/thought updates; 03: structured inbox bodies + field-keyed taint scanning), and finally wires observability + docs + e2e (04). Scope is issue #139's three features; #140 (bot result/inbox) consumes the same shapes in a follow-up.

## Architecture

- **Contract-first.** All shared types (TurnState, StopReason, TurnStateEvent, AgentInboxBody, the per-surface payload structs) are defined in ONE new file, `internal/agent/coordination.go`, owned by leaf 01-01. Every other leaf consumes the contract IDs from this document; no leaf redefines a shared type.
- **Typed topics where stable.** New stable topics are `bus.Topic[T]` declarations (the `TopicTurnTerminal` pattern, topics.go:14). `agent.turn.state` is new and typed. Existing `agent.delegate.result` stays raw (two payload shapes today — child settlement + `kind:"workflow"`; a typed wrapper would silently zero-fill the workflow shape).
- **Additive only.** No existing payload key is removed or renamed. DelegateResult GAINS a StopReason field; existing consumers decode the map and ignore unknown keys. ACP SessionEvent GAINS kinds; the WS classifier gains cases (exhaustive linter enforced). Disabled paths stay byte-identical.
- **WS classification.** Every new WS-visible payload gets a `WSClass()` marker (wsclass pattern, handler.go:320). New topics are added to the relay subscription list in server.go and to `make graphs` output.
- **Taint scanning keys on structure.** Today `prepareHandoff` regex-scans raw peer text (collaboration_handoff.go:111). Leaf 03-02 adds an optional structured-field scan path: when the inbox body parses as an AgentInboxBody, the scanner inspects declared fields (tool target, file paths) instead of the whole blob. Free-text bodies keep today's scan byte-identically.

## Interface Contracts

### C1: TurnState + StopReason vocabulary

```go
// Owner: 01-01-coordination-contract.md
type TurnState string
const (
    TurnStateRunning        TurnState = "running"
    TurnStateIdle           TurnState = "idle"
    TurnStateRequiresAction TurnState = "requires_action"
)

type StopReason string
const (
    StopReasonEndTurn       StopReason = "end_turn"
    StopReasonCancelled     StopReason = "cancelled"
    StopReasonMaxTokens     StopReason = "max_tokens"
    StopReasonRefusal       StopReason = "refusal"
    StopReasonMaxIterations StopReason = "max_iterations"
    StopReasonError         StopReason = "error"
)

// TurnStateEvent is the payload of the agent.turn.state topic.
type TurnStateEvent struct {
    AgentID    string    `json:"agent_id"`
    SessionID  string    `json:"session_id,omitempty"`   // pair/team session, when scoped
    ChildID    string    `json:"child_id,omitempty"`     // delegate child handle
    State      TurnState `json:"state"`
    StopReason StopReason `json:"stop_reason,omitempty"`
    Phase      string    `json:"phase,omitempty"`
    Timestamp  time.Time `json:"timestamp"`
}
func (TurnStateEvent) WSClass() wsclass.WSClass { return wsclass.WSProgress }
// Consumers: 02-01, 02-02, 03-01, 04-01
```

### C2: DelegateResult gains StopReason

```go
// Owner: 01-01 (type), 02-01 (emitter)
// Modify internal/agent/delegate.go:182-189:
type DelegateResult struct {
    AgentID    string     `json:"agent_id"`
    Success    bool       `json:"success"`
    Response   string     `json:"response,omitempty"`
    Error      string     `json:"error,omitempty"`
    ChildID    string     `json:"child_id,omitempty"`
    Status     string     `json:"status,omitempty"`
    StopReason StopReason `json:"stop_reason,omitempty"` // NEW
}
// Mapping (resultFromRecord delegate.go:847):
//   completed   -> end_turn (refine: refusal if output carries a refusal marker)
//   interrupted -> cancelled
//   failed      -> error (refine: max_iterations if the loop budget tripped)
// Consumers: 02-01, 04-01
```

### C3: ACP turn result carries stopReason

```go
// Owner: 02-01 (session.go), contract from 01-01
// internal/acp/session.go Prompt() already decodes:
//   var res struct{ StopReason string `json:"stopReason"` }
// Change: SessionEvent gains Kind "turn_end" with Text = res.StopReason
// (wire values end_turn/cancelled/max_tokens/refusal map 1:1 to StopReason).
// Session.Send returns (output string, stopReason StopReason, err error).
// Consumers: 02-01, 04-01
```

### C4: AgentInboxBody (typed inbox payload)

```go
// Owner: 03-01 (type), 03-02 (scanner)
type AgentInboxBody struct {
    Body      string   `json:"body"`                 // human text (always present)
    Structured *InboxStructured `json:"structured,omitempty"`
}
type InboxStructured struct {
    ToolTargets []string `json:"tool_targets,omitempty"` // tool names the peer invoked
    FilePaths   []string `json:"file_paths,omitempty"`   // absolute paths the peer touched
    PlanEntries []string `json:"plan_entries,omitempty"` // plan/step text when the peer ran a plan
}
// Envelope: send_agent_message gains optional "structured" arg (same shape).
// Stored as JSON in agent_messages.body when present; legacy free text stays
// valid (backward compatible).
// Consumers: 03-01, 03-02, 04-01
```

### C5: Typed ACP progress payload (plan/thought)

```go
// Owner: 02-02
// New in internal/acp/protocol.go (beside SessionUpdate):
type AgentProgressUpdate struct {
    SessionUpdate string `json:"sessionUpdate"`          // plan | agent_thought_chunk | available_commands_update
    MessageID     string `json:"messageId,omitempty"`
    PlanID        string `json:"planId,omitempty"`
    Content       json.RawMessage `json:"content,omitempty"`
    Title         string `json:"title,omitempty"`
}
// onNotice (session.go:392) gains cases: plan, agent_thought_chunk,
// available_commands_update. Each emits SessionEvent{Kind: "plan"|"thought"|
// "commands", Text: <rendered>} — the acp_executor emit closure forwards them
// to agent.progress with stage "acp" (existing shape, new detail values).
// Consumers: 02-02, 04-01
```

### C6: agent.turn.state topic

```go
// Owner: 01-01
var TopicTurnState = bus.NewTopic[TurnStateEvent]("agent.turn.state")
// Published via bus.PublishT (non-blocking; progress is best-effort).
// Added to: server.go relay subscription list, wsclass switch, orphan-check
// baseline (scripts/orphan-baseline.txt), make graphs.
// Consumers: 02-01, 02-02, 03-01, 04-01
```

## Child Document Index

| # | Document | Type | Dependencies | Est. Context | Concurrency |
|---|----------|------|-------------|-------------|-------------|
| 00T | [00T-e2e-tests.md](00T-e2e-tests.md) | leaf (test gate, root) | none | 45K | A |
| 01 | [01-contract/](01-contract/orchestrator.md) | branch | 00T | - | - |
| 01-00T | [01-contract/00T-unit-tests.md](01-contract/00T-unit-tests.md) | leaf (test gate) | none | 30K | A |
| 01-01 | [01-contract/01-coordination-contract.md](01-contract/01-coordination-contract.md) | leaf | 01-00T | 55K | A |
| 02 | [02-acp-emit/](02-acp-emit/orchestrator.md) | branch | 01 | - | - |
| 02-00T | [02-acp-emit/00T-unit-tests.md](02-acp-emit/00T-unit-tests.md) | leaf (test gate) | 01 | 35K | A |
| 02-01 | [02-acp-emit/01-delegate-pair-emit.md](02-acp-emit/01-delegate-pair-emit.md) | leaf | 02-00T, 01-01 | 60K | A |
| 02-02 | [02-acp-emit/02-acp-updates.md](02-acp-emit/02-acp-updates.md) | leaf | 02-00T, 01-01 | 55K | B |
| 03 | [03-inbox/](03-inbox/orchestrator.md) | branch | 01 | - | - |
| 03-00T | [03-inbox/00T-unit-tests.md](03-inbox/00T-unit-tests.md) | leaf (test gate) | 01 | 30K | A |
| 03-01 | [03-inbox/01-typed-body.md](03-inbox/01-typed-body.md) | leaf | 03-00T, 01-01 | 50K | A |
| 03-02 | [03-inbox/02-structured-scan.md](03-inbox/02-structured-scan.md) | leaf | 03-01 | 45K | B |
| 04 | [04-wire/](04-wire/orchestrator.md) | branch | 01, 02, 03 | - | - |
| 04-01 | [04-wire/01-ws-docs-graphs.md](04-wire/01-ws-docs-graphs.md) | leaf | 02, 03 | 50K | A |

Same-letter children have no inter-dependencies and dispatch together (max 3 per batch). File-level serialization: 02-01 and 02-02 both touch internal/acp/session.go — 02-01 owns Prompt()/Cancel/Session.Send; 02-02 owns onNotice. They are file-disjoint WITHIN session.go only by function, so 02-02 dispatches AFTER 02-01 lands (Wave order: 02-01 then 02-02). Same for 03-01/03-02: 03-01 owns messaging.go + agent_message.go; 03-02 owns collaboration_handoff.go only — file-disjoint, parallel-safe, but 03-02 depends on 03-01's AgentInboxBody type, so it waits for 03-01's contract commit.

## Dispatch Protocol

For each concurrency group, in dependency order. Full recipe: skill `hierarchical-plan-execution`.

### Phase 0: Test Gate (code branches, before ANY implementation dispatch)

Dispatch test leaves first:
- Root: `00T-e2e-tests.md` (cross-branch e2e: turn state observed end-to-end, stopReason on delegate result, ACP plan update reaches WS, typed inbox round-trip, structured taint scan blocks/allows correctly).
- Branch 01: `00T-unit-tests.md` (vocabulary marshal/unmarshal, resultFromRecord mapping table, TurnStateEvent WSClass).
- Branch 02: `00T-unit-tests.md` (delegate emits state transitions; ACP plan/thought/commands events emitted; stopReason plumbed).
- Branch 03: `00T-unit-tests.md` (AgentInboxBody round-trip, legacy body compat, structured scan field-keying).

Validate in-session (never delegate review) against the Test Gate matrix; apply the delta protocol; commit gate tests; set children to TESTS_VALIDATED. Then implementation may dispatch. See references/tests-first-gate.md.

### Phase 1: Dispatch Concurrency Group A

- Wave A1: `01-00T` (contract tests). After TESTS_VALIDATED: `01-01` (the contract file). 01-01 is the hard dependency for everything else — nothing else dispatches until 01-01 is REVIEWED and committed.
- Wave A2 (after 01-01 committed): `00T` (root e2e), `02-00T`, `03-00T` in parallel. After their gates validate: `02-01`, `03-01` in parallel (file-disjoint: session.go/delegate.go/pair_orchestrator.go vs messaging.go/agent_message.go).

### Phase 2: Review and Commit Each Child

Review in-session (the main model - never a delegated reviewer). Check leaf spec, contracts, Review Checklist; run tests; gate-test diff must be empty. Pass: commit + status REVIEWED. Gaps: re-dispatch with findings, max 3 cycles, then escalate to parent.

### Phase 3: Dispatch Concurrency Group B

- Wave B1 (after 02-01 + 03-01 REVIEWED): `02-02` (ACP updates — needs 02-01's session.go changes landed), `03-02` (structured scan — needs 03-01's AgentInboxBody type). Parallel-safe (different packages).
- Wave B2 (after 02-02 + 03-02 REVIEWED): `04-01` (WS wiring + docs + graphs).

### Phase 4: Integration Review

After ALL children REVIEWED: verify contracts between children, run the full suite, check no import cycles, run `make graphs`, verify wsclass exhaustive switch, check orphan-check baseline. Gaps: re-dispatch the failing child with contract fixes (uncommitted). Pass: normalize formatting, run the line-number corruption check, commit integration changes, set children COMPLETE, report COMPLETE to parent.

## Review Checklist

The orchestrator (main model) verifies each child in-session:

- [ ] All tasks from the leaf document are implemented
- [ ] Interface contracts satisfied (signatures, types, paths)
- [ ] All specified files created/modified at exact paths
- [ ] Gate tests pass GREEN; `git diff` shows no gate-test modification (any such diff = automatic NEEDS_FIXES)
- [ ] Tests written and passing (TDD followed)
- [ ] Code follows project conventions
- [ ] No scope creep; no obvious bugs or security issues
- [ ] No debug artifacts (print debugging, TODOs, placeholders, commented-out code)
- [ ] No line-number corruption (`     N|` prefixes) in source files

Output: APPROVED or a list of specific gaps.

## Coding Conventions

- **Language/Framework:** Go (repo go.mod toolchain; generics available)
- **Naming:** exported = PascalCase; JSON tags snake_case; bus topics lowercase dot-separated
- **Imports:** grouped stdlib / internal / external; no unused (staticcheck U1000 gate)
- **Error handling:** wrap with `fmt.Errorf("op: %w", err)`; no `_ =` on non-test additions (pre-commit check [10]); nil-seam setters need `//nolint:nilseam`
- **Testing:** stdlib testing only (no testify in internal/agent); table-driven `for _, tc := range`; new feature tests go in `e2e/suites/` with `//go:build e2e` + manifest entry (no new unit-test files except gate leaves under this tree's test leaves, which are e2e-tier where the feature is user-visible)
- **Formatting:** `go fmt ./...` before reporting (no gofmt CI gate, but keep clean)
- **Commits:** orchestrator only; leaves never commit

## Requirement Traceability

| Req | Source (user words, verbatim when possible) | Plan element (leaf/task/contract) | Verified by |
|-----|---------------------------------------------|-----------------------------------|-------------|
| R1 | "turn state machine (running/idle/requires_action)" | C1 TurnState, C6 TopicTurnState; 01-01 T1-T2; emitters 02-01/02-02/03-01 | 00T e2e + unit tests |
| R2 | "stop reasons (end_turn, cancelled, max_tokens, refusal)" | C1 StopReason, C2 DelegateResult.StopReason, C3 ACP stopReason; 02-01 T2-T3 | unit mapping table + e2e |
| R3 | "handle remaining session/update variants (plan, agent_thought, available_commands)" | C5 AgentProgressUpdate; 02-02 | unit + e2e WS delivery |
| R4 | "type the inbox body so taint scan keys on fields" | C4 AgentInboxBody, C5; 03-01/03-02 | unit + e2e scan block/allow |
| R5 | "single schema doc in docs/workflows/" | 04-01 T-docs | docs gate [16] + review |
| R6 | "E2E coverage in the hermetic tier" | 00T + manifest entry | make e2e-fast green |

## Completion Tracking Table

| Child | Status | Iterations | Review Notes |
|-------|--------|------------|-------------|
| 00T-e2e-tests | PENDING | 0 | |
| 01-contract (branch) | PENDING | 0 | |
| 01-00T-unit-tests | PENDING | 0 | |
| 01-01-coordination-contract | PENDING | 0 | |
| 02-acp-emit (branch) | PENDING | 0 | |
| 02-00T-unit-tests | PENDING | 0 | |
| 02-01-delegate-pair-emit | PENDING | 0 | |
| 02-02-acp-updates | PENDING | 0 | |
| 03-inbox (branch) | PENDING | 0 | |
| 03-00T-unit-tests | PENDING | 0 | |
| 03-01-typed-body | PENDING | 0 | |
| 03-02-structured-scan | PENDING | 0 | |
| 04-wire (branch) | PENDING | 0 | |
| 04-01-ws-docs-graphs | PENDING | 0 | |

Status values: PENDING | TESTS_IN_PROGRESS | TESTS_VALIDATED | IN_PROGRESS | IMPLEMENTED | REVIEWED | COMPLETE | BLOCKED

Gate rule: implementation children stay PENDING until their branch's test-gate leaves reach TESTS_VALIDATED.

## Integration Test Plan

1. `go build ./...` — compiles with new types/topics.
2. `go test -p 2 ./internal/agent/... ./internal/acp/... ./internal/employee/... ./internal/comm/...` — unit + existing suites green.
3. `make e2e-fast` — new e2e suite green (turn state observed end-to-end via WS; delegate stopReason; ACP plan update; inbox round-trip; structured scan).
4. `make graphs` then `git diff --exit-code docs/generated/` — topology regenerated and committed.
5. `python3 scripts/orphan-check.py` — new topic `agent.turn.state` has a subscriber (WS relay) or is in the baseline.
6. `go run ./tools/analyzers/predid/... ./...` — no predictable IDs introduced (event IDs use pkg/id).
7. `grep -rn "agent.turn.state" docs/generated/bus-topology.md` — topic present with publisher + subscriber.

## Test Gate

Test leaves: 00T-e2e-tests.md (root), 01-contract/00T-unit-tests.md, 02-acp-emit/00T-unit-tests.md, 03-inbox/00T-unit-tests.md - dispatched in Phase 0, validated by the control agent (orchestrator, in-session).

| # | Feature (from plan) | Source (leaf/spec section) | Unit tests | E2E tests | Status |
|---|---------------------|----------------------------|-----------|-----------|--------|
| F1 | Turn state emitted on delegate settle (running→idle/requires_action) | 02-01 T2 | TestDelegate_EmitsTurnState | e2e: turn-state-observed | PENDING |
| F2 | StopReason on DelegateResult, mapping table | 02-01 T3 | TestResultFromRecord_StopReason | e2e: delegate-stop-reason | PENDING |
| F3 | ACP stopReason plumbed through Session.Send | 02-01 T4 | TestACPPrompt_StopReason | n/a (unit) | PENDING |
| F4 | ACP plan/thought/commands updates emitted | 02-02 T2 | TestACPSession_UpdateKinds | e2e: acp-plan-update-ws | PENDING |
| F5 | Turn state emitted on ACP turn boundaries | 02-02 T3 | TestACPTurn_EmitsState | e2e: turn-state-observed | PENDING |
| F6 | AgentInboxBody round-trip + legacy compat | 03-01 T1-T2 | TestAgentInboxBody_Roundtrip | e2e: inbox-roundtrip | PENDING |
| F7 | send_agent_message structured arg accepted | 03-01 T3 | TestSendAgentMessage_Structured | e2e: inbox-roundtrip | PENDING |
| F8 | Structured taint scan keys on fields; legacy text scan unchanged | 03-02 T1-T2 | TestHandoff_StructuredScan | e2e: scan-block-allow | PENDING |
| F9 | agent.turn.state in WS relay + graphs + baseline | 04-01 | n/a (integration) | e2e: turn-state-observed | PENDING |
| F10 | Schema doc in docs/workflows/ | 04-01 T-docs | n/a | docs gate [16] | PENDING |

Statuses: PENDING | DELTA (mismatch, resolution in progress) | VALIDATED

Delta log: [date, feature ID, direction, one-line reason]

## Frozen Documents

Leaf documents freeze at dispatch. Corrections ride dispatch context as dated addenda (dated, appended to the dispatch brief). Never edit a dispatched leaf document mid-execution.

## Open Questions

- Q1: Should `agent.turn.state` be blocking (PublishBlockingT) or drop-on-full? Rec: non-blocking PublishT (progress is best-effort; the state is derivable from the result event). Impact: if a consumer needs guaranteed delivery, flip to PublishBlockingT — a one-line change in 01-01.
- Q2: Should pair phase transitions reuse TurnStateEvent or keep pair.phase? Rec: reuse — pair phase strings (actor_turn/reviewer_turn/completed/failed) map onto running/idle; add a `phase` field for the fine-grained value (already in C1). Impact: pair consumers must handle both.
- Q3: `available_commands_update` — emit as a typed event but is there a consumer? Rec: yes — forward to agent.progress stage "acp" with detail=commands list; the TUI/GUI can render later. No new bus topic.

## Notes

- The survey corrected one assumption: GoalLoop does NOT render candidates into a prompt; candidates are parsed from model JSON and executed via plan.Prompt (goal_loop.go:747 Assess, :959 Execute). The structured-inbox seam is messaging.go + agent_message.go + collaboration_handoff.go, NOT goal_loop.
- `agent.progress` is a raw map for chat-stream text (loop.go:9457) — the plan deliberately does NOT extend it; new structure rides `agent.turn.state` and typed SessionEvent kinds.
- `agent.delegate.result` carries two payload shapes (child settlement + `kind:"workflow"`) — consumers must discriminate on `kind`; the new StopReason field is additive and workflow payloads simply omit it.
- The e2e suite name is provisional (`inter-agent-coord`); the test leaf proposes the final name + manifest entry.