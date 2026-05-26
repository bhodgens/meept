package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/caimlas/meept/internal/memory/memvid"
)

// SessionTracker tracks conversation patterns per session.
type SessionTracker struct {
	mu                      sync.RWMutex
	sessions                map[string]*SessionState
	maxAge                  time.Duration
	memvidClient            *memvid.Client
	sessionIdleTriggerHours int
	stopCh                  chan struct{} // Used to signal background goroutine to stop
	stopOnce                sync.Once     // Ensures Stop is only called once
	logger                  *slog.Logger
}

// SessionState holds state for a single conversation session.
type SessionState struct {
	SessionID      string
	CreatedAt      time.Time
	LastActivityAt time.Time
	IntentHistory  []*Intent
	TotalRequests  int
	Metrics        SessionMetrics
	Persisted      bool // true if session has been persisted to memvid
}

// SessionMetrics holds session performance metrics.
type SessionMetrics struct {
	Duration      time.Duration
	Iterations    int
	TokenUsage    int
	ToolCalls     int
	AgentSwitches int
	Errors        int
	Revisions     int
}

// SessionTrackerConfig holds configuration for SessionTracker.
type SessionTrackerConfig struct {
	MaxAge                  time.Duration
	SessionIdleTriggerHours int
	MemvidClient            *memvid.Client
}

// NewSessionTracker creates a new session tracker.
func NewSessionTracker(maxAge time.Duration) *SessionTracker {
	return &SessionTracker{
		sessions: make(map[string]*SessionState),
		maxAge:   maxAge,
		stopCh:   make(chan struct{}),
		logger:   slog.Default(),
	}
}

// NewSessionTrackerWithConfig creates a new session tracker with memvid persistence.
func NewSessionTrackerWithConfig(cfg SessionTrackerConfig) *SessionTracker {
	return &SessionTracker{
		sessions:                make(map[string]*SessionState),
		maxAge:                  cfg.MaxAge,
		memvidClient:            cfg.MemvidClient,
		sessionIdleTriggerHours: cfg.SessionIdleTriggerHours,
		stopCh:                  make(chan struct{}),
		logger:                  slog.Default(),
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

// PersistIdleSessions persists sessions that have been idle for the configured threshold.
// Returns the last persistence error encountered, or nil if all sessions persisted
// successfully (or were not idle / had already been persisted).
// AGENT-20 FIX: Previously returned nil even when persistSession failed. Now tracks
// the last error and returns it after the loop. Callsites that need per-session error
// reporting should inspect the error return value.
func (t *SessionTracker) PersistIdleSessions(ctx context.Context) error {
	if t.memvidClient == nil {
		return nil // No memvid client configured
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	idleThreshold := time.Duration(t.sessionIdleTriggerHours) * time.Hour
	now := time.Now()
	var lastErr error

	for _, state := range t.sessions {
		if state.Persisted {
			continue // Already persisted
		}

		if now.Sub(state.LastActivityAt) > idleThreshold {
			if err := t.persistSession(ctx, state); err != nil {
				t.logger.Error("Failed to persist idle session",
					"session_id", state.SessionID,
					"error", err,
				)
				lastErr = fmt.Errorf("session %s: %w", state.SessionID, err)
				continue
			}
			state.Persisted = true
		}
	}

	return lastErr
}

// persistSession persists a single session to memvid.
func (t *SessionTracker) persistSession(ctx context.Context, state *SessionState) error {
	// Create session metadata
	metadata := map[string]any{
		"session_id":       state.SessionID,
		"start_time":       state.CreatedAt.Format(time.RFC3339),
		"end_time":         state.LastActivityAt.Format(time.RFC3339),
		"duration_seconds": state.LastActivityAt.Sub(state.CreatedAt).Seconds(),
		"total_requests":   state.TotalRequests,
		"intents":          t.extractIntents(state.IntentHistory),
		KeyAgentID:         t.getLastAgentFromIntent(state.IntentHistory),
		"outcome":          t.determineOutcome(state),
		"iterations":       state.Metrics.Iterations,
		KeyTokenUsage:      state.Metrics.TokenUsage,
		"tool_calls":       state.Metrics.ToolCalls,
		"agent_switches":   state.Metrics.AgentSwitches,
		"errors":           state.Metrics.Errors,
		"revisions":        state.Metrics.Revisions,
	}

	// Store session summary
	summary := t.generateSessionSummary(state)
	_, err := t.memvidClient.StoreWithZone(ctx, summary, "sessions", metadata)
	return err
}

// extractIntents extracts intent types from intent history.
func (t *SessionTracker) extractIntents(intents []*Intent) []string {
	result := make([]string, len(intents))
	for i, intent := range intents {
		if intent != nil {
			result[i] = intent.Type
		}
	}
	return result
}

// getLastAgentFromIntent extracts the agent ID from the last intent.
func (t *SessionTracker) getLastAgentFromIntent(intents []*Intent) string {
	if len(intents) == 0 {
		return ""
	}
	last := intents[len(intents)-1]
	if last != nil {
		return last.AgentType
	}
	return ""
}

// determineOutcome determines the session outcome.
func (t *SessionTracker) determineOutcome(state *SessionState) string {
	if state.Metrics.Errors > 3 {
		return "failed"
	}
	if state.Metrics.Revisions > 5 {
		return "partial"
	}
	return ReportStatusCompleted
}

// generateSessionSummary generates a text summary of the session.
func (t *SessionTracker) generateSessionSummary(state *SessionState) string {
	data, _ := json.Marshal(map[string]any{
		"session_id": state.SessionID,
		"duration":   state.LastActivityAt.Sub(state.CreatedAt).String(),
		"requests":   state.TotalRequests,
		"intents":    t.extractIntents(state.IntentHistory),
		"outcome":    t.determineOutcome(state),
	})
	return string(data)
}

// RecordMetrics records session metrics.
func (t *SessionTracker) RecordMetrics(sessionID string, metrics SessionMetrics) {
	t.mu.Lock()
	defer t.mu.Unlock()

	state := t.getOrCreateSession(sessionID)
	state.Metrics = metrics
}

// GetIdleSessions returns sessions that have been idle for the specified duration.
func (t *SessionTracker) GetIdleSessions(idleDuration time.Duration) []*SessionState {
	t.mu.RLock()
	defer t.mu.RUnlock()

	now := time.Now()
	var idle []*SessionState

	for _, state := range t.sessions {
		if now.Sub(state.LastActivityAt) > idleDuration {
			idle = append(idle, state)
		}
	}

	return idle
}

// StartBackgroundPersistence starts a background goroutine that persists idle sessions.
// The goroutine runs every hour and persists sessions idle for > sessionIdleTriggerHours.
// Call StopBackgroundPersistence() to stop the goroutine.
func (t *SessionTracker) StartBackgroundPersistence(ctx context.Context) {
	if t.memvidClient == nil {
		return // No memvid client configured
	}

	pollInterval := 1 * time.Hour
	ticker := time.NewTicker(pollInterval)

	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.stopCh:
				return
			case <-ticker.C:
				if err := t.PersistIdleSessions(ctx); err != nil {
					t.logger.Error("Background persistence failed", "error", err)
				}
			}
		}
	}()
}

// StopBackgroundPersistence stops the background persistence goroutine.
// Safe to call multiple times; only the first call has any effect.
func (t *SessionTracker) StopBackgroundPersistence() {
	t.stopOnce.Do(func() {
		close(t.stopCh)
	})
}
