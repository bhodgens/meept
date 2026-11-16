# ACP Session Updates + StopReason Plumbing - Implementation Leaf

> **For the implementing agent:** Implement ALL tasks below using TDD. The gate tests already exist and are committed - make them pass; do NOT delete, skip, weaken, or rewrite any gate test. Do NOT commit. Do NOT git add. Do NOT use read_file on existing source files - explore with search_files or terminal cat. After writing a file, do NOT read it back - write once and stop. Report what you built, files touched, and any deviations.

## Meta

- **Parent:** ../orchestrator.md
- **Scope:** ACP bridge: plumb `stopReason` through Session.Send (C3), add `AgentProgressUpdate` type (C5), handle plan/agent_thought_chunk/available_commands_update in onNotice (C5), emit TurnStateEvent at ACP turn boundaries (F5).
- **Dependencies:** 02-01 REVIEWED and committed (it landed the Prompt-boundary turn-state call + turnstate.go helper); 01-01 (contract)
- **Estimated Context:** 55K
- **Concurrency Group:** B (serial after 02-01 — shared file session.go)
- **File ownership:** you own `internal/acp/session.go` (onNotice + Prompt stopReason + Send signature) and `internal/acp/protocol.go` (AgentProgressUpdate type). 02-01 already landed its Prompt turn-state call — do not duplicate it.

## Goal

Close the ACP-side gaps: stopReason is no longer discarded (F3), the remaining session/update variants produce SessionEvents (F4), and ACP turn boundaries publish TurnStateEvent (F5).

## Context

- `internal/acp/session.go:296-319` — Prompt: decodes `stopReason` into `res` and drops it. Change: emit `SessionEvent{Kind: "turn_end", Text: res.StopReason}`; change Send signature to return stopReason.
- `internal/acp/session.go:376-404` — onNotice: switch handles agent_message_chunk + tool_call/tool_call_update. Add: plan, agent_thought_chunk, available_commands_update.
- `internal/acp/session.go:322-330` — Cancel: no state event. Emit TurnStateIdle+StopReasonCancelled on cancel.
- `internal/acp/protocol.go:183-196` — SessionUpdate struct (add AgentProgressUpdate beside it, per C5).
- `internal/acp/session.go:70-75` — SessionEvent struct (Kind + Text; already exists).
- `internal/agent/turnstate.go` — `publishTurnState` helper (02-01). The session needs a bus or a publisher func — use the nil-safe setter pattern from 02-01 T5 (`SetTurnStatePublisher`).

## Verified Anchors

- `internal/acp/session.go:299-318` — Prompt stopReason decode + discard (exact target).
- `internal/acp/session.go:392-403` — onNotice switch (exact target).
- `internal/acp/protocol.go:183-196` — SessionUpdate struct (add beside).
- `internal/acp/session.go:70-75` — SessionEvent{Kind, Text}.
- `internal/acp/manager.go` — where sessions are constructed (wire the turn-state publisher here if a session bus exists; otherwise the daemon does it in 04-01).

## Interface Contracts (From Parent)

### What This Leaf Exposes

```go
// internal/acp/protocol.go
type AgentProgressUpdate struct {
    SessionUpdate string          `json:"sessionUpdate"`
    MessageID     string          `json:"messageId,omitempty"`
    PlanID        string          `json:"planId,omitempty"`
    Content       json.RawMessage `json:"content,omitempty"`
    Title         string          `json:"title,omitempty"`
}

// internal/acp/session.go
// Session.Send signature becomes:
func (s *Session) Send(ctx context.Context, text string) (string, StopReason, error) // StopReason from agent package? NO — acp cannot import agent (cycle). Use a local string alias:
//   acp.StopReason string (values end_turn/cancelled/max_tokens/refusal); the agent layer maps it to agent.StopReason in 04-01 wiring.
// SessionEvent gains Kind values: "turn_end" (Text = stopReason), "plan", "thought", "commands".
// SetTurnStatePublisher(fn func(AgentID, State, StopReason, Phase string)) — nil-safe setter; the fn is wired by 04-01 to agent.publishTurnState.
```

> NOTE: `internal/acp` must NOT import `internal/agent` (agent imports acp — cycle). The ACP layer defines its own string-typed `StopReason` and `TurnStatePublisher` func signature; the daemon (04-01) adapts them to `agent.StopReason`/`agent.publishTurnState`.

### What This Leaf Consumes

- C1 (01-01) via the string layer (no direct import).
- 02-01's turnstate.go + SetTurnStatePublisher pattern.

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | AgentProgressUpdate type | Modify: `internal/acp/protocol.go` | `go build ./internal/acp/...` | (compile-only; no RED) | PASS: type compiles with JSON tags | Given the struct; When marshaled; Then snake/camel keys per tags |
| T2 | onNotice handles plan/thought/commands | Modify: `internal/acp/session.go` | `go test ./internal/acp/ -run 'TestACPSession_UpdateKinds' -v` | FAIL: only 2 kinds | PASS: 3 new kinds emit SessionEvents | Given scripted session/update; When onNotice; Then Kind=plan/thought/commands |
| T3 | Prompt plumbs stopReason; Send returns it | Modify: `internal/acp/session.go` | `go test ./internal/acp/ -run 'TestACPPrompt_StopReason' -v` | FAIL: Send returns 2 values | PASS: Send returns (out, "end_turn", nil) | Given fake transport; When Send; Then stopReason returned |
| T4 | ACP turn emits TurnStateEvent (running→idle) | Modify: `internal/acp/session.go` | `go test ./internal/acp/ -run 'TestACPTurn_EmitsState' -v` | FAIL: no emission | PASS: running at start, idle+stopReason at end | Given session with publisher wired; When Send; Then publisher called twice |
| T5 | Cancel emits idle+cancelled | Modify: `internal/acp/session.go` | `go test ./internal/acp/ -run 'TestACPCancel_EmitsState' -v` | FAIL: no emission | PASS: publisher called with idle+cancelled | Given a live session; When Cancel; Then event observed |

**T2 expansion:** extend the switch at :392:
```
case "plan":                    emit{Kind:"plan", Text: renderPlan(p.Update)}
case "agent_thought_chunk":     emit{Kind:"thought", Text: p.Update.Content.Text}
case "available_commands_update": emit{Kind:"commands", Text: renderCommands(p.Update)}
```
renderPlan/renderCommands: small local helpers (fmt.Sprintf of title/entries). The decode struct in onNotice gains `Content json.RawMessage` + `Entries`/`Commands` fields as needed — extend the anonymous struct, do not break agent_message_chunk/tool_call cases.

**T3 expansion:** change Prompt to:
```go
var res struct{ StopReason string `json:"stopReason"` }
...
s.emit(SessionEvent{Kind: "turn_end", Text: res.StopReason})
return out.String(), StopReason(res.StopReason), nil
```
Update ALL callers of Send: `internal/agent/delegate.go:646` (runACPChild), `internal/agent/acp_executor.go` (runACPTurn), and any test callers. The agent-side callers map `acp.StopReason` → `agent.StopReason` (string cast; the values are identical).

**T4 expansion:** wire `SetTurnStatePublisher` (nil-safe). At Prompt entry: `s.turnStatePub(s.AgentID(), "running", "", "")`. At exit: `s.turnStatePub(s.AgentID(), "idle", res.StopReason, "")`. At Cancel: `s.turnStatePub(s.AgentID(), "idle", "cancelled", "")`.

## Self-Verification Checklist

- [ ] `go build ./internal/acp/... ./internal/agent/...` clean (agent compiles against the new Send signature)
- [ ] `go test ./internal/acp/ -run 'StopReason|UpdateKinds|EmitsState' -v` — PASS
- [ ] No import of internal/agent from internal/acp (cycle check: `go list -deps ./internal/acp | grep internal/agent` — must be empty)
- [ ] Gate tests untouched
- [ ] `go fmt` applied; only session.go + protocol.go modified

**DO NOT COMMIT.**

## Completion Report Format

1. **Files touched:** [paths]
2. **Verify command + output:** [last 20 lines]
3. **Deviations:** [none / list — especially any caller updates for the Send signature change]

## Review Checklist (For Review Agent)

- [ ] AgentProgressUpdate exact per C5
- [ ] onNotice handles all 5 kinds without breaking the original 2
- [ ] Send signature changed; all callers updated; stopReason plumbed
- [ ] Turn-state emission at Prompt/Cancel boundaries
- [ ] No acp→agent import; gate tests untouched; no line-number corruption

Output: APPROVED or specific gaps.