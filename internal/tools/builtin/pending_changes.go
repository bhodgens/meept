package builtin

import (
	"sync"
	"time"
)

// PendingChange represents a file modification awaiting acceptance.
type PendingChange struct {
	ID          string            `json:"id"`
	SessionID   string            `json:"session_id"`
	FilePath    string            `json:"file_path"`
	Original    string            `json:"original"`      // Original file content
	Modified    string            `json:"modified"`      // Modified content (preview)
	Diff        string            `json:"diff"`          // Unified diff preview
	CreatedAt   time.Time         `json:"created_at"`
	ExpiresAt   *time.Time        `json:"expires_at,omitempty"`
	Metadata    map[string]any    `json:"metadata,omitempty"`
}

// PendingChangesRegistry manages session-scoped pending changes.
type PendingChangesRegistry struct {
	mu       sync.RWMutex
	changes  map[string]*PendingChange // keyed by change ID
	sessions map[string][]string       // sessionID -> change IDs
}

// NewPendingChangesRegistry creates a new pending changes registry.
func NewPendingChangesRegistry() *PendingChangesRegistry {
	return &PendingChangesRegistry{
		changes:  make(map[string]*PendingChange),
		sessions: make(map[string][]string),
	}
}

// Add registers a new pending change.
func (r *PendingChangesRegistry) Add(change *PendingChange) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.changes[change.ID] = change
	r.sessions[change.SessionID] = append(r.sessions[change.SessionID], change.ID)
}

// Get retrieves a pending change by ID.
func (r *PendingChangesRegistry) Get(id string) (*PendingChange, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	change, ok := r.changes[id]
	return change, ok
}

// Remove removes a change by ID (after accept/reject).
func (r *PendingChangesRegistry) Remove(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	change, ok := r.changes[id]
	if !ok {
		return
	}

	// Remove from session tracking
	if change.SessionID != "" {
		sessionChanges := r.sessions[change.SessionID]
		for i, cid := range sessionChanges {
			if cid == id {
				r.sessions[change.SessionID] = append(sessionChanges[:i], sessionChanges[i+1:]...)
				break
			}
		}
	}

	delete(r.changes, id)
}

// GetBySession returns all pending changes for a session.
func (r *PendingChangesRegistry) GetBySession(sessionID string) []*PendingChange {
	r.mu.RLock()
	defer r.mu.RUnlock()

	changeIDs, ok := r.sessions[sessionID]
	if !ok {
		return nil
	}

	changes := make([]*PendingChange, 0, len(changeIDs))
	for _, id := range changeIDs {
		if change, ok := r.changes[id]; ok {
			changes = append(changes, change)
		}
	}
	return changes
}

// RemoveBySession removes all pending changes for a session (e.g., on session end).
func (r *PendingChangesRegistry) RemoveBySession(sessionID string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	changeIDs, ok := r.sessions[sessionID]
	if !ok {
		return
	}

	for _, id := range changeIDs {
		delete(r.changes, id)
	}
	delete(r.sessions, sessionID)
}

// Expire removes changes that have passed their expiration time.
func (r *PendingChangesRegistry) Expire() {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	toRemove := make([]string, 0)

	for id, change := range r.changes {
		if change.ExpiresAt != nil && now.After(*change.ExpiresAt) {
			toRemove = append(toRemove, id)
		}
	}

	for _, id := range toRemove {
		change := r.changes[id]
		// Remove from session tracking
		if change.SessionID != "" {
			sessionChanges := r.sessions[change.SessionID]
			for i, cid := range sessionChanges {
				if cid == id {
					r.sessions[change.SessionID] = append(sessionChanges[:i], sessionChanges[i+1:]...)
					break
				}
			}
		}
		delete(r.changes, id)
	}
}

// SetExpiry sets an expiration time for a change.
func (r *PendingChangesRegistry) SetExpiry(id string, expiresAt time.Time) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	change, ok := r.changes[id]
	if !ok {
		return false
	}

	change.ExpiresAt = &expiresAt
	return true
}
