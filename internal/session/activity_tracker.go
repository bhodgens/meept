package session

import (
	"sync"
	"time"
)

// ActivityState tracks the most recent activity for a single session.
type ActivityState struct {
	SessionID string
	LastActivity time.Time
	ClientID string
}

// ActivityTracker tracks last-activity timestamps per session so callers
// can query which sessions have been active within a time window.
type ActivityTracker struct {
	mu       sync.RWMutex
	activity map[string]*ActivityState
}

// NewActivityTracker creates and returns a ready-to-use ActivityTracker.
func NewActivityTracker() *ActivityTracker {
	return &ActivityTracker{
		activity: make(map[string]*ActivityState),
	}
}

// RecordActivity records that a session was active.
func (t *ActivityTracker) RecordActivity(sessionID, clientID string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	state := t.activity[sessionID]
	if state == nil {
		state = &ActivityState{
			SessionID: sessionID,
		}
		t.activity[sessionID] = state
	}
	state.LastActivity = time.Now()
	state.ClientID = clientID
}

// GetActiveSessions returns session IDs whose last activity is within the
// provided window relative to the current time.
func (t *ActivityTracker) GetActiveSessions(window time.Duration) []string {
	t.mu.RLock()
	defer t.mu.RUnlock()

	now := time.Now()
	var ids []string
	for _, state := range t.activity {
		if now.Sub(state.LastActivity) <= window {
			ids = append(ids, state.SessionID)
		}
	}
	return ids
}

// HasRecentActivity reports whether the given session had activity within
// the provided window relative to the current time.
func (t *ActivityTracker) HasRecentActivity(sessionID string, window time.Duration) bool {
	t.mu.RLock()
	defer t.mu.RUnlock()

	state := t.activity[sessionID]
	if state == nil {
		return false
	}
	return time.Now().Sub(state.LastActivity) <= window
}
