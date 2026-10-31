package activity

import (
	"context"
	"time"
)

// SessionReader is the narrow local seam over the session store (dispatch
// addendum 2026-10-30 #2): internal/session.Store is a wide interface with
// multiple implementations, so this package declares only the reads it
// needs instead of widening the repo-wide contract. *session.SQLiteStore
// satisfies it structurally via SessionStoreAdapter.
type SessionReader interface {
	List() ([]SessionRow, error)
	Messages(sessionID string, offset, limit int) ([]SessionMessage, error)
}

// TaskLister is the narrow read seam over the task registry/store (only the
// reads this package needs; the adapters in adapters.go satisfy it).
type TaskLister interface {
	ListTasks() ([]TaskRow, error)
}

// StepLister is the narrow read seam over the task step store (the adapter
// in adapters.go satisfies it).
type StepLister interface {
	ListSteps() ([]StepRow, error)
}

// ChainLister is the narrow read seam over the audit-log chain store (the
// adapter in adapters.go satisfies it).
type ChainLister interface {
	ListRecords(ctx context.Context, limit int) ([]ChainRecord, error)
}

// FindingLister is the narrow read seam over the employee audit-findings
// store (the adapter in adapters.go satisfies it).
type FindingLister interface {
	ListFindings(ctx context.Context, employeeID, severity string, since time.Time, limit int) ([]FindingRow, error)
}
