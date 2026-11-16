# Coordination Contract - Implementation Leaf

> **For the implementing agent:** Implement ALL tasks below using TDD. The gate tests already exist at `internal/agent/coordination_test.go` (committed) - your job is to make them pass; do NOT delete, skip, weaken, or rewrite any gate test. Do NOT commit. Do NOT git add. Do NOT use read_file on existing source files - explore with search_files or terminal cat. After writing a file, do NOT read it back - write once and stop. Report what you built, files touched, and any deviations.

## Meta

- **Parent:** ../orchestrator.md
- **Scope:** Create `internal/agent/coordination.go` defining the inter-agent coordination vocabulary: TurnState, StopReason, TurnStateEvent, TopicTurnState.
- **Dependencies:** 00T-unit-tests (gate tests committed)
- **Estimated Context:** 55K
- **Concurrency Group:** A
- **Audit references:** C1

## Goal

One new file that every other branch imports for the shared vocabulary. The file compiles standalone; the topic has no publisher yet (that lands in 02/03), which is fine.

## Context

- `internal/agent/coordination.go` — NEW file, package `agent`.
- `internal/agent/topics.go:14` — the `bus.NewTopic[T]` pattern to mirror.
- `internal/agent/handler.go:320` — the `WSClass()` marker pattern to mirror.
- `internal/bus/topic.go` — `NewTopic`, `Topic[T]{Name string}`.
- `internal/comm/wsclass` — `WSProgress` constant.

## Verified Anchors

- `internal/agent/topics.go:14` — `var TopicTurnTerminal = bus.NewTopic[TurnTerminalEvent]("turn.terminal")`
- `internal/agent/handler.go:320` — `func (TurnTerminalEvent) WSClass() wsclass.WSClass { return wsclass.WSProgress }`
- `internal/agent/handler.go:284-316` — TurnTerminalEvent struct with JSON tags (the shape to model TurnStateEvent on).

## Interface Contracts (From Parent)

### What This Leaf Exposes

```go
package agent

import (
    "time"

    "github.com/caimlas/meept/internal/bus"
    "github.com/caimlas/meept/internal/comm/wsclass"
)

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

type TurnStateEvent struct {
    AgentID    string     `json:"agent_id"`
    SessionID  string     `json:"session_id,omitempty"`
    ChildID    string     `json:"child_id,omitempty"`
    State      TurnState  `json:"state"`
    StopReason StopReason `json:"stop_reason,omitempty"`
    Phase      string     `json:"phase,omitempty"`
    Timestamp  time.Time  `json:"timestamp"`
}

func (TurnStateEvent) WSClass() wsclass.WSClass { return wsclass.WSProgress }

var TopicTurnState = bus.NewTopic[TurnStateEvent]("agent.turn.state")
```

### What This Leaf Consumes

- None (self-contained; imports bus + wsclass only).

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Define TurnState + StopReason with exact constants | Create: `internal/agent/coordination.go` | `go test ./internal/agent/ -run 'TestTurnState_Marshal\|TestStopReason_Marshal' -v` | Gate test fails "undefined: TurnState" until constants exist | PASS: 9 marshal rows green | Given the C1 constants; When the gate tests marshal each value; Then wire strings match |
| T2 | Define TurnStateEvent + WSClass marker | Modify: `internal/agent/coordination.go` | `go test ./internal/agent/ -run 'TestTurnStateEvent_WSClass' -v` | Gate test fails "undefined: TurnStateEvent" | PASS: field marshaling + WSClass==WSProgress | Given the struct with JSON tags; When marshaled + WSClass called; Then keys/values correct |
| T3 | Declare TopicTurnState typed topic | Modify: `internal/agent/coordination.go` | `go test ./internal/agent/ -run 'TestTopicTurnState_Name' -v` | Gate test fails "undefined: TopicTurnState" | PASS: Name=="agent.turn.state" | Given TopicTurnState; When .Name read; Then exact string |

**T1 expansion:**
1. Write `internal/agent/coordination.go` with package decl, imports (time, bus, wsclass), and the TurnState + StopReason type + const blocks from the contract.
2. Run: `go test ./internal/agent/ -run 'TestTurnState_Marshal|TestStopReason_Marshal' -v` — expect PASS (both test funcs green).
3. If the gate test file references a symbol not yet written (e.g. TurnStateEvent in the same file), expect that test to still fail until T2 — that is fine.

**T2 expansion:**
1. Add TurnStateEvent struct + WSClass method.
2. Run: `go test ./internal/agent/ -run 'TestTurnStateEvent_WSClass' -v` — expect PASS.

**T3 expansion:**
1. Add `var TopicTurnState = bus.NewTopic[TurnStateEvent]("agent.turn.state")`.
2. Run: `go test ./internal/agent/ -run 'TestTopicTurnState_Name' -v` — expect PASS.

## Self-Verification Checklist

- [ ] `go build ./internal/agent/...` clean
- [ ] `go vet ./internal/agent/...` clean
- [ ] `go test ./internal/agent/ -run 'TestTurnState|TestStopReason|TestTopicTurnState' -v` — all PASS
- [ ] Contract signatures match C1 exactly (names, constants, JSON tags, WSClass, topic name)
- [ ] `go fmt ./internal/agent/` applied
- [ ] No other files modified

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

## Completion Report Format

1. **Files touched:** [created/modified paths]
2. **Verify command + output:** [last 20 lines]
3. **Deviations:** [none / list with rationale]

## Review Checklist (For Review Agent)

- [ ] Contract types/constants/tags match C1 verbatim
- [ ] WSClass marker present and returns wsclass.WSProgress
- [ ] Topic name exactly "agent.turn.state"
- [ ] Gate tests untouched (`git diff` shows no change to coordination_test.go by implementer)
- [ ] No scope creep; no line-number corruption

Output: APPROVED or specific gaps with file + line references.