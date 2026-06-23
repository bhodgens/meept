package session

import (
	"context"
	"time"
)

// DesignationHistoryEntry records a single designation transition for a session.
// The first transition (initial designation) has an empty FromStatus.
type DesignationHistoryEntry struct {
	ID         int64             `json:"id"`
	SessionID  string            `json:"session_id"`
	FromStatus DesignationStatus `json:"from_status"` // empty if first designation
	ToStatus   DesignationStatus `json:"to_status"`
	Reason     string            `json:"reason,omitempty"`
	Timestamp time.Time          `json:"timestamp"`
}

// DesignationHistoryStore persists designation transitions and supports listing
// the audit trail for a given session.
type DesignationHistoryStore interface {
	// Record persists a designation transition. If from is DesignationNone
	// (or empty), this represents the initial designation.
	Record(ctx context.Context, sessionID string, from, to DesignationStatus, reason string) error
	// List returns the designation history for a session, ordered oldest-first.
	List(ctx context.Context, sessionID string) ([]DesignationHistoryEntry, error)
}
