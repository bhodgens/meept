# Phase 2: Unified Intent Taxonomy + Validation

**Status:** Not started
**Priority:** High (requires Phase 1)
**Estimated Effort:** 2-3 sprints

---

## Overview

Currently, intent types are defined in at least 3 different places with overlapping but inconsistent values. This phase consolidates all intent definitions into a single source of truth and adds validation to ensure routing decisions are correct.

---

## Current State Analysis

### Intent Type Definitions (Scattered)

| Location | Line | Intent Types Defined |
|----------|------|---------------------|
| `dispatcher.go` keyword patterns | ~493-532 | `platform`, `report`, `recall`, `debug`, `code`, `review`, `git`, `schedule`, `plan`, `analyze`, `search`, `chat` |
| `llm_classifier.go` agentMapping | TBD | ~10-12 intents (needs verification) |
| `capability_matcher.go` getDefaultIntentType | ~251-261 | Agent ID fallbacks |
| `dispatcher.go` shouldCreateTask | ~302-315 | Hardcoded switch cases |

### Problems

1. **Inconsistent naming**: Keyword classifier uses `analyze`, LLM might use `research`
2. **Agent ID coupling**: Some intents default to agent ID (e.g., `coder`) instead of semantic intent (`code`)
3. **Drift risk**: Adding a new intent requires updating 3+ files
4. **No validation**: No way to verify if routing was correct

---

## Objectives

1. **Create `IntentType` enum** - Single source of truth for all intent types
2. **Consolidate mappings** - All classifiers reference the enum
3. **Add validation hook** - Post-completion agreement check
4. **Expose validation tool** - `platform_validate_routing` for agents

---

## Implementation Steps

### Step 1: Create IntentType Enum

**File:** `internal/agent/intent.go` (NEW)

```go
package agent

// IntentType represents a classified user intent.
// All dispatch routing MUST use these constants.
type IntentType string

const (
    // IntentUnknown is the default for unclassified requests.
    IntentUnknown IntentType = "unknown"

    // Conversational intents
    IntentChat     IntentType = "chat"      // General conversation
    IntentReport   IntentType = "report"    // Status/progress reports
    IntentRecall   IntentType = "recall"    // Memory recall
    IntentPlatform IntentType = "platform"  // Platform introspection

    // Execution intents (require task tracking)
    IntentCode     IntentType = "code"      // Write/modify code
    IntentDebug    IntentType = "debug"     // Fix bugs, diagnose issues
    IntentReview   IntentType = "review"    // Code review, PR check
    IntentPlan     IntentType = "plan"      // Decompose, architect, design
    IntentGit      IntentType = "git"       // Git operations
    IntentSchedule IntentType = "schedule"  // Create reminders/scheduling

    // Analysis intents
    IntentAnalyze  IntentType = "analyze"   // Research, summarize
    IntentSearch   IntentType = "search"    // Look up information
    IntentSkill    IntentType = "skill"     // Explicit skill invocation

    // Compound multi-intent (Phase 4)
    IntentCompound IntentType = "compound"  // Multiple intents detected
)

// IntentCategory groups intents by routing behavior.
type IntentCategory string

const (
    CategoryInline   IntentCategory = "inline"   // Handle directly, no task
    CategoryTrackable IntentCategory = "trackable" // Create task, async OK
    CategoryDefer    IntentCategory = "defer"    // Always async to orchestrator
)

// IntentDefinition describes an intent type.
type IntentDefinition struct {
    Type     IntentType     `json:"type"`
    Category IntentCategory `json:"category"`
    // DefaultAgent is the preferred agent for this intent.
    DefaultAgent string `json:"default_agent"`
    // Keywords are common trigger phrases (for documentation/logging).
    Keywords []string `json:"keywords,omitempty"`
    // RequiresPlanning indicates if orchestrator planning is beneficial.
    RequiresPlanning bool `json:"requires_planning"`
}

// IntentRegistry is the single source of truth for intent definitions.
var IntentRegistry = map[IntentType]IntentDefinition{
    IntentChat: {
        Type: IntentChat,
        Category: CategoryInline,
        DefaultAgent: "chat",
        Keywords: []string{"hello", "hi", "thanks", "help"},
    },
    IntentReport: {
        Type: IntentReport,
        Category: CategoryInline,
        DefaultAgent: "chat",
        Keywords: []string{"report", "what did you", "summary", "progress"},
    },
    IntentRecall: {
        Type: IntentRecall,
        Category: CategoryInline,
        DefaultAgent: "chat",
        Keywords: []string{"remember", "recall", "last time"},
    },
    IntentPlatform: {
        Type: IntentPlatform,
        Category: CategoryInline,
        DefaultAgent: "chat",
        Keywords: []string{"capabilities", "what can you", "platform status"},
    },
    IntentCode: {
        Type: IntentCode,
        Category: CategoryDefer,
        DefaultAgent: "coder",
        Keywords: []string{"implement", "create", "add feature", "refactor"},
        RequiresPlanning: true,
    },
    IntentDebug: {
        Type: IntentDebug,
        Category: CategoryDefer,
        DefaultAgent: "debugger",
        Keywords: []string{"fix bug", "error", "broken", "not working"},
        RequiresPlanning: false,
    },
    IntentReview: {
        Type: IntentReview,
        Category: CategoryDefer,
        DefaultAgent: "code-reviewer",
        Keywords: []string{"review pr", "check code", "code review"},
        RequiresPlanning: false,
    },
    IntentGit: {
        Type: IntentGit,
        Category: CategoryDefer,
        DefaultAgent: "committer",
        Keywords: []string{"commit", "push", "pull", "merge", "branch"},
        RequiresPlanning: false,
    },
    IntentSchedule: {
        Type: IntentSchedule,
        Category: CategoryDefer,
        DefaultAgent: "scheduler",
        Keywords: []string{"remind", "schedule", "alarm", "at "},
        RequiresPlanning: false,
    },
    IntentPlan: {
        Type: IntentPlan,
        Category: CategoryDefer,
        DefaultAgent: "planner",
        Keywords: []string{"plan", "design", "architect", "how should i"},
        RequiresPlanning: true,
    },
    IntentAnalyze: {
        Type: IntentAnalyze,
        Category: CategoryInline,
        DefaultAgent: "analyst",
        Keywords: []string{"research", "analyze", "explain", "summarize"},
        RequiresPlanning: false,
    },
    IntentSearch: {
        Type: IntentSearch,
        Category: CategoryInline,
        DefaultAgent: "analyst",
        Keywords: []string{"search", "find", "look up"},
        RequiresPlanning: false,
    },
    IntentSkill: {
        Type: IntentSkill,
        Category: CategoryInline,
        DefaultAgent: "skill",
        Keywords: []string{"/"},
    },
}

// GetIntentDefinition returns the definition for an intent type.
func GetIntentDefinition(t IntentType) (IntentDefinition, bool) {
    def, ok := IntentRegistry[t]
    return def, ok
}

// IntentCategory returns the category for an intent type.
func (t IntentType) Category() IntentCategory {
    if def, ok := IntentRegistry[t]; ok {
        return def.Category
    }
    return CategoryInline
}

// DefaultAgent returns the default agent for an intent type.
func (t IntentType) DefaultAgent() string {
    if def, ok := IntentRegistry[t]; ok {
        return def.DefaultAgent
    }
    return "chat"
}

// RequiresPlanning returns true if the intent benefits from orchestration.
func (t IntentType) RequiresPlanning() bool {
    if def, ok := IntentRegistry[t]; ok {
        return def.RequiresPlanning
    }
    return false
}

// IsValidIntentType checks if a string is a valid intent type.
func IsValidIntentType(s string) bool {
    for t := range IntentRegistry {
        if string(t) == s {
            return true
        }
    }
    return false
}
```

### Step 2: Update Keyword Classifier

**File:** `internal/agent/dispatcher.go`

**Location:** `KeywordClassifier.Classify()` (lines 488-562)

**Changes:**

```go
// Replace the patterns array to use IntentType constants
patterns := []struct {
    keywords   []string
    intentType agent.IntentType  // WAS: string
    agentType  string
    confidence float64
    planning   bool
}{
    // Platform introspection
    {[]string{"what can you do", "what are your capabilities"},
     agent.IntentPlatform, "chat", 0.9, false},

    // Report requests
    {[]string{"give me a report", "what did you do"},
     agent.IntentReport, "chat", 0.9, false},

    // Recall
    {[]string{"remember when", "recall"},
     agent.IntentRecall, "chat", 0.85, false},

    // Debug
    {[]string{"fix bug", "debug", "error"},
     agent.IntentDebug, "debugger", 0.8, false},

    // Code
    {[]string{"write code", "implement", "create function"},
     agent.IntentCode, "coder", 0.8, false},

    // ... etc for all patterns
}

// In the return statement:
return &Intent{
    Type: string(p.intentType),  // Convert to string for backward compat
    // ...
}, nil
```

### Step 3: Update LLM Classifier

**File:** `internal/agent/llm_classifier.go`

**Changes:**

1. Update `agentMapping` to reference `IntentType` constants
2. Update `intentThresholds` map keys to use `IntentType`
3. Update validation in `Classify()` to check `IsValidIntentType()`

```go
// Define mapping from LLM output to IntentType
var llmToIntent = map[string]agent.IntentType{
    "code": agent.IntentCode,
    "coding": agent.IntentCode,
    "debug": agent.IntentDebug,
    "debugging": agent.IntentDebug,
    "chat": agent.IntentChat,
    "conversation": agent.IntentChat,
    "research": agent.IntentAnalyze,
    "analyze": agent.IntentAnalyze,
    "plan": agent.IntentPlan,
    "planning": agent.IntentPlan,
    "schedule": agent.IntentSchedule,
    "git": agent.IntentGit,
    "review": agent.IntentReview,
}

// In Classify(), normalize LLM output:
rawType := llmOutput.Type
normalized, ok := llmToIntent[rawType]
if !ok {
    d.logger.Warn("Unknown LLM intent", "type", rawType)
    normalized = agent.IntentChat
}

return &Intent{
    Type: string(normalized),
    // ...
}, nil
```

### Step 4: Update Capability Matcher

**File:** `internal/agent/capability_matcher.go`

**Changes:**

```go
// In getDefaultIntentType(), map agents to IntentType
func (m *CapabilityMatcher) getDefaultIntentType(agentID string) string {
    // Map agent ID to canonical intent type
    agentToIntent := map[string]agent.IntentType{
        "coder": agent.IntentCode,
        "debugger": agent.IntentDebug,
        "planner": agent.IntentPlan,
        "analyst": agent.IntentAnalyze,
        "committer": agent.IntentGit,
        "scheduler": agent.IntentSchedule,
        "chat": agent.IntentChat,
    }

    if intent, ok := agentToIntent[agentID]; ok {
        return string(intent)
    }
    return string(agent.IntentChat)
}
```

### Step 5: Update shouldCreateTask and ShouldDispatchAsync

**File:** `internal/agent/dispatcher.go`

**Changes:**

```go
// shouldCreateTask - use intent category
func (d *Dispatcher) shouldCreateTask(intent *Intent) bool {
    intentType := agent.IntentType(intent.Type)

    // CategoryInline never creates tasks
    if intentType.Category() == agent.CategoryInline {
        return false
    }

    // Always create tasks for trackable/defer intents
    return intentType.Category() == agent.CategoryTrackable ||
           intentType.Category() == agent.CategoryDefer
}

// ShouldDispatchAsync - use RequiresPlanning flag
func (d *Dispatcher) ShouldDispatchAsync(result *DispatchResult) bool {
    if result == nil || result.Intent == nil {
        return false
    }

    // Skills are always inline
    if result.Response != "" {
        return false
    }

    intentType := agent.IntentType(result.Intent.Type)

    // Use the RequiresPlanning flag from registry
    return intentType.RequiresPlanning()
}
```

### Step 6: Add Validation Hook

**File:** `internal/agent/dispatcher.go` (NEW: post-completion validation)

```go
// ValidateRouting checks if a completed task was routed correctly.
// Returns validation result with confidence and feedback.
type RoutingValidation struct {
    TaskID         string   `json:"task_id"`
    OriginalIntent string   `json:"original_intent"`
    RoutedAgent    string   `json:"routed_agent"`
    IsValid        bool     `json:"is_valid"`
    Confidence     float64  `json:"confidence"`
    Feedback       string   `json:"feedback,omitempty"`
    ExpectedAgent  string   `json:"expected_agent,omitempty"`
}

// ValidateRouting compares the routed agent against expected.
func (d *Dispatcher) ValidateRouting(taskID, originalIntent, routedAgent, result string) *RoutingValidation {
    intentType := agent.IntentType(originalIntent)
    def, ok := agent.IntentRegistry[intentType]

    if !ok {
        return &RoutingValidation{
            TaskID: taskID,
            IsValid: false,
            Feedback: fmt.Sprintf("Unknown intent type: %s", originalIntent),
        }
    }

    expectedAgent := def.DefaultAgent
    isValid := routedAgent == expectedAgent

    // Special case: chat agent can handle any inline intent
    if routedAgent == "chat" && intentType.Category() == agent.CategoryInline {
        isValid = true
    }

    return &RoutingValidation{
        TaskID: taskID,
        OriginalIntent: originalIntent,
        RoutedAgent: routedAgent,
        IsValid: isValid,
        Confidence: 0.9, // TODO: enhance with LLM-based validation
        ExpectedAgent: expectedAgent,
        Feedback: func() string {
            if isValid {
                return "Correct routing"
            }
            return fmt.Sprintf("Expected agent '%s' for intent '%s'", expectedAgent, originalIntent)
        }(),
    }
}
```

### Step 7: Expose Validation Tool

**File:** `internal/tools/platform.go`

**New tool:** `platform_validate_routing`

```go
type ValidateRoutingTool struct {
    dispatcher *Dispatcher
}

func (t *ValidateRoutingTool) Description() string {
    return "Validate if a task was routed to the correct agent"
}

func (t *ValidateRoutingTool) Handler(ctx context.Context, input json.RawMessage) (string, error) {
    var req struct {
        TaskID      string `json:"task_id"`
        IntentType  string `json:"intent_type"`
        RoutedAgent string `json:"routed_agent"`
        Result      string `json:"result,omitempty"`
    }
    json.Unmarshal(input, &req)

    validation := t.dispatcher.ValidateRouting(
        req.TaskID,
        req.IntentType,
        req.RoutedAgent,
        req.Result,
    )

    data, _ := json.MarshalIndent(validation, "", "  ")
    return string(data), nil
}
```

### Step 8: Integrate Validation into Task Completion

**File:** `internal/agent/handler.go`

**Location:** `handleTaskCompleted()` (line 521)

```go
// After receiving task.completed:
validation := h.dispatcher.ValidateRouting(
    payload.TaskID,
    originalIntent,  // Need to store this from dispatch
    payload.AgentID, // Who actually executed
    payload.Result,
)

if !validation.IsValid {
    h.logger.Warn("Routing validation failed",
        "task_id", payload.TaskID,
        "expected", validation.ExpectedAgent,
        "got", validation.RoutedAgent,
    )
    // Record for Phase 3 learning
    h.dispatcher.stats.recordValidationFailure(validation)
}
```

---

## Deliverables

| Item | Description |
|------|-------------|
| `IntentType` enum | Single source of truth in `intent.go` |
| `IntentRegistry` | All intent definitions with metadata |
| Updated classifiers | All 3 classifiers use enum |
| `ValidateRouting()` | Post-completion validation |
| `platform_validate_routing` tool | Query routing correctness |

---

## Success Criteria

1. ✅ No hardcoded intent strings in dispatcher/classifiers
2. ✅ All intent references compile against the enum
3. ✅ Validation catches at least obvious mismatches (e.g., `code` → `chat`)
4. ✅ New intents can be added by modifying ONE file

---

## Testing

### Compilation Test

```bash
# This should fail if any hardcoded strings remain
go build ./internal/agent/...
```

### Validation Test

```go
func TestValidateRouting(t *testing.T) {
    d := &Dispatcher{}

    // Valid routing
    v := d.ValidateRouting("t1", "code", "coder", "done")
    assert.True(t, v.IsValid)

    // Invalid routing
    v = d.ValidateRouting("t2", "code", "chat", "done")
    assert.False(t, v.IsValid)
}
```

---

## Dependencies

- **Phase 1**: Need `DispatcherStats` for logging validation failures

---

## Risks

| Risk | Mitigation |
|------|------------|
| Breaking existing code | Provide conversion functions, update in stages |
| Enum becomes stale | Code ownership review on new intent additions |
| Validation overhead | Run async, sample 10% by default |

---

## Next Phase

→ **Phase 3: Compound Request Support (Multi-Intent)**

With a unified taxonomy, detecting and routing compound intents becomes straightforward—simply detect multiple `IntentType` values in a single request and trigger orchestration.
