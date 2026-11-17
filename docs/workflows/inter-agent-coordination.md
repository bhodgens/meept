# Inter-Agent Coordination (turn state, stop reasons, structured inbox)

Shared vocabulary for cross-agent observation and messaging: turn-state
edges, stop reasons, the typed ACP progress bridge, the structured inbox
envelope, and the field-keyed structured handoff scan. Everything here is
defined once in `internal/agent` (`coordination.go`, `turnstate.go`) and
adopted by the delegate, pair/team, ACP-bridge, and employee-messaging
surfaces. Scope of issue #139 (plan tree
`docs/plans/20261115-inter-agent-coordination/`); #140 (bot result/inbox)
consumes the same shapes in a follow-up.

## Bus topic: `agent.turn.state`

Typed topic (C6):

```go
// internal/agent/coordination.go
var TopicTurnState = bus.NewTopic[TurnStateEvent]("agent.turn.state")
```

- 3-segment topic: the WS relay subscribes to the explicit literal
  `"agent.turn.state"` in `WithWebSocket`
  (`internal/comm/http/server.go`) — the single-segment `*` and the
  2-segment `agent.*` never match it.
- Publish via `bus.PublishT(bus, agent.TopicTurnState, source, ev)`;
  subscribe via `bus.SubscribeT`. The compiler enforces payload
  agreement on both sides (internal/comm/AGENTS.md, typed-topics rule).
- Transport shapes: bus-holding publishers (PairOrchestrator, the daemon
  ACP wiring) publish the typed event directly; DelegateManager holds
  only the generic `publisher func(topic, payload)` seam and passes the
  struct through unmarshalled (`internal/agent/turnstate.go`).

## Vocabulary

### `AgentTurnState` — the lifecycle state of one agent's turn

| Value | Meaning |
|-------|---------|
| `running` | a turn is in flight |
| `idle` | no turn in flight (settled/completed) |
| `requires_action` | the agent stopped needing input (refusal/validation) |

### `StopReason` — why a turn left `running`

| Value | Meaning |
|-------|---------|
| `end_turn` | the agent finished normally |
| `cancelled` | caller/user cancelled |
| `max_tokens` | token budget exhausted |
| `refusal` | the model refused |
| `max_iterations` | fix-loop/iteration cap hit |
| `error` | any other failure |

## `TurnStateEvent` payload

```go
// internal/agent/coordination.go
type TurnStateEvent struct {
    AgentID    string         `json:"agent_id"`
    SessionID  string         `json:"session_id,omitempty"`
    ChildID    string         `json:"child_id,omitempty"`
    State      AgentTurnState `json:"state"`
    StopReason StopReason     `json:"stop_reason,omitempty"`
    Phase      string         `json:"phase,omitempty"`
    Timestamp  time.Time      `json:"timestamp"`
}

func (TurnStateEvent) WSClass() wsclass.WSClass { return wsclass.WSProgress }
```

- The topic name for typed decode registration is
  `agent.TopicTurnState.Name` == `"agent.turn.state"`.
- `WSClass() == WSProgress` (the wsclass marker path, pinned by the
  `typedPayloadDecoders` gate test in `internal/comm/http`): WS clients
  receive it as `type: "agent_progress"` — never `chat_message`
  (classification invariant in internal/comm/AGENTS.md).
- A zero `Timestamp` is defaulted to `time.Now().UTC()` by the publish
  helpers, so every event carries a usable clock stamp.

## Emitters

Every producer publishes on the ONE topic; consumers never poll.

| Surface | Emission points | Where |
|---------|-----------------|-------|
| Delegate children | `running` at spawn, `idle` (or `requires_action` for refusal/validation failures) at settle, with the classified `StopReason` | `internal/agent/delegate.go` |
| Pair sessions | every phase transition: `actor_turn` / `reviewer_turn` → `running`; `completed` → `idle`+`end_turn`; `failed` → `idle`+`error` | `internal/agent/pair_orchestrator.go` (`emitTurnState`) |
| ACP sessions | prompt boundaries: `running` (phase `prompt`) on prompt entry, `idle` on prompt completion/error with the ACP-mapped `stopReason` | `internal/acp/session.go` (`emitTurnState`), armed per session via `SetTurnStatePublisher` |
| Daemon wiring | installs the ACP seam once at manager construction: every lazily created session publishes through the bus | `internal/daemon/acp_register.go` + `internal/daemon/turnstate_wiring.go` (`acp.Manager.SetOnSessionCreated`) |

Nil-guard posture: `agent.publishTurnState` (and the daemon callback) are
no-ops on a nil bus — an unwired surface stays byte-identical with the
seam installed but silent.

## ACP stopReason mapping (C5)

ACP `session/update` completions carry the agent's own `stopReason` string.
`internal/acp` does NOT import `internal/agent`, so the session seam hands
the raw strings `(agentID, state, stopReason, phase)` to the installed
publisher; the daemon-side callback casts them onto the vocabulary above
(`agent.AgentTurnState` / `agent.StopReason`). The recognized ACP
stop reasons map onto the `StopReason` table; anything unrecognized still
publishes verbatim so consumers can see novel agent dialects instead of
losing them to a silent remap.

## Employee messaging: `AgentInboxBody` (C4)

`send_agent_message` accepts an optional `structured` object argument
(`tool_targets`, `file_paths`, `plan_entries` — all optional). When
present, the stored message body becomes the typed JSON envelope;
without it, the body stays the raw text (legacy byte-identical).

```go
// internal/employee/messaging.go — the wire contract (JSON tags)
type AgentInboxBody struct {
    Body       string           `json:"body"`
    Structured *InboxStructured `json:"structured,omitempty"`
}

type InboxStructured struct {
    ToolTargets []string `json:"tool_targets,omitempty"`
    FilePaths   []string `json:"file_paths,omitempty"`
    PlanEntries []string `json:"plan_entries,omitempty"`
}
```

- Consumers decode with `employee.ParseAgentInboxBody(raw)`: any body
  that is not a valid envelope (legacy raw text, truncated JSON) falls
  back verbatim as `{Body: raw, Structured: nil}`.
- The tool layer (`internal/tools/builtin/agent_message.go`) keeps a
  local mirror of the envelope types — `builtin` must not import
  `employee` (provider-seam import rule); the mirrors' JSON tags ARE the
  shared contract and must stay identical.

## Structured handoff scan (C7)

The collaboration handoff boundary (`internal/agent/collaboration_*.go`,
`prepareHandoff`) scans agent-to-agent payloads for exfiltration-style
directives. Since the inbox envelope exists, the scan is FIELD-KEYED:
`InboxStructured.FilePaths` entries (and the envelope's `body`) are
scanned as named fields, so a blocked handoff's reason names the field
that tripped it (`file_paths[i]`), not just a blob position. A clean
structured payload passes like plain text; a structured payload carrying
a real directive pattern blocks with the field named.

## Payload-shape ratifications (2026-11-16, issue #152)

### `TurnStateEvent.phase` is optional — one struct, two shapes (Q2 ratified)

Pair phase transitions and delegate/ACP turn boundaries publish on the
ONE `agent.turn.state` topic through the ONE `TurnStateEvent` struct.
Pair events set `phase` (`actor_turn` / `reviewer_turn` / `completed` /
`failed`); plain delegate/ACP events omit it. The WS relay decodes every
payload into the frozen `TurnStateEvent`
(`internal/comm/http/server_wsclass.go` `decodeTurnStateWSClass`), and
no Dart/GUI/TUI consumer of `agent.turn.state` requires the `phase`
field — it is additive and `omitempty`. Splitting a `PairStateEvent`
would fork the topic, the WS decoder, and the wsclass marker for zero
consumer benefit, so the single-type-with-phase shape is RATIFIED.
`phase` is pinned OPTIONAL: consumers must tolerate its absence; pair
consumers discriminate on it, never on struct identity. Both shapes are
pinned to decode cleanly by `TestTurnStateEvent_BothShapesDecode`
(internal/agent/coordination_test.go).

### `available_commands_update` is progress-only — no consumer (Q3 ratified)

ACP `session/update` notifications with
`sessionUpdate == "available_commands_update"` are rendered by
`internal/acp/session.go` into a `SessionEvent{Kind: "commands"}` and
drained verbatim (kind + text) into the `acp_agent` tool's `events`
array (`internal/tools/builtin/acp_agent.go` `drainSessionEvents`).
No TUI/GUI/menubar surface renders slash-command availability from
this event today — the Flutter GUI's command palette rides its own
`_loadCustomCommands` path and never reads ACP `commands` events, and
the TUI slash autocomplete is file/config-driven. The event is
therefore RATIFIED as informational progress: it is carried for
logging/drain visibility only, with NO consumer and NO new bus topic.
If a command-palette surface for ACP sessions is ever built, it should
consume this event rather than re-derive the list.

## Observability summary

| Layer | Surface |
|-------|---------|
| Bus | `agent.turn.state` (typed `Topic[TurnStateEvent]`) |
| WS | relayed as `agent_progress` via the wsclass marker (`typedPayloadDecoders` entry + explicit relay literal) |
| Connectivity graph | `docs/generated/bus-topology.md` (regenerate with `make graphs`) |
| e2e | `e2e/suites/inter-agent-coord/` (manifest: `e2e/manifest.json`, suite `inter-agent-coord`) |
