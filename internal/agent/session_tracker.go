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

	if len(state.IntentHistory) > 20 {
		state.IntentHistory = state.IntentHistory[len(state.IntentHistory)-20:]
	}
}

// GetSession returns session state (read-only lookup, no cleanup).
func (t *SessionTracker) GetSession(sessionID string) *SessionState {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.sessions[sessionID]
}

// Cleanup removes expired sessions. Must be called with full Lock.
func (t *SessionTracker) Cleanup() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.cleanupExpired()
}

// GetDominantIntent returns the most frequent intent in session.
func (t *SessionTracker) GetDominantIntent(sessionID string) string {
	t.mu.RLock()
	state, ok := t.sessions[sessionID]
	if !ok || len(state.IntentHistory) == 0 {
		t.mu.RUnlock()
		return ""
	}
	// Copy intent types while holding lock
	types := make([]string, len(state.IntentHistory))
	for i, intent := range state.IntentHistory {
		types[i] = intent.Type
	}
	t.mu.RUnlock()

	counts := make(map[string]int)
	var maxIntent string
	maxCount := 0
	for _, typ := range types {
		counts[typ]++
		if counts[typ] > maxCount {
			maxCount = counts[typ]
			maxIntent = typ
		}
	}
	return maxIntent
}

// GetLastIntent returns a copy of the most recent intent.
func (t *SessionTracker) GetLastIntent(sessionID string) *Intent {
	t.mu.RLock()
	state, ok := t.sessions[sessionID]
	if !ok || len(state.IntentHistory) == 0 {
		t.mu.RUnlock()
		return nil
	}
	last := state.IntentHistory[len(state.IntentHistory)-1]
	cp := *last // copy
	t.mu.RUnlock()
	return &cp
}

// GetLastAgent returns the most recent agent type.
func (t *SessionTracker) GetLastAgent(sessionID string) string {
	t.mu.RLock()
	state, ok := t.sessions[sessionID]
	if !ok || len(state.IntentHistory) == 0 {
		t.mu.RUnlock()
		return ""
	}
	agentType := state.IntentHistory[len(state.IntentHistory)-1].AgentType
	t.mu.RUnlock()
	return agentType
}

// GetIntentCounts returns intent frequency counts.
func (t *SessionTracker) GetIntentCounts(sessionID string) map[string]int {
	t.mu.RLock()
	state, ok := t.sessions[sessionID]
	if !ok || len(state.IntentHistory) == 0 {
		t.mu.RUnlock()
		return make(map[string]int)
	}
	types := make([]string, len(state.IntentHistory))
	for i, intent := range state.IntentHistory {
		types[i] = intent.Type
	}
	t.mu.RUnlock()

	counts := make(map[string]int)
	for _, typ := range types {
		counts[typ]++
	}
	return counts
}

func (t *SessionTracker) getOrCreateSession(sessionID string) *SessionState {
	if state, ok := t.sessions[sessionID]; ok {
		return state
	}
	state := &SessionState{
		SessionID:      sessionID,
		CreatedAt:      time.Now(),
		LastActivityAt: time.Now(),
	}
	t.sessions[sessionID] = state
	return state
}

func (t *SessionTracker) cleanupExpired() {
	now := time.Now()
	for id, state := range t.sessions {
		if now.Sub(state.LastActivityAt) > t.maxAge {
			delete(t.sessions, id)
		}
	}
}
