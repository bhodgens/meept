package services

import (
	"sync"
	"time"
)

// PushEntry represents a single push notification event.
type PushEntry struct {
	ID        string    `json:"id"`
	SessionID string    `json:"session_id"`
	Source    string    `json:"source"`
	Type      PushType  `json:"type"`
	Content   string    `json:"content"`
	Priority  PushPriority `json:"priority"`
	Timestamp time.Time `json:"timestamp"`
	Delivered bool      `json:"delivered"`
}

// PushHistory tracks push notification history with query capabilities.
type PushHistory struct {
	entries []PushEntry
	mu      sync.RWMutex
	maxSize int
}

// NewPushHistory creates a push history tracker.
func NewPushHistory(maxSize int) *PushHistory {
	if maxSize <= 0 {
		maxSize = 1000 // default max entries
	}
	return &PushHistory{
		entries: make([]PushEntry, 0, maxSize),
		maxSize: maxSize,
	}
}

// Record adds a push notification to history.
func (h *PushHistory) Record(entry PushEntry) {
	h.mu.Lock()
	defer h.mu.Unlock()

	// Add new entry
	h.entries = append(h.entries, entry)

	// Trim if exceeds max size
	if len(h.entries) > h.maxSize {
		h.entries = h.entries[1:]
	}
}

// Query returns recent push entries for a session.
func (h *PushHistory) Query(sessionID string, limit int) []PushEntry {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if limit <= 0 {
		limit = 50
	}

	var result []PushEntry
	count := 0

	// Iterate from most recent
	for i := len(h.entries) - 1; i >= 0 && count < limit; i-- {
		if sessionID == "" || h.entries[i].SessionID == sessionID {
			result = append(result, h.entries[i])
			count++
		}
	}

	return result
}

// QueryAll returns recent push entries (no session filter).
func (h *PushHistory) QueryAll(limit int) []PushEntry {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if limit <= 0 {
		limit = 50
	}

	start := len(h.entries) - limit
	if start < 0 {
		start = 0
	}

	result := make([]PushEntry, len(h.entries)-start)
	copy(result, h.entries[start:])
	return result
}

// Clear removes all history entries.
func (h *PushHistory) Clear() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.entries = h.entries[:0]
}
