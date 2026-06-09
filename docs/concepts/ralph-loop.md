# Ralph Loop: Self-Referential Task Verification

## Overview

Ralph Loop is a **verification layer** that wraps around the standard plan → orchestrator → worker execution flow. It provides automatic replanning when tasks complete without sufficient evidence of success, enabling self-correcting task execution.

**Key insight:** Ralph Loop doesn't replace the standard execution model — it monitors outcomes and triggers replanning when verification fails.

## Problem

Standard agent execution follows a linear flow:
```
Task → Plan → Execute → Complete
```

This has a critical gap: if execution produces insufficient results or the agent incorrectly claims completion, the task stalls with no automatic recovery.

Ralph Loop adds verification:
```
Task → Plan → Execute → [Verify] → Complete
                          ↓
                    (insufficient evidence?)
                          ↓
                    Replan → Re-execute
```

## Architecture

### Components

| Component | Location | Purpose |
|-----------|----------|---------|
| `RalphLoop` | `internal/agent/ralph_loop.go` | Manages verification state and replanning |
| `CheckCompletion` | `internal/agent/ralph_loop.go` | Verifies task completion evidence |
| `TriggerReplan` | `internal/agent/ralph_loop.go` | Creates new planning step on failure |
| `handleJobCompleted` | `internal/agent/orchestrator.go` | Integration point in orchestrator |

### Configuration

```go
type RalphLoopConfig struct {
    Enabled          bool // Global enable/disable
    MaxIterations    int  // Maximum replan cycles (default: 3)
    EvidenceRequired bool // Require evidence for completion claims
}
```

## How It Works

### Step 1: Task Completion Event

When a job completes, the orchestrator receives a `queue.job.completed` bus event:

```go
func (o *Orchestrator) handleJobCompleted(ctx context.Context, msg *models.BusMessage) {
    // Extract task_id from job
    stepID, taskID := o.extractTaskIDFromJob(ctx, event.JobID)

    // Ralph Loop verification (if enabled for this task)
    if o.ralphLoop != nil && taskID != "" {
        isComplete, evidence, needsReplan := o.ralphLoop.CheckCompletion(ctx, taskID, event.Result)
        if needsReplan && !isComplete {
            o.ralphLoop.TriggerReplan(ctx, taskID, evidence)
            return // Skip normal completion
        }
    }

    // Normal completion processing...
}
```

### Step 2: Evidence Verification

`CheckCompletion` verifies the task result:

1. **Parse result** — Extract `success`, `result`, and `evidence` fields
2. **Check evidence** — Verify evidence array is non-empty (if `EvidenceRequired`)
3. **Validate evidence** — Check evidence mentions key terms from task description
4. **Check iteration count** — Enforce `MaxIterations` limit

```go
func (rl *RalphLoop) CheckCompletion(ctx context.Context, taskID string, result json.RawMessage) (bool, []string, bool) {
    // Parse result
    var resultData struct {
        Success  bool     `json:"success,omitempty"`
        Result   string   `json:"result,omitempty"`
        Evidence []string `json:"evidence,omitempty"`
    }

    // Check evidence requirement
    if rl.config.EvidenceRequired && len(resultData.Evidence) == 0 {
        return false, nil, true // Needs replan
    }

    // Validate evidence against task description
    if !rl.validateEvidence(task.Description, resultData.Evidence) {
        return false, resultData.Evidence, true
    }

    return true, resultData.Evidence, false // Complete
}
```

### Step 3: Replan Trigger

If verification fails, `TriggerReplan` publishes a replan request:

```go
func (rl *RalphLoop) TriggerReplan(ctx context.Context, taskID string, previousEvidence []string) error {
    // Increment iteration counter
    rl.iterations[taskID]++

    // Build replan context with previous attempt info
    replanContext := fmt.Sprintf("Previous attempt (iteration %d/%d) failed.\nEvidence: %v",
        iteration, rl.config.MaxIterations, previousEvidence)

    // Publish replan request to bus
    rl.bus.Publish("orchestrator.replan", &models.BusMessage{
        Payload: json.RawMessage(fmt.Sprintf(`{"task_id": "%s", "context": %q}`, taskID, replanContext)),
    })
}
```

## Opt-in Mechanism

Ralph Loop uses **layered opt-in** to avoid inefficient thrashing on simple tasks:

### Layer 1: Dispatcher (Intent-based)

The dispatcher classifies intents and assigns a verification policy:

```go
type RalphLoopPolicy int

const (
    RalphLoopDisabled RalphLoopPolicy = iota
    RalphLoopEnabled
    RalphLoopOptional // Let planner decide
)

var RalphLoopDefaults = map[IntentType]RalphLoopPolicy{
    // HIGH VERIFICATION - Always enable
    IntentCode:     RalphLoopEnabled,
    IntentDebug:    RalphLoopEnabled,
    IntentRefactor: RalphLoopEnabled,

    // NO VERIFICATION - Deterministic operations
    IntentChat:   RalphLoopDisabled,
    IntentRecall: RalphLoopDisabled,
    IntentGit:    RalphLoopDisabled,

    // CONTEXTUAL - Planner decides
    IntentResearch: RalphLoopOptional,
    IntentPlan:     RalphLoopOptional,
}
```

### Layer 2: Strategic Planner (Complexity-aware)

The planner can override dispatcher decisions based on task complexity:

```go
func (sp *StrategicPlanner) Decompose(ctx context.Context, req PlanRequest) (*plan.Plan, error) {
    complexity := sp.analyzeComplexity(req.Description)

    // Deferred to planner: decide based on complexity
    if req.Metadata.RalphLoopDeferred {
        req.Metadata.RalphLoopEnabled = complexity.IsHigh()
    }

    // Override: dispatcher said yes but task is trivial
    if req.Metadata.RalphLoopEnabled && complexity.IsTrivial() {
        req.Metadata.RalphLoopEnabled = false
    }

    // Override: dispatcher said no but task is complex
    if !req.Metadata.RalphLoopEnabled && complexity.IsHigh() {
        req.Metadata.RalphLoopEnabled = true
    }
}
```

### Layer 3: Orchestrator (Runtime heuristics)

The orchestrator applies final runtime checks:

```go
shouldVerify := task.Metadata.RalphLoopEnabled ||
                len(task.Steps) > 3 ||
                hasUncertaintyMarkers(task.Description)
```

### Decision Matrix

| Dispatcher | Planner | Result | Example |
|------------|---------|--------|---------|
| Enabled | Enabled | ✅ Ralph Loop runs | `IntentCode` with multi-step plan |
| Enabled | Disabled | ❌ No Ralph Loop | `IntentCode` but trivial one-liner |
| Disabled | Enabled | ✅ Ralph Loop runs | `IntentChat` but complex research |
| Disabled | Disabled | ❌ No Ralph Loop | `IntentRecall` simple query |
| Optional | Optional | ✅ If complex | `IntentResearch` with ambiguity |

## Evidence Validation

Evidence validation uses keyword matching:

```go
func (rl *RalphLoop) validateEvidence(taskDescription string, evidence []string) bool {
    // Extract key terms from task description
    keyTerms := extractKeyTerms(taskDescription)

    // Check if any evidence mentions at least one key term
    for _, ev := range evidence {
        matches := 0
        for _, term := range keyTerms {
            if strings.Contains(strings.ToLower(ev), strings.ToLower(term)) {
                matches++
            }
        }
        if matches > 0 {
            return true
        }
    }
    return false
}

func extractK Terms(desc string) []string {
    stopWords := map[string]bool{"the": true, "a": true, "and": true, ...}
    words := strings.Fields(strings.ToLower(desc))

    var terms []string
    for _, word := range words {
        word = strings.Trim(word, ".,!?;:\"'()[]{}")
        if len(word) > 3 && !stopWords[word] {
            terms = append(terms, word)
        }
    }
    return terms
}
```

**Example:**
- Task: "Refactor the database connection pooling to use connection limits"
- Key terms: `refactor`, `database`, `connection`, `pooling`, `limits`
- Valid evidence: "Implemented connection pooling with max limit of 10"
- Invalid evidence: "Done" (no key terms mentioned)

## Iteration Tracking

Each task tracks iteration count to prevent infinite loops:

```go
// Iterations: task_id -> count
iterations map[string]int
mu         sync.Mutex

func (rl *RalphLoop) GetIterationCount(taskID string) int {
    rl.mu.Lock()
    defer rl.mu.Unlock()
    return rl.iterations[taskID]
}

func (rl *RalphLoop) Reset(taskID string) {
    rl.mu.Lock()
    defer rl.mu.Unlock()
    delete(rl.iterations, taskID)
}
```

**Max iterations:** When reached, task is marked complete regardless of evidence.

## When to Use Ralph Loop

### Enable For (High Verification Value)
- **Code generation** — LLM output may have subtle bugs
- **Debugging** — Fixes should be verified against the original issue
- **Refactoring** — Structural changes need validation
- **Complex research** — Multi-step analysis benefits from review
- **Tasks with uncertainty markers** — "investigate", "explore", "try to"

### Disable For (Deterministic Operations)
- **Memory recall** — SQLite query with exact results
- **Git operations** — `git status`, `git log` produce deterministic output
- **File operations** — `file_read`, `file_write` either succeed or fail
- **Simple status queries** — "What's the weather?"
- **Chat/conversation** — No verification needed for responses

## Related Patterns

- **Deterministic Execution** — Standard flow without verification loop
- **Collaborative Planning** — Review/approval workflow (parallel concept)
- **Report Router** — Handles multi-agent handoff after completion

## Files

- `internal/agent/ralph_loop.go` — Core implementation
- `internal/agent/orchestrator.go` — Integration point
- `internal/agent/dispatcher.go` — Intent classification
- `internal/agent/strategic.go` — Complexity analysis
