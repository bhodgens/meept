# Phase 3: Compound Request Support (Multi-Intent)

**Status:** Not started
**Priority:** Medium (requires Phase 2)
**Estimated Effort:** 3-4 sprints

---

## Overview

Currently, every user input maps to exactly ONE intent and ONE task. Requests like "fix the login bug AND add a logout button" are misrouted—only the first matched intent wins. This phase adds detection of compound requests and routes them to the orchestrator for proper decomposition into multiple tasks.

---

## Problem Statement

### Current Behavior

User input: `"Fix the login bug and add a logout button"`

1. `ClassifyAndRoute` runs
2. Keyword matcher finds "fix bug" → `IntentDebug` → `debugger`
3. "add a logout button" (coding task) is ignored
4. Single task created, assigned to `debugger`
5. `debugger` agent must somehow handle both (it can't)

### Desired Behavior

User input: `"Fix the login bug and add a logout button"`

1. `ClassifyAndRoute` detects **compound intent**
2. Creates **two tasks**:
   - Task 1: `IntentDebug` → `debugger`
   - Task 2: `IntentCode` → `coder`
3. Sends to orchestrator for coordinated execution
4. User receives acknowledgment with both task IDs

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

**File:** `internal/agent/dispatcher.go` (or `protocol.go`)

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
    // - "mixed": Some parallel, some sequential
    CompoundType string `json:"compound_type,omitempty"`

    // Summary is a combined description of all intents.
    Summary string `json:"summary"`
}

// DetectCompound analyzes a list of intents and determines if they're compound.
func (m *MultiIntent) DetectCompound() bool {
    if len(m.Intents) < 2 {
        return false
    }

    // TODO: Enhance with LLM-based analysis
    // For now, 2+ intents = compound
    m.IsCompound = true

    // Determine compound type
    hasDependency := false
    for _, intent := range m.Intents {
        if intent.RequiresPlanning {
            hasDependency = true
            break
        }
    }

    if hasDependency {
        m.CompoundType = "sequential"
    } else {
        m.CompoundType = "parallel"
    }

    return true
}
```

### Step 2: Add Compound Detection to ClassifyAndRoute

**File:** `internal/agent/dispatcher.go`

**Location:** `ClassifyAndRoute()` function

**Current flow:**
```
input → single intent classification → route to ONE agent
```

**New flow:**
```
input → run ALL classifiers → collect ALL matches → detect compound → route accordingly
```

**Changes:**

```go
func (d *Dispatcher) ClassifyAndRoute(ctx context.Context, input string, sessionID string) (*DispatchResult, error) {
    // ... existing skill invocation check ...

    // Step 1: Search memory (unchanged)
    memoryContext := d.searchMemory(ctx, input)

    // Step 2: NEW - Run classification from multiple angles
    multiIntent := d.classifyMultiIntent(ctx, input, memoryContext)

    // Step 3: Check if compound
    if multiIntent.IsCompound {
        return d.routeCompound(ctx, multiIntent, input, sessionID)
    }

    // Step 4: Single intent routing (existing behavior)
    intent := multiIntent.Intents[0]
    intent.MemoryRefs = d.extractMemoryRefs(memoryContext)

    // ... rest unchanged ...
}

// classifyMultiIntent runs classification to detect all potential intents.
func (d *Dispatcher) classifyMultiIntent(ctx context.Context, input string, context []memory.MemoryResult) *MultiIntent {
    var intents []*Intent

    // Run capability matcher
    capResult := d.capabilityMatcher.MatchAll(input)  // NEW: MatchAll returns all matches
    for _, match := range capResult {
        intents = append(intents, &Intent{
            Type: match.IntentType,
            Confidence: match.Confidence,
            AgentType: match.AgentID,
            Summary: match.IntentType,
        })
    }

    // Run LLM classifier with "detect multiple intents" prompt
    if d.llmClassifier != nil {
        llmIntents := d.llmClassifier.ClassifyMulti(ctx, input, context)  // NEW method
        intents = append(intents, llmIntents...)
    }

    // Run keyword classifier for all pattern matches (not just best)
    keywordIntents := d.keywordClassifier.ClassifyAll(ctx, input, context)  // NEW method
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

### Step 3: Implement routeCompound

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

    // Create child tasks for each intent
    childTaskIDs := make([]string, 0, len(multi.Intents))
    for i, intent := range multi.Intents {
        childTask := d.createTask(ctx, intent.Summary, intent, sessionID)
        if childTask != nil {
            // Link to parent
            childTask.Metadata["parent_task_id"] = parentTask.ID
            childTask.Metadata["compound_index"] = i
            d.taskStore.Update(childTask)
            childTaskIDs = append(childTaskIDs, childTask.ID)
        }
    }

    // Record compound metadata
    parentTask.Metadata["child_tasks"] = childTaskIDs
    parentTask.Metadata["compound_type"] = multi.CompoundType
    d.taskStore.Update(parentTask)

    d.stats.recordCompoundDispatch(len(multi.Intents))

    return &DispatchResult{
        Task: parentTask,
        AgentID: "orchestrator",  // Special: orchestrator handles coordination
        Intent: &Intent{
            Type: "compound",
            Summary: multi.Summary,
        },
    }, nil
}
```

### Step 4: Update PlanRequest for Compound

**File:** `internal/agent/strategic.go`

```go
type PlanRequest struct {
    TaskID    string `json:"task_id"`
    SessionID string `json:"session_id"`
    Input     string `json:"input"`
    Intent    string `json:"intent"`

    // NEW: Compound request fields
    IsCompound     bool     `json:"is_compound,omitempty"`
    ChildTaskIDs   []string `json:"child_task_ids,omitempty"`
    CompoundType   string   `json:"compound_type,omitempty"`
}
```

**In StrategicPlanner.Plan():**

```go
func (p *StrategicPlanner) Plan(ctx context.Context, req PlanRequest) (*PlanResult, error) {
    // Handle compound requests
    if req.IsCompound {
        return p.planCompound(ctx, req)
    }

    // ... existing single-intent planning ...
}

// planCompound creates steps for each child task.
func (p *StrategicPlanner) planCompound(ctx context.Context, req PlanRequest) (*PlanResult, error) {
    p.logger.Info("Planning compound request",
        "parent_task", req.TaskID,
        "child_tasks", len(req.ChildTaskIDs),
    )

    // For compound requests, create a coordination step per child task
    steps := make([]*task.Step, 0, len(req.ChildTaskIDs))

    for i, childID := range req.ChildTaskIDs {
        childTask, err := p.taskStore.Get(childID)
        if err != nil {
            p.logger.Warn("Child task not found", "id", childID)
            continue
        }

        steps = append(steps, &task.Step{
            TaskID:      req.TaskID,
            Description: fmt.Sprintf("[%d/%d] %s", i+1, len(req.ChildTaskIDs), childTask.Name),
            State:       task.StepPending,
            Metadata: map[string]any{
                "child_task_id": childID,
                "original_intent": childTask.Metadata["intent_type"],
            },
        })
    }

    return &PlanResult{
        TaskID: req.TaskID,
        Steps:  steps,
        IsCompound: true,
    }, nil
}
```

### Step 5: Update formatAsyncTaskAck for Compound

**File:** `internal/agent/handler.go`

```go
func (h *ChatHandler) formatAsyncTaskAck(result *DispatchResult) string {
    var sb strings.Builder
    sb.WriteString("## starting task\n\n")
    sb.WriteString(fmt.Sprintf("**task:** %s\n", strings.ToLower(result.Task.Name)))
    sb.WriteString(fmt.Sprintf("**id:** `%s`\n", result.Task.ID))

    // Check if compound
    if result.Intent.Type == "compound" {
        childIDs, _ := result.Task.Metadata["child_tasks"].([]string)
        sb.WriteString(fmt.Sprintf("**type:** compound request (%d sub-tasks)\n\n", len(childIDs)))
        sb.WriteString("### sub-tasks:\n")
        for i, childID := range childIDs {
            childTask, _ := h.taskStore.Get(childID)
            if childTask != nil {
                sb.WriteString(fmt.Sprintf("%d. `%s` - %s\n", i+1, childID, strings.ToLower(childTask.Name)))
            }
        }
        sb.WriteString("\n")
    } else {
        sb.WriteString(fmt.Sprintf("**assigned to:** %s agent\n", result.AgentID))
        sb.WriteString(fmt.Sprintf("**status:** planning steps...\n\n"))
    }

    sb.WriteString("you will receive updates as the task progresses.\n")
    return sb.String()
}
```

### Step 6: Add Compound Detection Methods to Classifiers

**File:** `internal/agent/llm_classifier.go`

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

Output JSON array of all detected intents.`, input)

    // ... call LLM, parse array of intents ...

    var intents []*Intent
    // Parse response array
    for _, item := range response {
        intents = append(intents, &Intent{
            Type: item.Type,
            Confidence: item.Confidence,
            AgentType: item.AgentType,
            Summary: item.Summary,
        })
    }

    return intents
}
```

**File:** `internal/agent/dispatcher.go`

```go
// ClassifyAll returns ALL keyword matches (not just best).
func (c *KeywordClassifier) ClassifyAll(ctx context.Context, input string, context []memory.MemoryResult) []*Intent {
    lower := strings.ToLower(input)
    var intents []*Intent

    for _, p := range c.patterns {  // patterns from existing code
        for _, kw := range p.keywords {
            if strings.Contains(lower, kw) {
                intents = append(intents, &Intent{
                    Type: p.intentType,
                    Confidence: p.confidence * 0.5,  // Lower confidence for keyword matches
                    AgentType: p.agentType,
                    Summary: extractSummary(input),
                })
            }
        }
    }

    return deduplicateIntents(intents)
}
```

### Step 7: Add Compound Stats Tracking

**File:** `internal/agent/dispatcher.go`

```go
// In DispatcherStats:
CompoundDispatches int `json:"compound_dispatches"`
AvgIntentsPerCompound float64 `json:"avg_intents_per_compound"`

// New method:
func (d *Dispatcher) stats.recordCompoundDispatch(intentCount int) {
    d.stats.mu.Lock()
    defer d.stats.mu.Unlock()
    d.stats.CompoundDispatches++

    // Update running average
    total := d.stats.avg_intents_per_compound * float64(d.stats.CompoundDispatches-1)
    d.stats.AvgIntentsPerCompound = (total + float64(intentCount)) / float64(d.stats.CompoundDispatches)
}
```

---

## Deliverables

| Item | Description |
|------|-------------|
| `MultiIntent` struct | Hold multiple intents |
| `classifyMultiIntent()` | Detect all intents in input |
| `routeCompound()` | Create parent + child tasks |
| Updated `PlanRequest` | Support compound fields |
| `planCompound()` | Orchestrator handling |
| Updated ACK message | Show all sub-tasks |

---

## Success Criteria

1. ✅ Compound requests like "A and B" create 2+ tasks
2. ✅ Parent task tracks all children
3. ✅ User sees all sub-tasks in acknowledgment
4. ✅ Each child task is assigned to correct agent
5. ✅ Stats track compound dispatch rate

---

## Testing

### Unit Tests

```go
func TestDetectCompound(t *testing.T) {
    multi := &MultiIntent{
        Intents: []*Intent{
            {Type: "debug", AgentType: "debugger"},
            {Type: "code", AgentType: "coder"},
        },
    }
    assert.True(t, multi.DetectCompound())
    assert.Equal(t, "parallel", multi.CompoundType)
}

func TestRouteCompound(t *testing.T) {
    d := setupDispatcher()
    result, err := d.ClassifyAndRoute(ctx, "Fix the bug and add a test", "session1")
    assert.NoError(t, err)
    assert.Equal(t, "compound", result.Intent.Type)
    assert.NotNil(t, result.Task.Metadata["child_tasks"])
}
```

### Integration Tests

```bash
# Test compound detection
./bin/meept chat "Fix the login bug and add a logout button"
# Expected: Two tasks created, acknowledgment shows both

./bin/meept chat "Research OAuth options and then implement it"
# Expected: Sequential compound (research first, then code)
```

---

## Dependencies

- **Phase 2**: Need `IntentType` enum for consistent compound detection

---

## Risks

| Risk | Mitigation |
|------|------------|
| Over-detection (false positives) | Set high confidence threshold (0.7+) for all detected intents |
| Task explosion (too many children) | Limit to 5 child tasks, merge rest into parent |
| Orchestration complexity | Start with parallel-only, add sequential later |

---

## Next Phase

→ **Phase 4: Semantic/Embedding Matching**

With compound detection working, add semantic matching to catch intents that keyword/LLM classifiers miss.
