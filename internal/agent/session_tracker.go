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

// GetSession returns session state.
func (t *SessionTracker) GetSession(sessionID string) *SessionState {
	t.mu.RLock()
	defer t.mu.RUnlock()
	t.cleanupExpired()
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

// GetLastIntent returns the most recent intent.
func (t *SessionTracker) GetLastIntent(sessionID string) *Intent {
	state := t.GetSession(sessionID)
	if state == nil || len(state.IntentHistory) == 0 {
		return nil
	}
	return state.IntentHistory[len(state.IntentHistory)-1]
}

// GetLastAgent returns the most recent agent.
func (t *SessionTracker) GetLastAgent(sessionID string) string {
	state := t.GetSession(sessionID)
	if state == nil || len(state.IntentHistory) == 0 {
		return ""
	}
	return state.IntentHistory[len(state.IntentHistory)-1].AgentType
}

// GetIntentCounts returns intent frequency counts.
func (t *SessionTracker) GetIntentCounts(sessionID string) map[string]int {
	state := t.GetSession(sessionID)
	if state == nil {
		return make(map[string]int)
	}

	counts := make(map[string]int)
	for _, intent := range state.IntentHistory {
		counts[intent.Type]++
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
