# Quota-Aware Agent Selection - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit — the orchestrator
> handles all git operations after review. Do NOT use read_file on existing
> source files — explore with search_files or terminal cat. After writing
> a file, do NOT read it back to verify — write once and stop.

## Meta

- **Parent:** ../master.md
- **Scope:** Surface endpoint park/cooldown state into selectAgent as a WARNING signal (not a re-route) so parked agents are visible in routing.
- **Dependencies:** none
- **Estimated Context:** 40K
- **Concurrency Group:** B

## Goal

`selectAgent` (internal/agent/tactical.go:2502) routes hints through a
static table and never consults provider health. When the coder agent's
LLM endpoint is parked or cooling down (see internal/llm quota/park
machinery), code-hint steps still queue behind it with no signal anywhere.
This leaf adds an OPTIONAL health source: when wired, selectAgent keeps
the hint table's route (the table is authoritative for capability
matching) but publishes a `routing.warning` bus event when the selected
agent's endpoint is parked or cooling. Unwired (nil) = byte-identical
legacy. A full re-route policy is deliberately OUT of scope: it changes
task outcomes and needs product sign-off.

## Context

- internal/agent/tactical.go: TacticalScheduler; selectAgent region at
  :2495-2526 (selectAgent, assignStepAgent, SelectAgentForHint). A parallel
  telemetry leaf owns NEW call sites in selectAgent — coordinate by
  touching ONLY the region you need: you add the warning check INSIDE
  selectAgent after the hint-table hit; you do NOT restructure the function.
- The LLM layer's park/cooldown state lives in internal/llm (endpoint
  cooldowns, universal parking). Grep for how daemon code queries
  parked/cooldown state today. If a clean interface cannot be built
  without refactoring internal/llm, implement the consumer side with the
  interface defined HERE and report the missing producer — the
  orchestrator will wire it or defer wiring.

Key files:
- internal/agent/tactical.go — selectAgent region + field + setter
- internal/agent/agent_quota_aware.go — NEW: interface + tests fixture

## Interface Contracts (From Parent)

### What This Leaf Exposes

```go
// File: internal/agent/agent_quota_aware.go (NEW)
package agent

// AgentHealthSource reports whether an agent's provider endpoint is
// currently parked or in cooldown. Producer wiring is optional; nil
// source = legacy behavior.
type AgentHealthSource interface {
    AgentParkedOrCooling(agentID string) bool
}

// File: internal/agent/tactical.go
// TacticalScheduler gains unexported field:
//   health AgentHealthSource
// Setter with nil guard (setters_test.go convention):
//   func (ts *TacticalScheduler) SetAgentHealthSource(src AgentHealthSource)
// selectAgent behavior: after `if agentID, ok := config.ToolHintAgent(...); ok`:
//   if ts.health != nil && ts.health.AgentParkedOrCooling(agentID) {
//       ts.publishEvent("routing.warning", map[string]any{
//           "step_id": step.ID, "agent_id": agentID,
//           "reason":  "endpoint_parked_or_cooling",
//       })
//   }
// Route UNCHANGED. Chat fallback branch: no warning (chat has no endpoint
// mapping).
```

### What This Leaf Consumes

Nothing from sibling leaves. Your added lines in selectAgent must not
conflict with the telemetry leaf's added lines: telemetry adds calls at
the TOP of each branch; you add the warning at the BOTTOM of the hit
branch. Both are additive one-liners in distinct spots — if the file
already contains sibling edits when you arrive, read the current region
first and place yours adjacent without removing theirs.

## Tasks

### Task 1: Interface + setter + nil-guard

**Objective:** Health source pluggable; nil = legacy.

**Files:**
- Create: internal/agent/agent_quota_aware.go
- Modify: internal/agent/tactical.go (field + setter only)
- Test: internal/agent/agent_quota_aware_test.go (NEW)

**Step 1: Failing test** — construct TacticalScheduler (follow
tactical_test.go construction), SetAgentHealthSource(fake{parked:true}),
assert setter stores; SetAgentHealthSource(nil) after, assert no panic.
Also a fake implementing the interface satisfies it (var _ AgentHealthSource).

**Step 2: Run** — FAIL (no setter). **Step 3: Implement.** **Step 4: PASS.**

### Task 2: selectAgent warning event

**Objective:** Parked-agent route publishes routing.warning; route unchanged.

**Files:**
- Modify: internal/agent/tactical.go (selectAgent body)
- Test: internal/agent/agent_quota_aware_test.go

**Step 1: Failing test** — table-driven:
- parked agent + wired health + hint "code" → publishes routing.warning
  with agent_id "coder", step still routes to "coder".
- healthy agent + wired health → no routing.warning event.
- parked agent + NIL health → no event, no panic (legacy).
Use ts.publishEvent's bus (see how tactical_test.go captures published
events; if none exists, use a real bus.MessageBus from internal/bus with
a test subscription).

**Step 2: FAIL. Step 3: Implement. Step 4: PASS.**

### Task 3: Producer probe (report-only)

**Objective:** Determine whether internal/llm exposes parked/cooldown
state queryable per agent WITHOUT refactoring.

**Files:** none modified.

Grep internal/llm for park/cooldown query surfaces (e.g. functions like
IsParked, CooldownActive, endpoint state getters). Report findings:
- If a per-agent query exists: name it and the wiring line you suggest
  (components.go).
- If not: state "no producer; wiring deferred" — do NOT build a shim
  producer yourself.

## Self-Verification Checklist

- [ ] Interface + setter + nil guard implemented
- [ ] selectAgent route byte-identical when health nil
- [ ] routing.warning carries step_id, agent_id, reason
- [ ] No re-route logic added anywhere
- [ ] `go test ./internal/agent/ -run AgentQuotaAware -short` green

**DO NOT COMMIT.**

**Deviations from spec:** [none / list with rationale]

## Review Checklist (For Review Agent)

- [ ] Tasks implemented, tests passing
- [ ] Contracts match exactly
- [ ] No conflicts with sibling leaf edits in selectAgent region

## Notes

- publishEvent on TacticalScheduler is a nil-guarded helper
  (tactical.go:2528) — reuse it; do not publish directly on the bus.
- Keep the new field unexported; the setter is the only write path
  (setters_test.go house rule).
