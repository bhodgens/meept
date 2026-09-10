# Capability-Aware Allotment - Implementation Orchestrator

> **For the executing agent:** You are the orchestrator for this tree node.
> Your job: (1) dispatch implementation agents, (2) review their work,
> (3) re-dispatch if incomplete, (4) track completion.
> Do NOT implement code yourself. All implementation happens in leaf agents.

## Meta

- **Role:** Root
- **Parent:** none (root)
- **Children:** 2 leaf documents
- **Scope:** Make the tactical scheduler size agent work allotments to the
  executing model's context window, so multi-step plans chunk into
  agent-sized pieces instead of forcing compaction.

## Goal

QuickPlan runs long plans through executor agents whose context windows
vary (8k–128k+). Today the tactical scheduler caps phases by COUNT
(`MaxStepsPerPhase`, default 8) and nothing reads the executor model's
context window. A coder on an 8k model gets the same 8-step allotment as
one on a 128k model — the small-model agent compacts mid-task or dies at
"conversation token budget exhausted" (observed: quickplan e2e smoke 1,
50k budget, coder died reviewing real files).

This tree adds **capability-aware allotment**: the scheduler learns the
executor model's context window (from the resolver's ModelConfig, which
context-discovery already populates), converts it into a per-step token
allotment, and splits oversized steps into consecutive sub-allotments.
Count caps remain as a backstop.

## Architecture

1. **Context window source:** `llm.Resolver.ResolveGeneration` returns
   `ModelConfig.ContextLimit` (already populated from models.json5
   `context_limit` and context-discovery overrides). The tactical
   scheduler gains a `ContextWindowProvider` function dependency so it
   can ask "how many tokens of work fits one agent turn" per agent type.
2. **Allotment math (pure function, leaf 01):**
   `allotmentTokens = (contextLimit - reserve) * ratio` where
   reserve = prompt/system overhead (default 4096) and ratio = usable
   work fraction (default 0.75, config-overridable). Steps are estimated
   by description length (`~4 chars/token` heuristic, config-floor and
   config-cap). `SplitStepsByAllotment` groups steps into allotment
   batches; batches beyond the first become continuation steps with
   explicit "continue from" framing.
3. **Wiring (leaf 02):** tactical scheduler consults the provider at
   `ScheduleReadySteps` time; the strategic planner passes the resolved
   model ref through `PlanRequest` so the scheduler knows which model
   executes. Config: `[agent] allotment` block (ratio, reserve, min/max
   batch), all with defaults so absent config = current behavior.

## Interface Contracts

### Contract 1: Allotment math (pure)

```go
// internal/agent/allotment.go
package agent

// AllotmentConfig configures capability-aware step chunking.
type AllotmentConfig struct {
    // UsableRatio is the fraction of the model context window available
    // for work after prompt/system overhead. Default 0.75.
    UsableRatio float64
    // ReserveTokens is the absolute token reserve subtracted before the
    // ratio (system prompt, tool definitions). Default 4096.
    ReserveTokens int
    // CharsPerToken is the text-to-token heuristic. Default 4.
    CharsPerToken float64
    // MinStepTokens floors tiny estimates so a one-line step still
    // consumes an allotment slot. Default 512.
    MinStepTokens int
    // MaxBatchSteps caps steps per allotment regardless of token fit
    // (count backstop). 0 = no count cap.
    MaxBatchSteps int
}

func DefaultAllotmentConfig() AllotmentConfig

// EstimateStepTokens estimates the token cost of one step description.
func EstimateStepTokens(desc string, cfg AllotmentConfig) int

// AllotmentTokens converts a model context window into a work budget.
// Returns 0 when contextLimit <= 0 (unknown window: caller falls back
// to count-based chunking).
func AllotmentTokens(contextLimit int, cfg AllotmentConfig) int

// SplitStepsByAllotment partitions ordered steps into batches whose
// estimated token sums fit allotmentTokens. Returns one batch per step
// when allotmentTokens <= 0 (caller falls back to count-based chunking).
func SplitStepsByAllotment(steps []*task.TaskStep, allotmentTokens int,
    cfg AllotmentConfig) [][]*task.TaskStep

// Owner: 01-allotment-math.md
// Consumers: 02-tactical-wiring.md
```

### Contract 2: Context window provider + scheduler wiring

```go
// internal/agent/tactical.go (TacticalSchedulerConfig + TacticalScheduler)
// ContextWindowProvider func(agentID string) int
//   Returns the executor model's context window for agentID; 0 = unknown.
//   Wired by daemon from llm.Resolver (ResolveGeneration → ContextLimit).
// (ts *TacticalScheduler) contextWindowFor(agentID string) int
//   nil-provider safe; returns 0.

// PlanRequest gains (internal/agent/strategic.go):
//   ExecutorModelRef string `json:"executor_model_ref,omitempty"`
//   Set from the dispatcher's resolved model for the intent; the plan
//   carries it so ScheduleReadySteps can ask the provider.

// Owner: 02-tactical-wiring.md
// Consumers: master integration test
```

### Contract 3: Continuation steps

```go
// When a phase's steps exceed one allotment, batch 2..N become
// continuation steps appended after the originals with Description
// prefixed "[continuation k/N] " and DependsOn chained to the previous
// batch's last step. Continuation steps are REAL TaskSteps (scheduled,
// tracked, validated) — not markers.
// Owner: 01-allotment-math.md (SplitStepsByAllotment), 02 (wiring)
```

## Child Document Index

| # | Document | Type | Dependencies | Est. Context | Concurrency |
|---|----------|------|-------------|-------------|-------------|
| 01 | 01-allotment-math.md | leaf | none | ~40K | A |
| 02 | 02-tactical-wiring.md | leaf | 01 | ~80K | B |

**Concurrency groups:** 01 alone (02 consumes its functions). 02 second.

## Dispatch Protocol

For each concurrency group, in dependency order:

### Phase 1: Dispatch Concurrency Group A

1. **Read** 01-allotment-math.md and dispatch via `delegate_task`:
   - Goal: "Implement all tasks from 01-allotment-math.md"
   - Context: full leaf text + Contract 1 + coding conventions +
     internal/task/step.go TaskStep struct INLINED
   - Include: "Do NOT commit. Do NOT run git add. Write code, run tests,
     report results only."
   - Include: "Do NOT use read_file on existing source files — explore
     with search_files or terminal cat instead."

### Phase 2: Review and Commit Child 01

1. **Orchestrator reviews in-session:** changed files vs leaf spec,
   `go build ./internal/agent/...`, `go test ./internal/agent/
   -run Allotment -count=1`, table-driven edge cases (empty desc, huge
   desc, zero context, tiny context).
2. **Commit on pass:** `git add internal/agent/allotment.go
   internal/agent/allotment_test.go && git commit -m
   "feat(quickplan): allotment math (leaf 01)"`.

### Phase 3: Dispatch Concurrency Group B

1. **Read** 02-tactical-wiring.md and dispatch (context: leaf text +
   Contracts 1-2 + current TacticalSchedulerConfig/NewTacticalScheduler/
   ScheduleReadySteps regions INLINED + PlanRequest struct +
   daemon wiring site for SetPlanManager as the wiring precedent).
2. Review in-session on return; run the full agent suite.
3. Commit on pass: `feat(quickplan): tactical allotment wiring (leaf 02)`.

### Phase 4: Integration Review

1. `go build ./...` clean
2. `go test ./internal/agent/ ./internal/plan/ ./internal/daemon/
   -short -count=1` — all pass
3. `make mutexio && make predid` — clean
4. Scratch-daemon smoke rerun: quickplan smoke 1 with an 8k-context
   model ref → plan splits into multiple continuation steps; with the
   128k ref → single batch (no split). Compare step counts in logs.

## Review Checklist

- [ ] All tasks from each leaf implemented
- [ ] Contracts 1-3 satisfied (function signatures exact)
- [ ] Zero-config behavior identical to current (defaults preserve
      count-based chunking when no provider/context available)
- [ ] No budget/compaction behavior changed for existing direct-mode
      loops (allotment only affects plan-phase step batching)
- [ ] Tests: allotment math table-driven incl. edge cases; wiring tests
      with fake provider
- [ ] No debug artifacts, no TODOs, no corruption
- [ ] gofmt/vet/mutexio/predid clean

## Coding Conventions

- Go 1.22+, stdlib + existing deps only
- Exported PascalCase; unexported camelCase
- Errors wrapped with %w; return early; no panic
- Table-driven tests alongside code
- gofmt before reporting
- Every config default documented in the struct comment
- Regex/compiled vars at package level where applicable

## Completion Tracking Table

| Child | Status | Iterations | Review Notes |
|-------|--------|------------|-------------|
| 01-allotment-math.md | PENDING | 0 | |
| 02-tactical-wiring.md | PENDING | 0 | |

Status values: PENDING | IN_PROGRESS | IMPLEMENTED | REVIEWED | COMPLETE | BLOCKED

## Integration Test Plan

1. `go build ./...` zero errors
2. `go test ./internal/agent/ -short -count=1` all pass (new allotment
   + wiring tests included)
3. `make mutexio && make predid` clean
4. Scratch smoke (quickplan-mode/E2E-SMOKE-REPORT.md harness): 8k model
   → multi-batch plan visible in daemon log; 128k model → single batch
5. No behavior change for plain `direct` mode or non-plan chat

## Notes

- Origin: user direction ("orchestrator chunks work to fit each agent's
  context; distribute across multiple coder agents to avoid compaction")
  + E2E smoke 1 failure (50k budget exhaustion mid-review). Investigation
  confirmed steps are count-capped only; nothing reads context_length.
- The resolver ALREADY tracks per-model ContextLimit (models.json5
  context_limit + llm.ContextDiscovery overrides via SetContextLimits).
  This tree only consumes it at scheduling time.
- Do NOT touch the classifier taxonomy (quickplan intent, cue guard) —
  frozen at iter-19/20.
- ASCII only; pre-commit hooks run the full gate battery.
