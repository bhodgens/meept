# Tactical Wiring - Implementation Leaf

## DISPATCH INSTRUCTION

> **Implementing agent:** Implement ALL tasks below using TDD. Do NOT
> commit. Do NOT use read_file on existing source files — explore with
> search_files or terminal cat. After writing a file, do NOT read it back
> to verify — write once and stop.

## Meta

- **Parent:** docs/plans/allotment/master.md
- **Scope:** Wire allotment math into the tactical scheduler: context-
  window provider dependency, executor-model ref plumbing, continuation-
  step creation at scheduling time.
- **Dependencies:** 01-allotment-math.md (COMPLETE)
- **Estimated Context:** ~80K
- **Concurrency Group:** B

## Goal

At ScheduleReadySteps time, the tactical scheduler asks a
ContextWindowProvider for the executor model's window, computes the
token allotment, and splits the phase's ready steps into allotment-
sized batches. Batches beyond the first are chained as continuation
steps. Without a provider (nil) or an unknown window (0), behavior is
byte-identical to today.

## Context

The tactical scheduler (internal/agent/tactical.go,
TacticalScheduler) schedules TaskSteps for executor agent loops.
PlanRequest (internal/agent/strategic.go) now carries
ExecutorModelRef; the dispatcher populates it from the intent's
resolved model. The resolver exposes ContextLimit per model
(internal/llm/resolver.go ResolveRef → ModelConfig.ContextLimit,
already fed by models.json5 and context discovery).

Key files:
- internal/agent/tactical.go — TacticalSchedulerConfig,
  NewTacticalScheduler, ScheduleReadySteps, scheduleStep
- internal/agent/allotment.go — the math (leaf 01, COMPLETE)
- internal/agent/strategic.go — PlanRequest (add ExecutorModelRef)
- internal/daemon/daemon.go — wiring site (mirror SetPlanManager)

## Interface Contracts (From Parent)

### What This Leaf Exposes

```go
// TacticalSchedulerConfig gains:
ContextWindowProvider func(agentID string) int

// TacticalScheduler gains:
func (ts *TacticalScheduler) SetContextWindowProvider(fn func(agentID string) int)

// PlanRequest (strategic.go) gains:
ExecutorModelRef string `json:"executor_model_ref,omitempty"`

// Continuation steps: Description prefixed by
// allotment.ContinuationDescription(desc, k, n); DependsOn chained to
// previous batch's last step ID.
```

### What This Leaf Consumes

```go
// From 01-allotment-math.md (COMPLETE):
func AllotmentTokens(contextLimit int, cfg AllotmentConfig) int
func SplitStepsByAllotment(steps []*task.TaskStep, allotmentTokens int,
    cfg AllotmentConfig) [][]*task.TaskStep
func ContinuationDescription(desc string, k, n int) string
```

## Tasks

### Task 1: Config + provider dependency

**Objective:** Add the provider and allotment config to the scheduler.

**Files:**
- Modify: `internal/agent/tactical.go` (TacticalSchedulerConfig struct,
  TacticalScheduler struct, NewTacticalScheduler)
- Test: `internal/agent/tactical_allotment_test.go`

**Step 1: Failing test**

```go
func TestTacticalScheduler_ContextWindowProvider_Nil(t *testing.T) {
    ts := NewTacticalScheduler(TacticalSchedulerConfig{ /* minimal */ })
    if got := ts.contextWindowFor("coder"); got != 0 {
        t.Errorf("contextWindowFor with nil provider = %d, want 0", got)
    }
}
```

**Step 2:** verify FAIL (no such method).

**Step 3: Implementation** — add `ContextWindowProvider func(agentID
string) int` to TacticalSchedulerConfig; store on the scheduler;
`contextWindowFor` returns 0 when nil. Also add an `AllotmentCfg
AllotmentConfig` field defaulting via DefaultAllotmentConfig() when
zero-valued in NewTacticalScheduler.

### Task 2: Allotment-aware batching in ScheduleReadySteps

**Objective:** when the context window is known (> 0), batch ready
steps via SplitStepsByAllotment; batches beyond the first become
continuation steps.

**Files:**
- Modify: `internal/agent/tactical.go` (ScheduleReadySteps)
- Test: `internal/agent/tactical_allotment_test.go`

**Step 1: Failing test**

Build a fake plan with 7 identical 512-token steps; provider returns
8192 (allotment 3072 → batches of 3/3/1 per leaf 01 tests). Assert:
- first batch schedules as today (3 steps, original descriptions)
- continuation steps exist for batch 2 and 3 with
  `strings.HasPrefix(desc, "[continuation 1/3] ")` / "[continuation
  2/3]", DependsOn chained to the previous batch's last scheduled ID

Use the existing test fakes for the scheduler's dependencies (mirror
the setup in tactical_test.go for ScheduleReadySteps).

**Step 2:** verify FAIL (no continuation steps created).

**Step 3: Implementation** — inside ScheduleReadySteps, after
collecting the ready steps for the phase (BEFORE the scheduling loop):

```go
allot := AllotmentTokens(ts.contextWindowFor(step.AgentID), ts.allotmentCfg)
if allot > 0 {
    batches := SplitStepsByAllotment(ready, allot, ts.allotmentCfg)
    if len(batches) > 1 {
        ready = flattenWithContinuations(batches)  // helper in this file
    }
}
// existing scheduling loop over `ready` unchanged
```

`flattenWithContinuations` rebuilds the ordered slice: batch 0 keeps
original descriptions; batch k (1-indexed) wraps each description with
ContinuationDescription(desc, k, len(batches)-1) and appends
DependsOn[previous step]. Preserve Sequence numbering (renumber
sequentially after insertion).

### Task 3: PlanRequest.ExecutorModelRef

**Objective:** carry the executor model ref so the provider can resolve
it.

**Files:**
- Modify: `internal/agent/strategic.go` (PlanRequest struct)
- Modify: `internal/agent/handler.go` (populate from dispatcher result)

**Step 1: Failing test** — dispatcher_quickplan_test.go style: assert
the published PlanRequest for a code intent carries a non-empty
ExecutorModelRef when the dispatcher knows the model (fake resolver
returns a model with ContextLimit > 0).

**Step 2:** verify FAIL.

**Step 3:** add the field to PlanRequest; in the handler where the
PlanRequest is built (grep `PlanRequest{` in handler.go), set
`ExecutorModelRef` from the dispatcher's resolved model reference for
the session/intent (mirror how SuggestedMode is carried). The value is
the model REF string (e.g. "local/lfm-8b-mlx-4bit") — resolution to a
number happens in the provider.

### Task 4: Daemon wiring

**Objective:** connect the resolver to the scheduler's provider.

**Files:**
- Modify: `internal/daemon/daemon.go` (near the SetPlanManager wiring
  site added by quickplan leaf 02)

**Step 1: Implementation** — where the tactical scheduler is
constructed/owned, add:

```go
tacticalScheduler.SetContextWindowProvider(func(agentID string) int {
    ref := /* executor model ref for agentID; fall back to default model ref */
    if mc := resolver.ResolveRef(ref); mc != nil {
        return mc.ContextLimit
    }
    return 0
})
```

The agentID→ref mapping: executor agents inherit the default model ref
unless they declare a model override — mirror whatever the registry
already does for model selection; when unknown, use
`resolver.DefaultModel().ModelID`'s ref shape. Keep the closure nil-
safe.

### Task 5: Tests for the daemon wiring

Table test: provider returns correct ContextLimit for the default model
ref; returns 0 for unknown agentIDs (nil-safety).

## Self-Verification Checklist

- [ ] All tasks implemented; tests passing
- [ ] `go build ./...` clean
- [ ] `go test ./internal/agent/ ./internal/plan/ ./internal/daemon/
      -short -count=1` green
- [ ] Nil provider + unknown window = byte-identical legacy behavior
      (continuation steps only when window known)
- [ ] No changes to direct-mode or non-plan chat paths
- [ ] gofmt/vet clean; no artifacts

**DO NOT COMMIT.** Orchestrator handles git operations.

**Deviations from spec:** document any (expected: minor — the daemon
wiring site may sit in components.go rather than daemon.go depending on
where tacticalScheduler is constructed; record which).

## Review Checklist (For Review Agent)

- [ ] Tasks 1-5 implemented
- [ ] Zero-config = legacy behavior (the critical regression check)
- [ ] Continuation steps are real scheduled TaskSteps with chained deps
- [ ] ExecutorModelRef populated and consumed
- [ ] Daemon wiring nil-safe, mirrors SetPlanManager pattern
- [ ] No scope creep

Output: APPROVED or specific gaps.

## Notes

- The 50k DefaultConversationTokenBudget is the per-TURN budget that
  killed the smoke — allotments sized from contextLimit (8k → 3072
  tokens) sit far below it, so small-model agents finish their batch
  before budget pressure. Larger-model agents get bigger batches
  automatically via the same formula.
- The scratch-config failure taught us:meept transport.http keys are
  addr/require_auth/rest (NOT listen_addr/auth/enabled-object). Do not
  reuse the throwaway smoke config as a schema reference.
