# Successor Hints - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit — the orchestrator
> handles all git operations after review. Do NOT use read_file on existing
> source files — explore with search_files or terminal cat. After writing
> a file, do NOT read it back to verify — write once and stop.

## Meta

- **Parent:** ../master.md
- **Scope:** Advisory `suggested_next_hint` field flows from a finished step's report onto the persisted TaskStep, with a telemetry event; nothing reads it for routing yet.
- **Dependencies:** none
- **Estimated Context:** 40K
- **Concurrency Group:** A

## Goal

The only way an executing agent can suggest who works next is the heavy
`request_handoff` tool (rate-limited, amendment-path). This leaf adds a
lightweight advisory channel: the step-job envelope learns
`suggested_next_hint`; OnJobCompleted copies it onto the step and persists
it, and publishes a `routing.telemetry` event when non-empty so usage is
measurable from day one. Nothing consumes it for routing in this tree.

## Context

- TaskStep: internal/task/step.go (struct TaskStep).
- Step-job payload: internal/agent/tactical.go stepJobPayloadFromStep
  (step → payload) and the retry/rebuild path that reads payload → step.
  Grep for `StepJobPayload` usages; the rebuild direction matters.
- The daemon's step-job result envelope is built in
  internal/daemon/components.go (buildStepResult region, ~:8266 area —
  grep `buildStepResult`). The finished turn's final report JSON is
  available there; extract `suggested_next_hint` if the report carries it.
- OnJobCompleted: internal/agent/tactical.go:1205. Evidence extraction
  happens at :1275-1335; add your copy-persist AFTER that block.

Key files:
- internal/task/step.go — one field
- internal/agent/tactical.go — payload round-trip + OnJobCompleted copy
- internal/daemon/components.go — envelope extraction

## Interface Contracts (From Parent)

### What This Leaf Exposes

```go
// internal/task/step.go — TaskStep gains:
SuggestedNextHint string `json:"suggested_next_hint,omitempty"`

// internal/agent/tactical.go — StepJobPayload gains the same json tag;
// stepJobPayloadFromStep copies step.SuggestedNextHint → payload; the
// payload→step rebuild copies it back.

// internal/daemon/components.go — buildStepResult envelope struct gains:
SuggestedNextHint string `json:"suggested_next_hint,omitempty"`
// populated from the turn's final report JSON when that JSON (or its
// metadata block) contains a non-empty suggested_next_hint string ≤64 chars
// after TrimSpace; longer values are truncated to 64 (defensive: model output).

// internal/agent/tactical.go OnJobCompleted — after evidence extraction:
//   if execResult.SuggestedNextHint != "" {
//       step.SuggestedNextHint = execResult.SuggestedNextHint
//       persist via ts.stepStore.Update(step) (log-only on error, matching
//       neighboring evidence-persist error handling)
//       ts.publishEvent("routing.telemetry", map[string]any{
//           "step_id": step.ID, "task_id": step.TaskID,
//           "suggested_next_hint": step.SuggestedNextHint,
//       })
//   }
```

### What This Leaf Consumes

Nothing from siblings. tactical.go region ownership: you own
stepJobPayloadFromStep + the OnJobCompleted evidence-tail region; the
telemetry leaf owns selectAgent + HandleHandoff; the quota leaf owns
selectAgent's hit-branch tail. Disjoint — read current file state before
editing (a sibling may have landed first).

## Tasks

### Task 1: TaskStep field + persistence round-trip

**Objective:** Field exists, round-trips through the store.

**Files:**
- Modify: internal/task/step.go
- Test: internal/task/step_suggested_hint_test.go (NEW)

**Step 1: Failing test** — create step, set SuggestedNextHint, Update,
GetByID, assert field survives (follow existing TaskStep store tests for
construction).

**Step 2: FAIL. Step 3: Implement. Step 4: PASS.**

### Task 2: Payload round-trip in tactical scheduler

**Objective:** step→payload and payload→step both carry the field.

**Files:**
- Modify: internal/agent/tactical.go (StepJobPayload + stepJobPayloadFromStep + rebuild path)
- Test: internal/agent/tactical_suggested_hint_test.go (NEW)

**Step 1: Failing test** — table-driven:
- step with hint → payload carries hint.
- payload with hint → rebuilt step carries hint.
- empty on both sides stays empty (no `"suggested_next_hint":""` in JSON —
  omitempty).

**Step 2: FAIL. Step 3: Implement. Step 4: PASS.**

### Task 3: OnJobCompleted adoption + telemetry event

**Objective:** A completion envelope with the hint persists it and emits routing.telemetry.

**Files:**
- Modify: internal/agent/tactical.go (OnJobCompleted evidence tail)
- Test: internal/agent/tactical_suggested_hint_test.go

**Step 1: Failing test** — craft a step + completed-job result JSON
containing suggested_next_hint; call ts.OnJobCompleted; assert
step.SuggestedNextHint persisted and one routing.telemetry event with
step_id/task_id/hint published. Second case: envelope without the field →
no event, field stays empty.

**Step 2: FAIL. Step 3: Implement. Step 4: PASS.**

### Task 4: Daemon envelope extraction

**Objective:** buildStepResult picks the hint out of the turn report.

**Files:**
- Modify: internal/daemon/components.go (buildStepResult region)
- Test: internal/daemon/components_suggested_hint_test.go (NEW)

**Step 1: Failing test** — feed the function a turn result whose report
JSON carries suggested_next_hint "debugger"; assert the envelope field
equals "debugger". A 100-char value truncates to 64. Absent → empty.
Grep buildStepResult's actual input shape first — the test must match the
real signature; do not invent inputs.

**Step 2: FAIL. Step 3: Implement. Step 4: PASS.**

## Self-Verification Checklist

- [ ] Field on TaskStep + payload + envelope, omitempty everywhere
- [ ] OnJobCompleted adoption is log-only-on-error and publishes one event
- [ ] 64-char truncation defensive guard in the daemon envelope
- [ ] No routing decision anywhere reads the field (advisory only)
- [ ] `go test ./internal/task/ -short` and targeted agent/daemon runs green

**DO NOT COMMIT.**

**Deviations from spec:** [none / list with rationale]

## Review Checklist (For Review Agent)

- [ ] Tasks implemented, tests passing
- [ ] Contracts match exactly
- [ ] Advisory semantics preserved (no consumer)

## Notes

- The daemon envelope extraction depends on the real buildStepResult
  input shape — spend your first exploration minutes there before writing
  the test.
- If the final report JSON shape makes reliable extraction ambiguous,
  extract ONLY from a top-level envelope key `suggested_next_hint` (not
  from nested prose) and note the limitation in your report.
