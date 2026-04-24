# Phase 5: Context-Aware Classification

**Status:** Completed
**Priority:** Low (requires Phase 1-4)
**Estimated Effort:** 2-3 sprints

---

## Overview

Current dispatch classifies each request independently, ignoring conversation history. A request like "now do the same for logout" is ambiguous without context. This phase uses memory context and conversation history to improve classification accuracy.

**Current State (verified 2026-04-24):**
- All Phase 5 features implemented and tested
- Context-aware classification with session tracking
- Anaphora resolution for "do the same" type requests
- Semantic matching with embedding-based classification
- All tests passing

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
→ Memory: {last_intent: "debug", last_agent: "debugger"}

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

---

## Implementation Steps

### Step 1: Extend Memory Context Structure

**File:** `internal/agent/dispatcher.go` (NEW)

```go
// MemoryContext wraps memory results with conversation metadata.
type MemoryContext struct {
    // Results are the raw memory matches.
    Results []memory.MemoryResult `json:"results"`

    // LastIntent is the most recent intent from this session.
    LastIntent *Intent `json:"last_intent,omitempty"`

    // LastAgent is the most recent agent used.
    LastAgent string `json:"last_agent,omitempty"`

    // IntentCounts tracks intent frequency in this session.
    IntentCounts map[string]int `json:"intent_counts,omitempty"`
}

// In ClassifyAndRoute(), build context:
func (d *Dispatcher) buildMemoryContext(ctx context.Context, input string, sessionID string) *MemoryContext {
    ctx := &MemoryContext{
        IntentCounts: make(map[string]int),
    }

    // Search memory for this session
    if d.memoryMgr != nil {
        results, _ := d.memoryMgr.Search(ctx, memory.MemoryQuery{
            Query: input,
            Limit: 10,
            SessionID: sessionID,
        })
        ctx.Results = results
    }

    // Extract last intent from session memory
    if d.memvid != nil {
        episodic := d.memvid.WithZone("episodic")
        recent, _ := episodic.Search(ctx, sessionID, 5)

        for _, r := range recent {
            if intentType, ok := r.Memory.Metadata["intent_type"].(string); ok {
                ctx.LastIntent = &Intent{Type: intentType}
                ctx.IntentCounts[intentType]++
            }
            if agentID, ok := r.Memory.Metadata["agent_id"].(string); ok {
                ctx.LastAgent = agentID
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
    // ... existing classification chain ...

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
    }

    // Boost 2: Same agent as last request
    if memCtx.LastAgent != "" && memCtx.LastAgent == intent.AgentType {
        boost += 0.1
    }

    // Boost 3: Intent is common in this session
    if count, ok := memCtx.IntentCounts[intent.Type]; ok && count >= 2 {
        boost += 0.05 * float64(count)
    }

    // Boost 4: Input contains anaphora (refers to context)
    if hasAnaphora(input) && memCtx.LastIntent != nil {
        if intent.Type == memCtx.LastIntent.Type {
            boost += 0.2
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

**File:** `internal/agent/dispatcher.go`

```go
// resolveAnaphora replaces context references with actual content.
func (d *Dispatcher) resolveAnaphora(ctx context.Context, input string, memCtx *MemoryContext) string {
    if memCtx == nil || memCtx.LastIntent == nil {
        return input
    }

    lower := strings.ToLower(input)

    // Pattern: "do the same for X" → expand to last action + X
    if strings.Contains(lower, "do the same") {
        lastSummary := memCtx.LastIntent.Summary
        forMatch := regexp.MustCompile(`do the same for (.+)`)
        if match := forMatch.FindStringSubmatch(lower); match != nil {
            return fmt.Sprintf("%s for %s", lastSummary, match[1])
        }
    }

    return input
}
```

### Step 4: Add Session Tracker

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
    TotalRequests  int
}

// NewSessionTracker creates a new session tracker.
func NewSessionTracker(maxAge time.Duration) *SessionTracker {
    return &SessionTracker{
        sessions: make(map[string]*SessionState),
        maxAge:   maxAge,
    }
}

// RecordIntent logs an intent for a session.
func (t *SessionTracker) RecordIntent(sessionID string, intent *Intent, agentID string) {
    t.mu.Lock()
    defer t.mu.Unlock()

    state := t.getOrCreateSession(sessionID)
    state.IntentHistory = append(state.IntentHistory, intent)
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
state := d.sessionTracker.GetSession(sessionID)
memCtx.DominantIntent = state.GetDominantIntent()
```

---

## Deliverables

| Item | Description |
|------|-------------|
| `MemoryContext` struct | Extended with session metadata |
| `applyContextWeighting()` | Boost scores based on context |
| `resolveAnaphora()` | Expand context references |
| `SessionTracker` | Per-session intent history |

---

## Success Criteria

1. "Do the same" requests route correctly
2. Session continuity improves classification confidence
3. Anaphora resolution expands references

---

## Testing

```go
func TestContextWeighting(t *testing.T) {
    memCtx := &MemoryContext{
        LastIntent: &Intent{Type: "debug"},
        LastAgent: "debugger",
    }

    intent := &Intent{Type: "debug", AgentType: "debugger", Confidence: 0.7}
    boosted := applyContextWeighting(intent, memCtx, "fix this too")

    assert.Greater(t, boosted.Confidence, 0.7)  // Should be boosted
}
```

---

## Dependencies

- **Phase 1**: Need stats for tracking context effectiveness
- **Phase 4**: Semantic matching helps with anaphora

---

## Completion

Phase 5 completes the dispatcher enhancement roadmap. After this:

| Phase | Feature | Status |
|-------|---------|--------|
| 1 | Analytics | Full visibility into routing |
| 2 | Unified taxonomy | Single source of truth |
| 3 | Compound requests | Multi-intent detection |
| 4 | Semantic matching | Embedding-based classification |
| 5 | Context-aware | Session continuity |

The dispatcher now handles:
- Simple requests (via all classifiers)
- Compound requests (multi-intent detection)
- Ambiguous requests (semantic + context)
- Context-dependent requests (anaphora resolution)
