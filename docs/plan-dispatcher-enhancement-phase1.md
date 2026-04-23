# Phase 1: Dispatcher Analytics Subsystem

**Status:** Not started
**Priority:** High (foundation for all other phases)
**Estimated Effort:** 2-3 sprints

---

## Overview

This phase establishes the observability foundation needed to understand dispatcher behavior, track routing decisions, and collect data for learning from past performance. Without this telemetry, any classifier improvements are blind.

---

## Objectives

1. **Implement `DispatcherStats`** - Actually track dispatch counts, intent distributions, fallback rates, and classification methods
2. **Log fallbacks** - Capture all requests that fall through to `chat` agent for pattern analysis
3. **Track task outcomes** - Correlate dispatch decisions with task completion/failure for feedback learning

---

## Implementation Steps

### Step 1: Implement `DispatcherStats` Structure

**File:** `internal/agent/dispatcher.go`

**Current State:** `DispatcherStats` struct exists (line ~574) but is never populated or used.

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

    // Time-based (for rate monitoring)
    LastHourCount int `json:"last_hour_count"`
    LastReset time.Time `json:"last_reset"`
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

1. Add `stats *DispatcherStats` field to `Dispatcher` struct
2. Initialize in `NewDispatcher()`:
   ```go
   d.stats = &DispatcherStats{
       ByMethod: make(map[string]int),
       ByAgent: make(map[string]int),
       ByIntent: make(map[string]int),
       LastReset: time.Now(),
   }
   ```
3. Update stats in `classifyIntent()`:
   - Increment `ByMethod["capability_matcher"]` when capability matcher runs
   - Increment `ByMethod["llm"]` when LLM classifier runs
   - Increment `ByMethod["keyword"]` when keyword classifier runs
   - Increment `ByMethod["fallback"]` AND `FallbackCount` when falling back to chat

### Step 2: Track Every Classification Decision

**File:** `internal/agent/dispatcher.go`

**Location:** `classifyIntent()` function (lines ~220-288)

**Changes:**

```go
func (d *Dispatcher) classifyIntent(ctx context.Context, input string, context []memory.MemoryResult) (*Intent, error) {
    d.stats.mu.Lock()
    d.stats.TotalDispatched++
    d.stats.mu.Unlock()

    // Step 1: Capability matcher
    if d.capabilityMatcher != nil {
        result := d.capabilityMatcher.Match(input)
        if result != nil && result.Confidence >= 0.7 {
            d.stats.recordMethod("capability_matcher")
            d.stats.recordAgent(result.AgentID)
            d.stats.recordIntent(result.IntentType)
            // ... rest unchanged
        }
        d.stats.recordMethodAttempt("capability_matcher")
    }

    // Step 2: LLM classifier
    if d.llmClassifier != nil {
        intent, err := d.llmClassifier.Classify(ctx, input, context)
        if err == nil && intent != nil {
            if ShouldUseLLMResult(intent) {
                d.stats.recordMethod("llm")
                d.stats.recordAgent(intent.AgentType)
                d.stats.recordIntent(intent.Type)
                // ... rest unchanged
            }
        }
        d.stats.recordMethodAttempt("llm")
    }

    // Step 3: Keyword classifier
    if d.keywordClassifier != nil {
        intent, err := d.keywordClassifier.Classify(ctx, input, context)
        if err == nil && intent != nil {
            d.stats.recordMethod("keyword")
            d.stats.recordAgent(intent.AgentType)
            d.stats.recordIntent(intent.Type)
            // ... rest unchanged
        }
        d.stats.recordMethodAttempt("keyword")
    }

    // Step 4: Fallback - CRITICAL TO LOG
    d.stats.recordFallback(input, "all_classifiers_failed", 0.0, "chat")
    return &Intent{
        Type: "chat",
        Confidence: 0.3,
        AgentType: "chat",
        Summary: "Could not determine intent, clarifying with user",
    }, nil
}
```

### Step 3: Wire Up Task Outcome Tracking

**File:** `internal/agent/handler.go`

**Location:** `handleTaskCompleted()` and `handleTaskFailed()` (lines 521-661)

**Changes:**

Add outcome tracking that correlates back to the original dispatch decision:

```go
// In ChatHandler, add a dispatch log map
dispatchLog map[string]*DispatchEntry  // key: task_id

type DispatchEntry struct {
    TaskID      string    `json:"task_id"`
    SessionID   string    `json:"session_id"`
    Input       string    `json:"input"`
    IntentType  string    `json:"intent_type"`
    AgentID     string    `json:"agent_id"`
    Method      string    `json:"method"` // how it was classified
    Confidence  float64   `json:"confidence"`
    DispatchedAt time.Time `json:"dispatched_at"`
}

// In handleRequest, when async dispatch happens:
h.dispatchLog[result.Task.ID] = &DispatchEntry{
    TaskID: result.Task.ID,
    IntentType: result.Intent.Type,
    AgentID: result.AgentID,
    Method: "orchestrator",
    DispatchedAt: time.Now(),
}

// In handleTaskCompleted:
if entry, ok := h.dispatchLog[payload.TaskID]; ok {
    h.dispatcher.stats.recordSuccessfulOutcome(
        entry.IntentType,
        entry.AgentID,
        entry.Method,
        payload.ExecutionTime,
    )
    delete(h.dispatchLog, payload.TaskID)
}

// In handleTaskFailed:
if entry, ok := h.dispatchLog[payload.TaskID]; ok {
    h.dispatcher.stats.recordFailedOutcome(
        entry.IntentType,
        entry.AgentID,
        entry.Method,
        payload.Error,
    )
    delete(h.dispatchLog, payload.TaskID)
}
```

### Step 4: Add Stats Access Methods

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

// ResetStats clears statistics (useful for testing or periodic resets).
func (d *Dispatcher) ResetStats() {
    d.stats.mu.Lock()
    defer d.stats.mu.Unlock()
    d.stats = &DispatcherStats{
        ByMethod: make(map[string]int),
        ByAgent: make(map[string]int),
        ByIntent: make(map[string]int),
        LastReset: time.Now(),
    }
}
```

### Step 5: Expose Stats via Platform Tool

**File:** `internal/tools/platform.go` (create if doesn't exist, or add to existing platform tools)

**New tool:** `platform_stats`

```go
// In tool registry registration:
registry.Register("platform_stats", &PlatformStatsTool{dispatcher: dispatcher})

// Tool implementation:
type PlatformStatsTool struct {
    dispatcher *Dispatcher
}

func (t *PlatformStatsTool) Description() string {
    return "Retrieve dispatcher routing statistics and fallback analysis"
}

func (t *PlatformStatsTool) Handler(ctx context.Context, input json.RawMessage) (string, error) {
    var req struct {
        IncludeFallbacks bool `json:"include_fallbacks,omitempty"`
        FallbackLimit    int  `json:"fallback_limit,omitempty"`
    }
    json.Unmarshal(input, &req)

    stats := t.dispatcher.GetStats()

    result := map[string]any{
        "total_dispatched": stats.TotalDispatched,
        "by_method": stats.ByMethod,
        "by_agent": stats.ByAgent,
        "by_intent": stats.ByIntent,
        "fallback_count": stats.FallbackCount,
        "last_reset": stats.LastReset,
    }

    if req.IncludeFallbacks {
        limit := req.FallbackLimit
        if limit == 0 {
            limit = 20
        }
        result["fallback_details"] = t.dispatcher.GetFallbackDetails(limit)
    }

    data, _ := json.MarshalIndent(result, "", "  ")
    return string(data), nil
}
```

### Step 6: Add Bus Event for Stats Updates

**File:** `internal/bus/events.go` (or wherever bus events are defined)

**New event type:**

```go
const (
    EventTypeDispatcherStats = "dispatcher.stats"
)
```

Publish a stats snapshot periodically (every 5 minutes or after every 100 dispatches):

```go
// In dispatcher.go, add periodic publish
func (d *Dispatcher) startStatsReporting(ctx context.Context) {
    ticker := time.NewTicker(5 * time.Minute)
    go func() {
        for {
            select {
            case <-ctx.Done():
                ticker.Stop()
                return
            case <-ticker.C:
                stats := d.GetStats()
                payload, _ := json.Marshal(stats)
                d.bus.Publish(EventTypeDispatcherStats, &bus.Message{
                    Payload: payload,
                })
            }
        }
    }()
}
```

---

## Deliverables

| Item | Description |
|------|-------------|
| `DispatcherStats` struct | Populated with real data |
| `classifyIntent()` instrumentation | Tracks every classification decision |
| `handleTaskCompleted/Failed` correlation | Links dispatch to outcomes |
| `platform_stats` tool | Query dispatcher stats from agents |
| Bus event `dispatcher.stats` | Periodic stats publication |

---

## Success Criteria

1. ✅ Every dispatch increments exactly one counter in `ByMethod`
2. ✅ Fallback requests are logged with input text for pattern analysis
3. ✅ Task completion/failure correlates back to original dispatch method
4. ✅ `platform_stats` tool returns JSON with all stats fields
5. ✅ Stats survive daemon restart (optional: persist to SQLite)

---

## Testing

### Unit Tests

```bash
go test ./internal/agent/... -run TestDispatcherStats
go test ./internal/agent/... -run TestClassifyIntentTracking
```

### Integration Tests

1. Run dispatcher with test inputs
2. Query `platform_stats` tool
3. Verify counts match expected values

### Manual Verification

```bash
# After running some chats:
./bin/meept chat "What's your status?"
./bin/meept chat "Fix the bug in server.go"
./bin/meept chat "Create a new endpoint"

# Then query stats
./bin/meept tools platform_stats '{}'
```

---

## Dependencies

- None (this is foundational infrastructure)

---

## Risks

| Risk | Mitigation |
|------|------------|
| Performance overhead from stats tracking | Use atomic operations where possible, batch updates |
| Memory growth from fallback details | Limit `FallbackDetails` to most recent 100 entries |
| Stats lost on restart | Optional: persist to SQLite alongside metrics |

---

## Next Phase

→ **Phase 2: Unified Intent Taxonomy + Validation**

Once Phase 1 is complete, you'll have visibility into:
- Which classification method fires most often
- Which intents are most common
- How often fallbacks occur and what the inputs look like

This data informs Phase 2's taxonomy consolidation.
