# Routing Telemetry - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit — the orchestrator
> handles all git operations after review. Do NOT use read_file on existing
> source files — explore with search_files or terminal cat. After writing
> a file, do NOT read it back to verify — write once and stop.

## Meta

- **Parent:** ../master.md
- **Scope:** RoutingTelemetry recorder + wire points in selectAgent, assignStepAgent, and HandleHandoff.
- **Dependencies:** none
- **Estimated Context:** 40K
- **Concurrency Group:** B

## Goal

Nothing records how routing decisions are made: hint-table hits,
chat-fallbacks, explicit overrides, or handoff acceptance. Those numbers
decide whether quota-aware re-routing or hint relaxation is ever justified.
This leaf adds a fire-and-forget recorder publishing three bus topics:
`routing.decision`, `routing.hint_miss`, `routing.handoff_outcome`, and
wires it at three call sites. It never blocks routing and never fails a
step.

## Context

- internal/agent/tactical.go regions: selectAgent :2502-2507,
  assignStepAgent :2515-2519, HandleHandoff :3007-3212 (publish at
  :3196 `task.handoff_created` is the success point; the failure paths
  are the error returns).
- TacticalScheduler construction: grep `func NewTacticalScheduler` —
  construct the recorder there when bus != nil. If there is no constructor
  (struct built literally), add an unexported field initialized lazily in
  publishEvent's style: nil-guarded, created in NewTacticalScheduler if it
  exists, else in a small `ensureTelemetry` helper. Report which shape you
  found.
- Sibling-leaf region ownership: quota-aware leaf adds lines at the BOTTOM
  of selectAgent's hit branch; successor-hints leaf owns
  stepJobPayloadFromStep + OnJobCompleted. You own the TOP of each
  selectAgent branch + assignStepAgent + HandleHandoff. Read current file
  state before editing (siblings may have landed first) and preserve
  their lines.

Key files:
- internal/agent/routing_telemetry.go — NEW
- internal/agent/tactical.go — three wire points + field/constructor

## Interface Contracts (From Parent)

### What This Leaf Exposes

```go
// File: internal/agent/routing_telemetry.go (NEW)
package agent

// RoutingTelemetry records routing decisions as bus events.
// Fire-and-forget: nil bus = all methods no-op; publish failures are
// logged at Debug and swallowed (telemetry must never fail a step).
type RoutingTelemetry struct {
    bus    *bus.MessageBus
    logger *slog.Logger
}

func NewRoutingTelemetry(bus *bus.MessageBus, logger *slog.Logger) *RoutingTelemetry

// RecordHintRoute publishes "routing.decision":
//   {step_id, agent_id, source} — source ∈ "hint_table"|"explicit"|"chat_fallback"
func (rt *RoutingTelemetry) RecordHintRoute(stepID, agentID, source string)

// RecordHintMiss publishes "routing.hint_miss":
//   {step_id, tool_hint, resolved_to: "chat"}
func (rt *RoutingTelemetry) RecordHintMiss(stepID, toolHint string)

// RecordHandoff publishes "routing.handoff_outcome":
//   {task_id, from_step_id, to_agent_id, accepted}
func (rt *RoutingTelemetry) RecordHandoff(taskID, fromStepID, toAgentID string, accepted bool)
```

Wire points (all nil-guarded `if ts.telemetry != nil`):
- selectAgent: hint-table hit branch → RecordHintRoute(step.ID, agentID,
  "hint_table"); chat fallback → RecordHintMiss(step.ID, step.ToolHint)
  AND RecordHintRoute(step.ID, chat, "chat_fallback").
- assignStepAgent: the explicit-AgentID branch (step.AgentID != "" when
  entering) → RecordHintRoute(step.ID, step.AgentID, "explicit").
- HandleHandoff: at the successful creation point (just before/after the
  existing task.handoff_created publish) → RecordHandoff(..., accepted=true);
  at the rate-limit rejection (tactical.go:3032-3039) and the amendment
  rejection → RecordHandoff(..., accepted=false).

Topics added this leaf: `routing.decision`, `routing.hint_miss`,
`routing.handoff_outcome`. The `routing.telemetry` topic name from the
successor-hints leaf is SEPARATE and owned by that leaf — do not publish it.

### What This Leaf Consumes

Nothing from siblings.

## Tasks

### Task 1: RoutingTelemetry type

**Objective:** Recorder exists, nil-bus no-op, publishes correct payloads.

**Files:**
- Create: internal/agent/routing_telemetry.go
- Test: internal/agent/routing_telemetry_test.go (NEW)

**Step 1: Failing test** — real bus.MessageBus from internal/bus with a
test subscription on each topic (follow how tactical tests capture
publishEvent traffic). Table-driven: each Record* method → one event with
exact payload keys. Nil-bus instance: no panic.

**Step 2: FAIL. Step 3: Implement. Step 4: PASS.**

### Task 2: Wire points in tactical scheduler

**Objective:** Decisions observable at all three sites.

**Files:**
- Modify: internal/agent/tactical.go
- Test: internal/agent/routing_telemetry_test.go

**Step 1: Failing test** — construct TacticalScheduler with bus; run
selectAgent against (a) a routed hint ("code"), (b) an unrouted hint
("nonexistent_hint_xyz"), (c) an explicitly assigned step through
assignStepAgent; assert the expected routing.decision/hint_miss events.
HandleHandoff: run one accepted handoff (direct path, amendment path off)
and one rate-limited rejection (set maxHandoffSteps=0... check the
constructor for how the cap is configured; if 0 means disabled rather
than "reject all", construct with cap=1 and submit two); assert
handoff_outcome events with accepted true/false.

**Step 2: FAIL. Step 3: Implement** (field + constructor/init + three
call sites). **Step 4: PASS.**

### Task 3: Metadata freshness

**Objective:** The connectivity graph knows the new topics.

**Files:**
- Modify: docs/generated/* IF the generator requires a registration list
  in source (check scripts/gen-connectivity-graph.py inputs). If topics
  are discovered automatically from source, only run the make target —
  the ORCHESTRATOR runs `make graphs` at integration; you only verify
  the publish calls use the string literals above verbatim (the
  generator greps source).

## Self-Verification Checklist

- [ ] Type + three methods, nil-bus no-op, exact payload keys
- [ ] Three wire points, all nil-guarded
- [ ] selectAgent/assignStepAgent behavior otherwise unchanged
- [ ] Handoff rejection paths record accepted=false
- [ ] `go test ./internal/agent/ -run RoutingTelemetry -short` green

**DO NOT COMMIT.**

**Deviations from spec:** [none / list with rationale]

## Review Checklist (For Review Agent)

- [ ] Tasks implemented, tests passing
- [ ] Contracts match exactly
- [ ] Sibling leaf edits in tactical.go preserved

## Notes

- TacticalScheduler.publishEvent (tactical.go:2528) already nil-guards the
  bus and wraps in a models.BusMessage — REUSE it from wire points instead
  of publishing directly; RoutingTelemetry's own publish can delegate to a
  small helper or replicate the pattern.
- Do not add a metrics-store dependency in this leaf (deferred; report it
  as a possible follow-up).
