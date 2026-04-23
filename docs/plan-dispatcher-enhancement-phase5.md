# Phase 5: Context-Aware Classification

**Status:** Not started
**Priority:** Low (requires Phase 1-4)
**Estimated Effort:** 2-3 sprints

---

## Overview

Current dispatch classifies each request independently, ignoring conversation history. A request like "now do the same for logout" is ambiguous without context. This phase uses memory context and conversation history to improve classification accuracy.

---

## Problem Statement

### Current Context Handling

```
User: "Fix the login bug"
→ Classified as: debug → debugger ✓

User: "Now do the same for logout"
→ Classified as: chat (no keywords match) → chat agent ✗
```

### Desired Context-Aware Behavior

```
User: "Fix the login bug"
→ Classified as: debug → debugger ✓
→ Memory: {last_intent: "debug", last_agent: "debugger", task: "login bug"}

User: "Now do the same for logout"
→ Context: "the same" refers to last_intent (debug)
→ Classified as: debug → debugger ✓
```

---

## Objectives

1. **Context extraction** - Pull relevant context from memory/conversation history
2. **Context weighting** - Boost intent scores based on context relevance
3. **Anaphora resolution** - Handle "do the same", "also fix this", etc.
4. **Session continuity** - Track intent patterns per session
5. **Conversation-aware dispatcher** - Use history for classification

---

## Implementation Steps

### Step 1: Extend Memory Context Structure

**File:** `internal/agent/dispatcher.go`

**Current:** Memory context is only used for LLM classification.

**Changes:**

```go
// MemoryContext wraps memory results with conversation metadata.
type MemoryContext struct {
    // Results are the raw memory matches.
    Results []memory.MemoryResult `json:"results"`

    // LastIntent is the most recent intent from this session.
    LastIntent *Intent `json:"last_intent,omitempty"`

    // LastAgent is the most recent agent used.
    LastAgent string `json:"last_agent,omitempty"`

    // ConversationIntentCounts tracks intent frequency in this session.
    IntentCounts map[string]int `json:"intent_counts,omitempty"`

    // PendingTasks are incomplete tasks from this session.
    PendingTasks []*task.Task `json:"pending_tasks,omitempty"`
}

// In ClassifyAndRoute():
func (d *Dispatcher) ClassifyAndRoute(ctx context.Context, input string, sessionID string) (*DispatchResult, error) {
    // ... existing skill check ...

    // Step 1: ENHANCED memory search with context extraction
    memoryContext := d.buildMemoryContext(ctx, input, sessionID)

    // Step 2: Use context in classification
    intent, err := d.classifyIntent(ctx, input, memoryContext)
    // ... rest unchanged ...
}

// buildMemoryContext extracts conversation context from memory.
func (d *Dispatcher) buildMemoryContext(ctx context.Context, input string, sessionID string) *MemoryContext {
    ctx := &MemoryContext{
        IntentCounts: make(map[string]int),
    }

    // Search episodic memory for this session
    if d.memoryMgr != nil {
        results, _ := d.memoryMgr.Search(ctx, memory.MemoryQuery{
            Query: input,
            Limit: 10,
            SessionID: sessionID,  // Filter to current session
        })
        ctx.Results = results
    }

    // Extract last intent from session memory
    if d.memvid != nil {
        episodic := d.memvid.WithZone("episodic")
        recent, _ := episodic.Search(ctx, sessionID, 5)  // Last 5 from this session

        for _, r := range recent {
            // Parse metadata
            if intentType, ok := r.Memory.Metadata["intent_type"].(string); ok {
                ctx.LastIntent = &Intent{
                    Type: intentType,
                }
                ctx.IntentCounts[intentType]++
            }
            if agentID, ok := r.Memory.Metadata["agent_id"].(string); ok {
                ctx.LastAgent = agentID
            }
        }
    }

    // Get pending tasks from session
    if d.taskStore != nil {
        pending := d.taskStore.ListBySession(sessionID)
        for _, t := range pending {
            if t.State == "pending" || t.State == "running" {
                ctx.PendingTasks = append(ctx.PendingTasks, t)
            }
        }
    }

    return ctx
}
```

### Step 2: Add Context Weighting to Classification

**File:** `internal/agent/dispatcher.go`

```go
func (d *Dispatcher) classifyIntent(ctx context.Context, input string, memCtx *MemoryContext) (*Intent, error) {
    // ... existing classificationchain ...

    // After getting intent from any classifier, apply context boost
    if intent != nil && memCtx != nil {
        intent = d.applyContextWeighting(intent, memCtx, input)
    }

    return intent, nil
}

// applyContextWeighting adjusts confidence based on conversation context.
func (d *Dispatcher) applyContextWeighting(intent *Intent, memCtx *MemoryContext, input string) *Intent {
    boost := 0.0

    // Boost 1: Same intent as last request (continuity)
    if memCtx.LastIntent != nil && memCtx.LastIntent.Type == intent.Type {
        boost += 0.15
        d.logger.Debug("Context boost: same intent as last request",
            "intent", intent.Type,
            "boost", boost,
        )
    }

    // Boost 2: Same agent as last request
    if memCtx.LastAgent != "" && memCtx.LastAgent == intent.AgentType {
        boost += 0.1
    }

    // Boost 3: Intent is common in this session
    if count, ok := memCtx.IntentCounts[intent.Type]; ok && count >= 2 {
        boost += 0.05 * float64(count)  // +0.10 for 2, +0.15 for 3, etc.
    }

    // Boost 4: Input contains anaphora (refers to context)
    if hasAnaphora(input) && memCtx.LastIntent != nil {
        // "do the same", "also", "this", "that" → trust context more
        if intent.Type == memCtx.LastIntent.Type {
            boost += 0.2
        }
    }

    // Boost 5: Pending task context
    for _, pending := range memCtx.PendingTasks {
        if strings.Contains(strings.ToLower(input), strings.ToLower(pending.Name)) {
            // User is referring to existing task
            boost += 0.15
            intent.Summary = fmt.Sprintf("Continue: %s", pending.Name)
        }
    }

    // Cap boost at 0.3
    if boost > 0.3 {
        boost = 0.3
    }

    intent.Confidence = min(1.0, intent.Confidence+boost)
    return intent
}

// hasAnaphora checks if input contains context-referring language.
func hasAnaphora(input string) bool {
    lower := strings.ToLower(input)
    anaphora := []string{
        "do the same", "same thing", "also", "too", "as well",
        "this", "that", "these", "those",
        "continue", "keep going", "next",
        "the above", "the previous",
    }

    for _, word := range anaphora {
        if strings.Contains(lower, word) {
            return true
        }
    }

    return false
}
```

### Step 3: Handle Explicit Context References

**File:** `internal/agent/dispatcher.go` (NEW)

```go
// resolveAnaphora replaces context references with actual content.
// E.g., "fix the same issue" → "fix the login bug" (from last intent)
func (d *Dispatcher) resolveAnaphora(ctx context.Context, input string, memCtx *MemoryContext) string {
    if memCtx == nil || memCtx.LastIntent == nil {
        return input
    }

    lower := strings.ToLower(input)

    // Pattern: "do the same for X" → expand to last action + X
    if strings.Contains(lower, "do the same") {
        lastSummary := memCtx.LastIntent.Summary
        // Extract object from input: "do the same for logout"
        forMatch := regexp.MustCompile(`do the same for (.+)`)
        if match := forMatch.FindStringSubmatch(lower); match != nil {
            return fmt.Sprintf("%s for %s", lastSummary, match[1])
        }
    }

    // Pattern: "also fix this" → append context
    if strings.Contains(lower, "also") && strings.Contains(lower, "fix") {
        if memCtx.PendingTasks != nil && len(memCtx.PendingTasks) > 0 {
            return fmt.Sprintf("%s: %s", memCtx.PendingTasks[0].Name, input)
        }
    }

    // Pattern: "continue with X" → reference pending task
    if strings.Contains(lower, "continue") {
        for _, pending := range memCtx.PendingTasks {
            if strings.Contains(lower, strings.ToLower(pending.Name)) {
                return fmt.Sprintf("Continue task %s: %s", pending.ID, pending.Description)
            }
        }
    }

    return input
}
```

### Step 4: Add Session Intent Tracker

**File:** `internal/agent/session_tracker.go` (NEW)

```go
package agent

import (
    "sync"
    "time"
)

// SessionTracker tracks conversation patterns per session.
type SessionTracker struct {
    mu       sync.RWMutex
    sessions map[string]*SessionState
    maxAge   time.Duration
}

// SessionState holds state for a single conversation session.
type SessionState struct {
    SessionID      string
    CreatedAt      time.Time
    LastActivityAt time.Time
    IntentHistory  []*Intent
    AgentHistory   []string
    TotalRequests  int
}

// NewSessionTracker creates a new session tracker.
func NewSessionTracker(maxAge time.Duration) *SessionTracker {
    return &SessionTracker{
        sessions: make(map[string]*SessionState),
        maxAge: maxAge,
    }
}

// RecordIntent logs an intent for a session.
func (t *SessionTracker) RecordIntent(sessionID string, intent *Intent, agentID string) {
    t.mu.Lock()
    defer t.mu.Unlock()

    state := t.getOrCreateSession(sessionID)
    state.IntentHistory = append(state.IntentHistory, intent)
    state.AgentHistory = append(state.AgentHistory, agentID)
    state.TotalRequests++
    state.LastActivityAt = time.Now()

    // Keep only last 20 intents
    if len(state.IntentHistory) > 20 {
        state.IntentHistory = state.IntentHistory[len(state.IntentHistory)-20:]
    }
}

// GetSession returns session state (for context building).
func (t *SessionTracker) GetSession(sessionID string) *SessionState {
    t.mu.RLock()
    defer t.mu.RUnlock()
    return t.sessions[sessionID]
}

// GetDominantIntent returns the most frequent intent in session.
func (t *SessionTracker) GetDominantIntent(sessionID string) string {
    state := t.GetSession(sessionID)
    if state == nil {
        return ""
    }

    counts := make(map[string]int)
    var maxIntent string
    maxCount := 0

    for _, intent := range state.IntentHistory {
        counts[intent.Type]++
        if counts[intent.Type] > maxCount {
            maxCount = counts[intent.Type]
            maxIntent = intent.Type
        }
    }

    return maxIntent
}

// Cleanup removes expired sessions.
func (t *SessionTracker) Cleanup() {
    t.mu.Lock()
    defer t.mu.Unlock()

    now := time.Now()
    for id, state := range t.sessions {
        if now.Sub(state.LastActivityAt) > t.maxAge {
            delete(t.sessions, id)
        }
    }
}

func (t *SessionTracker) getOrCreateSession(sessionID string) *SessionState {
    if state, ok := t.sessions[sessionID]; ok {
        return state
    }

    state := &SessionState{
        SessionID: sessionID,
        CreatedAt: time.Now(),
        LastActivityAt: time.Now(),
    }
    t.sessions[sessionID] = state
    return state
}
```

**Integration with Dispatcher:**

```go
// In Dispatcher struct:
sessionTracker *SessionTracker

// In NewDispatcher():
d.sessionTracker = NewSessionTracker(30 * time.Minute)

// In ClassifyAndRoute, after successful classification:
d.sessionTracker.RecordIntent(sessionID, result.Intent, result.AgentID)

// In buildMemoryContext():
memCtx.SessionState = d.sessionTracker.GetSession(sessionID)
```

### Step 5: Add Context Stats

**File:** `internal/agent/dispatcher.go`

```go
// In DispatcherStats:
ContextBoosts int `json:"context_boosts"`
AnaphoraResolutions int `json:"anaphora_resolutions"`
AvgConfidenceWithCtx float64 `json:"avg_confidence_with_context"`
AvgConfidenceWithoutCtx float64 `json:"avg_confidence_without_context"`

func (d *Dispatcher) stats.recordContextBoost() {
    d.stats.mu.Lock()
    d.stats.ContextBoosts++
    d.stats.mu.Unlock()
}
```

### Step 6: Add Context Tool

**File:** `internal/tools/platform.go`

```go
// New tool: platform_session_context
type SessionContextTool struct {
    tracker *SessionTracker
}

func (t *SessionContextTool) Description() string {
    return "Get conversation context for the current session"
}

func (t *SessionContextTool) Handler(ctx context.Context, input json.RawMessage) (string, error) {
    var req struct {
        SessionID string `json:"session_id"`
    }
    json.Unmarshal(input, &req)

    state := t.tracker.GetSession(req.SessionID)
    if state == nil {
        return `{"session": null}`, nil
    }

    result := map[string]any{
        "session_id": state.SessionID,
        "total_requests": state.TotalRequests,
        "last_intent": state.IntentHistory[len(state.IntentHistory)-1],
        "dominant_intent": t.tracker.GetDominantIntent(state.SessionID),
        "intent_history": state.IntentHistory,
    }

    data, _ := json.MarshalIndent(result, "", "  ")
    return string(data), nil
}
```

---

## Deliverables

| Item | Description |
|------|-------------|
| `MemoryContext` struct | Extended with session metadata |
| `applyContextWeighting()` | Boost scores based on context |
| `resolveAnaphora()` | Expand context references |
| `SessionTracker` | Per-session intent history |
| Context stats | Track boost effectiveness |
| `platform_session_context` tool | Query session history |

---

## Success Criteria

1. ✅ "Do the same" requests route correctly
2. ✅ Session continuity improves classification confidence
3. ✅ Anaphora resolution expands references
4. ✅ Context boosts increase accuracy by 10%+

---

## Testing

### Unit Tests

```go
func TestContextWeighting(t *testing.T) {
    d := setupDispatcher()
    memCtx := &MemoryContext{
        LastIntent: &Intent{Type: "debug"},
        LastAgent: "debugger",
    }

    intent := &Intent{Type: "debug", AgentType: "debugger", Confidence: 0.7}
    boosted := d.applyContextWeighting(intent, memCtx, "fix this too")

    assert.Greater(t, boosted.Confidence, 0.7)  // Should be boosted
}

func TestAnaphoraResolution(t *testing.T) {
    memCtx := &MemoryContext{
        LastIntent: &Intent{Summary: "Fix the login bug"},
    }

    resolved := resolveAnaphora(ctx, "do the same for logout", memCtx)
    assert.Equal(t, "Fix the login bug for logout", resolved)
}
```

---

## Dependencies

- **Phase 1**: Need stats for tracking context effectiveness
- **Phase 4**: Semantic matching helps with anaphora

---

## Risks

| Risk | Mitigation |
|------|------------|
| Over-boosting (wrong context) | Cap boost at 0.3, require high base confidence |
| Memory bloat from session tracking | Limit to 20 intents, cleanup after 30 min |
| Privacy concerns with session history | Make opt-in via config |

---

## Completion

Phase 5 completes the dispatcher enhancement roadmap. After this:

- **Phase 1** ✅: Full visibility into dispatcher behavior
- **Phase 2** ✅: Unified intent taxonomy
- **Phase 3** ✅: Compound request handling
- **Phase 4** ✅: Semantic matching
- **Phase 5** ✅: Context-aware classification

The dispatcher now handles:
- Simple requests (via all classifiers)
- Compound requests (multi-intent detection)
- Ambiguous requests (semantic + context)
- Context-dependent requests (anaphora resolution)
