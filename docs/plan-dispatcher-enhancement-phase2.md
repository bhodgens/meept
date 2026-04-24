# Phase 2: Unified Intent Taxonomy + Validation

**Status:** Completed
**Priority:** High (requires Phase 1)
**Estimated Effort:** 1 sprint
**Completed:** 2026-04-24

---

## Summary

All implementation steps completed:

1. **IntentType enum created** - Single source of truth in `intent.go` with all intent types, Category(), DefaultAgent(), RequiresPlanning(), ShouldCreateTask(), ShouldDispatchAsync(), and IsValidIntentType() methods
2. **KeywordClassifier updated** - Uses IntentType constants instead of string literals
3. **shouldCreateTask and ShouldDispatchAsync updated** - Use IntentType methods with fallback for unknown intents
4. **ValidateRouting() added** - Post-completion routing validation with RoutingValidation struct

All tests pass.

---

## Overview

Currently, intent types are defined as raw strings in multiple places with overlapping but inconsistent values. This phase consolidates all intent definitions into a single source of truth and adds validation to ensure routing decisions are correct.

**Current State (verified 2026-04-24):**
- Intent types are raw strings: `"chat"`, `"code"`, `"debug"`, `"platform"`, `"report"`, `"recall"`, `"analyze"`, `"search"`, `"plan"`, `"git"`, `"schedule"`, `"review"`, `"skill"`
- Keyword classifier defines patterns at `dispatcher.go:493-532`
- `shouldCreateTask` at `dispatcher.go:302-315` uses hardcoded switch cases
- `ShouldDispatchAsync` at `dispatcher.go:647-673` uses hardcoded switch cases
- No centralized `IntentType` enum exists

---

## Objectives

1. **Create `IntentType` enum** - Single source of truth for all intent types
2. **Consolidate mappings** - All classifiers reference the enum
3. **Add validation** - Post-completion routing correctness check

---

## Implementation Steps

### Step 1: Create IntentType Enum

**File:** `internal/agent/intent.go` (NEW)

```go
package agent

import "strings"

// IntentType represents a classified user intent.
type IntentType string

const (
    // Unknown
    IntentUnknown IntentType = "unknown"

    // Conversational (inline handling)
    IntentChat     IntentType = "chat"
    IntentReport   IntentType = "report"
    IntentRecall   IntentType = "recall"
    IntentPlatform IntentType = "platform"

    // Execution (async to orchestrator)
    IntentCode     IntentType = "code"
    IntentDebug    IntentType = "debug"
    IntentReview   IntentType = "review"
    IntentPlan     IntentType = "plan"
    IntentGit      IntentType = "git"
    IntentSchedule IntentType = "schedule"

    // Analysis (inline)
    IntentAnalyze  IntentType = "analyze"
    IntentSearch   IntentType = "search"

    // Skill invocation
    IntentSkill    IntentType = "skill"
)

// IntentCategory groups intents by routing behavior.
type IntentCategory string

const (
    CategoryInline   IntentCategory = "inline"
    CategoryTrackable IntentCategory = "trackable"
    CategoryDefer    IntentCategory = "defer"
)

// Category returns the routing category for an intent.
func (t IntentType) Category() IntentCategory {
    switch t {
    case IntentChat, IntentReport, IntentRecall, IntentPlatform, IntentAnalyze, IntentSearch:
        return CategoryInline
    case IntentCode, IntentDebug, IntentReview, IntentPlan, IntentGit, IntentSchedule:
        return CategoryDefer
    case IntentSkill:
        return CategoryInline
    default:
        return CategoryInline
    }
}

// DefaultAgent returns the default agent for an intent.
func (t IntentType) DefaultAgent() string {
    switch t {
    case IntentChat, IntentReport, IntentRecall, IntentPlatform:
        return "chat"
    case IntentCode, IntentReview:
        return "coder"
    case IntentDebug:
        return "debugger"
    case IntentPlan:
        return "planner"
    case IntentAnalyze, IntentSearch:
        return "analyst"
    case IntentGit:
        return "committer"
    case IntentSchedule:
        return "scheduler"
    case IntentSkill:
        return "skill"
    default:
        return "chat"
    }
}

// RequiresPlanning returns true if the intent benefits from orchestration.
func (t IntentType) RequiresPlanning() bool {
    switch t {
    case IntentCode, IntentPlan:
        return true
    default:
        return false
    }
}

// IsValid IntentType checks if a string is a valid intent type.
func IsValidIntentType(s string) bool {
    switch IntentType(s) {
    case IntentChat, IntentReport, IntentRecall, IntentPlatform,
         IntentCode, IntentDebug, IntentReview, IntentPlan, IntentGit,
         IntentSchedule, IntentAnalyze, IntentSearch, IntentSkill:
        return true
    }
    return false
}

// Keywords returns common trigger phrases for documentation/logging.
func (t IntentType) Keywords() []string {
    switch t {
    case IntentChat:
        return []string{"hello", "hi", "thanks", "help"}
    case IntentReport:
        return []string{"report", "what did you", "summary", "progress"}
    case IntentRecall:
        return []string{"remember", "recall", "last time"}
    case IntentPlatform:
        return []string{"capabilities", "what can you", "platform"}
    case IntentCode:
        return []string{"implement", "create", "add feature", "refactor"}
    case IntentDebug:
        return []string{"fix bug", "error", "broken", "not working"}
    case IntentReview:
        return []string{"review pr", "check code", "code review"}
    case IntentGit:
        return []string{"commit", "push", "pull", "merge", "branch"}
    case IntentSchedule:
        return []string{"remind", "schedule", "alarm", "at "}
    case IntentPlan:
        return []string{"plan", "design", "architect", "how should i"}
    case IntentAnalyze, IntentSearch:
        return []string{"research", "analyze", "explain", "search"}
    default:
        return nil
    }
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
    intentType IntentType
    agentType  string
    confidence float64
    planning   bool
}{
    // Platform introspection
    {[]string{"what can you do", "what are your capabilities"},
     IntentPlatform, "chat", 0.9, false},

    // Report requests
    {[]string{"give me a report", "what did you do"},
     IntentReport, "chat", 0.9, false},

    // Recall
    {[]string{"remember when", "recall"},
     IntentRecall, "chat", 0.85, false},

    // Debug
    {[]string{"fix bug", "debug", "error"},
     IntentDebug, "debugger", 0.8, false},

    // Code
    {[]string{"write code", "implement", "create function"},
     IntentCode, "coder", 0.8, false},

    // Git
    {[]string{"commit", "push", "pull", "merge", "branch"},
     IntentGit, "committer", 0.8, false},

    // Schedule
    {[]string{"remind", "schedule", "alarm", "at "},
     IntentSchedule, "scheduler", 0.8, false},

    // Plan
    {[]string{"plan", "design", "architect", "how should i"},
     IntentPlan, "planner", 0.8, true},

    // Analyze/Search
    {[]string{"research", "analyze", "explain"},
     IntentAnalyze, "analyst", 0.7, false},
    {[]string{"search", "find", "look up"},
     IntentSearch, "analyst", 0.7, false},

    // Chat
    {[]string{"hello", "hi", "thanks"},
     IntentChat, "chat", 0.6, false},
}

// In the return statement:
return &Intent{
    Type: string(p.intentType),
    // ...
}, nil
```

### Step 3: Update shouldCreateTask and ShouldDispatchAsync

Replace hardcoded string switches with enum methods.

**File:** `internal/agent/dispatcher.go`

```go
// shouldCreateTask - use intent category
func (d *Dispatcher) shouldCreateTask(intent *Intent) bool {
    intentType := IntentType(intent.Type)
    // CategoryInline never creates tasks; Defer always does
    return intentType.Category() == CategoryTrackable ||
           intentType.Category() == CategoryDefer
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

    intentType := IntentType(result.Intent.Type)
    // Simple intents are handled inline
    if intentType.Category() == CategoryInline {
        return false
    }

    // Use the RequiresPlanning flag
    return intentType.RequiresPlanning()
}
```

### Step 4: Add Validation Hook

**File:** `internal/agent/dispatcher.go` (NEW)

```go
// RoutingValidation checks if a task was routed correctly.
type RoutingValidation struct {
    TaskID        string  `json:"task_id"`
    OriginalIntent string `json:"original_intent"`
    RoutedAgent   string  `json:"routed_agent"`
    IsValid       bool    `json:"is_valid"`
    ExpectedAgent string  `json:"expected_agent,omitempty"`
    Feedback      string  `json:"feedback,omitempty"`
}

// ValidateRouting compares the routed agent against expected.
func (d *Dispatcher) ValidateRouting(taskID, originalIntent, routedAgent string) *RoutingValidation {
    intentType := IntentType(originalIntent)

    // Check if intent type is valid
    if !IsValidIntentType(originalIntent) {
        return &RoutingValidation{
            TaskID: taskID,
            IsValid: false,
            Feedback: fmt.Sprintf("Unknown intent type: %s", originalIntent),
        }
    }

    expectedAgent := intentType.DefaultAgent()
    isValid := routedAgent == expectedAgent

    // Special case: chat agent can handle inline intents
    if routedAgent == "chat" && intentType.Category() == CategoryInline {
        isValid = true
    }

    return &RoutingValidation{
        TaskID: taskID,
        OriginalIntent: originalIntent,
        RoutedAgent: routedAgent,
        IsValid: isValid,
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

---

## Deliverables

| Item | Description |
|------|-------------|
| `IntentType` enum | Single source of truth in `intent.go` |
| `IntentRegistry` | All intent definitions with metadata |
| Updated classifiers | All 3 classifiers use enum |
| `ValidateRouting()` | Post-completion validation |

---

## Success Criteria

1. No hardcoded intent strings in dispatcher/classifiers
2. All intent references compile against the enum
3. Validation catches obvious mismatches (e.g., `code` → `chat`)

---

## Testing

```go
func TestValidateRouting(t *testing.T) {
    d := &Dispatcher{}

    // Valid routing
    v := d.ValidateRouting("t1", "code", "coder")
    assert.True(t, v.IsValid)

    // Invalid routing
    v = d.ValidateRouting("t2", "code", "chat")
    assert.False(t, v.IsValid)
}
```

---

## Dependencies

- **Phase 1**: Need `DispatcherStats` for logging validation failures

---

## Next Phase

→ **Phase 3: Compound Request Support (Multi-Intent)**
