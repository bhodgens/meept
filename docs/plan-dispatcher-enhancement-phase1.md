# Phase 1: Dispatcher Analytics Subsystem

**Status:** Completed
**Priority:** High (foundation for all other phases)
**Estimated Effort:** 1 sprint
**Completed:** 2026-04-24

---

## Summary

All implementation steps completed:

1. **DispatcherStats struct enhanced** - Added mutex, ByMethod tracking, FallbackCount, and FallbackDetails
2. **classifyIntent() instrumented** - Tracks every classification decision by method, agent, and intent
3. **Stats access methods added** - GetStats() and GetFallbackDetails() for retrieving statistics
4. **Platform stats query implemented** - handleStatsQuery() returns JSON statistics

All tests pass. The implementation matches the plan specification with added nil-safety for test compatibility.

---

## Overview

This phase establishes the observability foundation needed to understand dispatcher behavior, track routing decisions, and collect data for learning from past performance. Without this telemetry, any classifier improvements are blind.

**Current State (verified 2026-04-24):**
- `DispatcherStats` struct exists at `dispatcher.go:574-578` but is **never populated or used**
- The dispatcher has a full classifier fallback chain (capability matcher → LLM → keyword → chat)
- No tracking of which classification method succeeds
- No correlation between dispatch decisions and task outcomes

---

## Objectives

1. **Implement `DispatcherStats`** - Track dispatch counts, intent distributions, fallback rates, and classification methods
2. **Log fallbacks** - Capture all requests that fall through to `chat` agent for pattern analysis
3. **Track task outcomes** - Correlate dispatch decisions with task completion/failure for feedback learning

---

## Implementation Steps

### Step 1: Enhance `DispatcherStats` Structure

**File:** `internal/agent/dispatcher.go`

**Current State:**
```go
// Line 574-578: Basic struct with no tracking methods
type DispatcherStats struct {
    TotalDispatched int            `json:"total_dispatched"`
    ByAgent         map[string]int `json:"by_agent"`
    ByIntent        map[string]int `json:"by_intent"`
}
```

**Changes:**

```go
// DispatcherStats tracks routing and classification statistics.
type DispatcherStats struct {
    mu sync.RWMutex

    // Total counts
    TotalDispatched int `json:"total_dispatched"`

    // By classification method
    ByMethod map[string]int `json:"by_method"` // "capability_matcher", "llm", "keyword", "fallback"

    // By agent routing
    ByAgent map[string]int `json:"by_agent"`

    // By intent type
    ByIntent map[string]int `json:"by_intent"`

    // Fallback tracking
    FallbackCount int `json:"fallback_count"`
    FallbackDetails []FallbackEntry `json:"fallback_details,omitempty"`
}

// FallbackEntry captures details about a fallback routing decision.
type FallbackEntry struct {
    Timestamp time.Time `json:"timestamp"`
    Input     string    `json:"input"`
    Method    string    `json:"method"` // which classifier failed
    Confidence float64  `json:"confidence"`
    RoutedTo  string    `json:"routed_to"`
}
```

**Implementation:**

1. Add `stats *DispatcherStats` field to `Dispatcher` struct (after line 61)
2. Initialize in `NewDispatcher()` (after line 97):
   ```go
   d.stats = &DispatcherStats{
       ByMethod: make(map[string]int),
       ByAgent: make(map[string]int),
       ByIntent: make(map[string]int),
   }
   ```

### Step 2: Track Every Classification Decision

**File:** `internal/agent/dispatcher.go`

**Location:** `classifyIntent()` function (lines 215-288)

**Changes:**

Add helper methods for stats tracking:

```go
// Helper methods for stats tracking (add after classifyIntent)
func (d *Dispatcher) recordMethod(method string) {
    d.stats.mu.Lock()
    defer d.stats.mu.Unlock()
    if d.stats.ByMethod == nil {
        d.stats.ByMethod = make(map[string]int)
    }
    d.stats.ByMethod[method]++
}

func (d *Dispatcher) recordAgent(agentID string) {
    d.stats.mu.Lock()
    defer d.stats.mu.Unlock()
    if d.stats.ByAgent == nil {
        d.stats.ByAgent = make(map[string]int)
    }
    d.stats.ByAgent[agentID]++
}

func (d *Dispatcher) recordIntent(intentType string) {
    d.stats.mu.Lock()
    defer d.stats.mu.Unlock()
    if d.stats.ByIntent == nil {
        d.stats.ByIntent = make(map[string]int)
    }
    d.stats.ByIntent[intentType]++
}

func (d *Dispatcher) recordFallback(input string, method string, confidence float64, routedTo string) {
    d.stats.mu.Lock()
    defer d.stats.mu.Unlock()
    d.stats.FallbackCount++
    d.stats.FallbackDetails = append(d.stats.FallbackDetails, FallbackEntry{
        Timestamp: time.Now().UTC(),
        Input: truncateString(input, 200),
        Method: method,
        Confidence: confidence,
        RoutedTo: routedTo,
    })
    // Keep only last 100 fallbacks
    if len(d.stats.FallbackDetails) > 100 {
        d.stats.FallbackDetails = d.stats.FallbackDetails[len(d.stats.FallbackDetails)-100:]
    }
}
```

**Update `classifyIntent()` to track each path:**

```go
func (d *Dispatcher) classifyIntent(ctx context.Context, input string, context []memory.MemoryResult) (*Intent, error) {
    d.stats.mu.Lock()
    d.stats.TotalDispatched++
    d.stats.mu.Unlock()

    // Step 1: Capability matcher
    if d.capabilityMatcher != nil {
        result := d.capabilityMatcher.Match(input)
        if result != nil && result.Confidence >= 0.7 {
            d.recordMethod("capability_matcher")
            d.recordAgent(result.AgentID)
            d.recordIntent(result.IntentType)
            return &Intent{
                Type: result.IntentType,
                Confidence: result.Confidence,
                AgentType: result.AgentID,
                Summary: extractSummary(input),
            }, nil
        }
    }

    // Step 2: LLM classifier
    if d.llmClassifier != nil {
        intent, err := d.llmClassifier.Classify(ctx, input, context)
        if err == nil && intent != nil && ShouldUseLLMResult(intent) {
            d.recordMethod("llm")
            d.recordAgent(intent.AgentType)
            d.recordIntent(intent.Type)
            return intent, nil
        }
    }

    // Step 3: Keyword classifier
    if d.keywordClassifier != nil {
        intent, err := d.keywordClassifier.Classify(ctx, input, context)
        if err == nil && intent != nil {
            d.recordMethod("keyword")
            d.recordAgent(intent.AgentType)
            d.recordIntent(intent.Type)
            return intent, nil
        }
    }

    // Step 4: Fallback - CRITICAL TO LOG
    d.recordFallback(input, "all_classifiers_failed", 0.0, "chat")
    return &Intent{
        Type: "chat",
        Confidence: 0.3,
        AgentType: "chat",
        Summary: "Could not determine intent, clarifying with user",
    }, nil
}
```

### Step 3: Add Stats Access Methods

**File:** `internal/agent/dispatcher.go`

**New methods:**

```go
// GetStats returns a copy of current dispatcher statistics.
func (d *Dispatcher) GetStats() DispatcherStats {
    d.stats.mu.RLock()
    defer d.stats.mu.RUnlock()
    return *d.stats
}

// GetFallbackDetails returns recent fallback entries for analysis.
func (d *Dispatcher) GetFallbackDetails(limit int) []FallbackEntry {
    d.stats.mu.RLock()
    defer d.stats.mu.RUnlock()
    if limit > len(d.stats.FallbackDetails) {
        limit = len(d.stats.FallbackDetails)
    }
    return d.stats.FallbackDetails[len(d.stats.FallbackDetails)-limit:]
}
```

### Step 4: Expose Stats via Platform Query

The existing `handlePlatformIntrospection` (lines 384-425) can be extended:

```go
// Add new method for stats queries
func (d *Dispatcher) handleStatsQuery(ctx context.Context) (string, error) {
    stats := d.GetStats()

    result := map[string]any{
        "total_dispatched": stats.TotalDispatched,
        "by_method": stats.ByMethod,
        "by_agent": stats.ByAgent,
        "by_intent": stats.ByIntent,
        "fallback_count": stats.FallbackCount,
    }

    data, _ := json.MarshalIndent(result, "", "  ")
    return string(data), nil
}
```

---

## Deliverables

| Item | Description |
|------|-------------|
| `DispatcherStats` struct | Enhanced with method tracking and fallback details |
| `classifyIntent()` instrumentation | Tracks every classification decision |
| Stats access methods | `GetStats()`, `GetFallbackDetails()` |
| Platform stats query | Returns JSON statistics |

---

## Success Criteria

1. Every dispatch increments exactly one counter in `ByMethod`
2. Fallback requests are logged with input text (limited to 100 most recent)
3. Stats query returns JSON with all stats fields
4. No performance regression (stats tracking adds < 1ms per dispatch)

---

## Testing

```bash
go test ./internal/agent/... -run TestDispatcherStats
```

---

## Dependencies

- None (this is foundational infrastructure)

---

## Alignment with Determinism Audit

The determinism audit (`docs/audit-determinism-mk2.md`) verified execution-side determinism (evidence flow, validator coverage, state transitions). Phase 1 provides visibility into **routing decisions** before tasks are executed.

---

## Next Phase

→ **Phase 2: Unified Intent Taxonomy + Validation**
