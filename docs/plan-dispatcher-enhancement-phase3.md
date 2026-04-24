# Phase 3: Compound Request Support (Multi-Intent)

**Status:** Completed
**Priority:** Medium (requires Phase 2)
**Estimated Effort:** 1 sprint
**Completed:** 2026-04-24

---

## Summary

All implementation steps completed:

1. **MultiIntent struct created** - Holds multiple intents with IsCompound, CompoundType, Summary, and DetectCompound() method
2. **classifyMultiIntent() added** - Detects all intents using keyword and LLM classifiers
3. **ClassifyAll() method added** - KeywordClassifier returns all matches instead of just best
4. **ClassifyMulti() LLM method added** - LLM-based multi-intent detection
5. **routeCompound() implemented** - Creates parent task with metadata for compound requests
6. **PlanRequest updated** - Added IsCompound and CompoundType fields

All tests pass.

---

## Overview

Currently, every user input maps to exactly ONE intent and ONE task. Requests like "fix the login bug AND add a logout button" are misrouted—only the first matched intent wins. This phase adds detection of compound requests and routes them to the orchestrator for proper decomposition into multiple tasks.

**Current State (verified 2026-04-24):**
- `PlanRequest` at `strategic.go:17-22` has a single `Intent` string field
- `ClassifyAndRoute` returns a single `*Intent`
- `shouldDecompose` at `strategic.go:280-309` checks for complexity indicators but only for single-intent decomposition
- No multi-intent detection exists

---

## Problem Statement

### Current Behavior

```
User: "Fix the login bug and add a logout button"
1. ClassifyAndRoute runs
2. Keyword matcher finds "fix bug" → IntentDebug → debugger
3. "add a logout button" (coding task) is ignored
4. Single task created, assigned to debugger
5. Debugger agent must somehow handle both (it can't)
```

### Desired Behavior

```
User: "Fix the login bug and add a logout button"
1. ClassifyAndRoute detects compound intent
2. Creates TWO tasks:
   - Task 1: IntentDebug → debugger
   - Task 2: IntentCode → coder
3. Sends to orchestrator for coordinated execution
4. User receives acknowledgment with both task IDs
```

---

## Objectives

1. **Detect compound intents** - Identify requests with multiple independent goals
2. **Create MultiIntent structure** - Hold multiple `Intent` objects
3. **Update PlanRequest** - Support compound plans
4. **Orchestrator handling** - Decompose into separate tasks
5. **User acknowledgment** - Show all tasks created

---

## Implementation Steps

### Step 1: Create MultiIntent Structure

**File:** `internal/agent/dispatcher.go` (NEW, after `DispatchResult`)

```go
// MultiIntent represents multiple detected intents in a single request.
type MultiIntent struct {
    // Intents is the list of detected intents.
    Intents []*Intent `json:"intents"`

    // IsCompound is true if multiple independent intents were detected.
    IsCompound bool `json:"is_compound"`

    // CompoundType indicates how intents relate:
    // - "sequential": Must be done in order (depends on each other)
    // - "parallel": Can be done independently
    CompoundType string `json:"compound_type,omitempty"`

    // Summary is a combined description of all intents.
    Summary string `json:"summary"`
}

// DetectCompound analyzes intents and determines if they're compound.
func (m *MultiIntent) DetectCompound() bool {
    if len(m.Intents) < 2 {
        m.IsCompound = false
        return false
    }

    m.IsCompound = true

    // Simple heuristic: if any intent requires planning, it's sequential
    for _, intent := range m.Intents {
        if intent.RequiresPlanning {
            m.CompoundType = "sequential"
            return true
        }
    }

    m.CompoundType = "parallel"
    return true
}
```

### Step 2: Add Compound Detection to ClassifyAndRoute

**File:** `internal/agent/dispatcher.go`

**Location:** `ClassifyAndRoute()` function (lines 117-213)

**Changes:**

```go
func (d *Dispatcher) ClassifyAndRoute(ctx context.Context, input string, sessionID string) (*DispatchResult, error) {
    // ... existing skill check and memory search ...

    // NEW Step 2: Multi-intent classification
    multiIntent := d.classifyMultiIntent(ctx, input, memoryContext)

    if multiIntent.IsCompound {
        return d.routeCompound(ctx, multiIntent, input, sessionID)
    }

    // Step 3: Single intent routing (existing behavior)
    intent := multiIntent.Intents[0]
    // ... rest unchanged ...
}

// classifyMultiIntent runs classification to detect all potential intents.
func (d *Dispatcher) classifyMultiIntent(ctx context.Context, input string, context []memory.MemoryResult) *MultiIntent {
    var intents []*Intent

    // Run LLM classifier with multi-intent detection
    if d.llmClassifier != nil {
        llmIntents := d.llmClassifier.ClassifyMulti(ctx, input, context)
        intents = append(intents, llmIntents...)
    }

    // Run keyword classifier for all pattern matches (not just best)
    keywordIntents := d.keywordClassifier.ClassifyAll(ctx, input, context)
    intents = append(intents, keywordIntents...)

    // Deduplicate by intent type (keep highest confidence)
    intents = deduplicateIntents(intents)

    multi := &MultiIntent{
        Intents: intents,
        Summary: extractSummary(input),
    }
    multi.DetectCompound()

    return multi
}
```

### Step 3: Add Multi-Intent Methods to Classifiers

**File:** `internal/agent/dispatcher.go`

```go
// ClassifyAll returns ALL keyword matches (not just best).
func (c *KeywordClassifier) ClassifyAll(ctx context.Context, input string, context []memory.MemoryResult) []*Intent {
    lower := strings.ToLower(input)
    var intents []*Intent

    patterns := c.getPatterns() // existing patterns

    for _, p := range patterns {
        for _, kw := range p.keywords {
            if strings.Contains(lower, kw) {
                intents = append(intents, &Intent{
                    Type: p.intentType,
                    Confidence: p.confidence * 0.5, // Lower for multi-match
                    AgentType: p.agentType,
                    Summary: extractSummary(input),
                })
            }
        }
    }

    return deduplicateIntents(intents)
}

func deduplicateIntents(intents []*Intent) []*Intent {
    seen := make(map[string]*Intent)
    for _, intent := range intents {
        existing, ok := seen[intent.Type]
        if !ok || intent.Confidence > existing.Confidence {
            seen[intent.Type] = intent
        }
    }

    result := make([]*Intent, 0, len(seen))
    for _, intent := range seen {
        result = append(result, intent)
    }
    return result
}
```

**File:** `internal/agent/llm_classifier.go`

Add multi-intent classification:

```go
// ClassifyMulti detects multiple intents in a single input.
func (c *LLMClassifier) ClassifyMulti(ctx context.Context, input string, context []memory.MemoryResult) []*Intent {
    // Use a prompt that asks LLM to detect ALL intents
    prompt := fmt.Sprintf(`Analyze this user request and identify ALL distinct intents.

A request may contain multiple independent tasks joined by "and", "also", "then", etc.

For EACH detected intent, output:
- intent_type: one of [chat, code, debug, plan, analyze, search, git, schedule, review]
- confidence: 0.0-1.0
- summary: brief description

User input: %s

Output ONLY valid JSON array: [{"intent_type": "debug", "confidence": 0.8, "summary": "..."}]`, input)

    // ... call LLM, parse array of intents ...
}
```

### Step 4: Implement routeCompound

**File:** `internal/agent/dispatcher.go`

```go
// routeCompound handles compound intent routing.
func (d *Dispatcher) routeCompound(ctx context.Context, multi *MultiIntent, input string, sessionID string) (*DispatchResult, error) {
    d.logger.Info("Compound intent detected",
        "intents", len(multi.Intents),
        "type", multi.CompoundType,
    )

    // Create a parent task to track the compound request
    parentTask := d.createTask(ctx, multi.Summary, &Intent{
        Type: "compound",
        Summary: multi.Summary,
    }, sessionID)

    if parentTask == nil {
        return nil, fmt.Errorf("failed to create parent task")
    }

    // Record compound metadata
    parentTask.Metadata["compound_type"] = multi.CompoundType
    parentTask.Metadata["compound_intents"] = len(multi.Intents)
    d.taskStore.Update(parentTask)

    // The orchestrator will handle decomposition into child tasks
    // via the standard PlanRequest flow
    d.stats.recordCompoundDispatch(len(multi.Intents))

    return &DispatchResult{
        Task: parentTask,
        AgentID: "orchestrator",
        Intent: &Intent{
            Type: "compound",
            Summary: multi.Summary,
        },
    }, nil
}
```

### Step 5: Update StrategicPlanner for Compound

**File:** `internal/agent/strategic.go`

**Update `PlanRequest` to support compound:**

```go
type PlanRequest struct {
    TaskID    string `json:"task_id"`
    SessionID string `json:"session_id"`
    Input     string `json:"input"`
    Intent    string `json:"intent"`

    // Compound support (Phase 3)
    IsCompound   bool     `json:"is_compound,omitempty"`
    CompoundType string   `json:"compound_type,omitempty"`
}
```

**Update `parsePlanOutput` to handle compound:**

```go
func (sp *StrategicPlanner) parsePlanOutput(req PlanRequest, output string) ([]*task.TaskStep, error) {
    // ... existing parsing ...

    // For compound requests, create coordination steps
    if req.IsCompound {
        sp.logger.Info("Handling compound plan",
            "task_id", req.TaskID,
            "type", req.CompoundType,
        )
        // Create steps that coordinate multiple sub-tasks
    }

    // ... rest unchanged ...
}
```

### Step 6: Update Acknowledgment for Compound

**File:** `internal/agent/handler.go`

Update `formatAsyncTaskAck` (line 663):

```go
func (h *ChatHandler) formatAsyncTaskAck(result *DispatchResult) string {
    var sb strings.Builder
    sb.WriteString("## starting task\n\n")
    sb.WriteString(fmt.Sprintf("**task:** %s\n", strings.ToLower(result.Task.Name)))
    sb.WriteString(fmt.Sprintf("**id:** `%s`\n", result.Task.ID))

    // Check if compound
    if result.Intent.Type == "compound" {
        compoundType, _ := result.Task.Metadata["compound_type"].(string)
        intentCount, _ := result.Task.Metadata["compound_intents"].(int)
        sb.WriteString(fmt.Sprintf("**type:** compound request (%d intents, %s)\n\n", intentCount, compoundType))
    } else {
        sb.WriteString(fmt.Sprintf("**assigned to:** %s agent\n", result.AgentID))
        sb.WriteString("**status:** planning steps...\n\n")
    }

    sb.WriteString("you will receive updates as the task progresses.\n")
    return sb.String()
}
```

---

## Deliverables

| Item | Description |
|------|-------------|
| `MultiIntent` struct | Hold multiple intents |
| `classifyMultiIntent()` | Detect all intents in input |
| `routeCompound()` | Create parent task for coordination |
| Updated `PlanRequest` | Support compound fields |
| Compound ACK message | Shows multi-intent requests |

---

## Success Criteria

1. Compound requests like "A and B" are detected
2. Parent task tracks compound metadata
3. User sees compound acknowledgment
4. Orchestrator coordinates execution

---

## Dependencies

- **Phase 2**: Need `IntentType` enum for consistent compound detection

---

## Risks

| Risk | Mitigation |
|------|------------|
| Over-detection (false positives) | Set high confidence threshold (0.7+) for all detected intents |
| Task explosion | Limit to 5 intents per compound request |
| Orchestration complexity | Start with parallel-only, add sequential later |

---

## Next Phase

→ **Phase 4: Semantic/Embedding Matching**
