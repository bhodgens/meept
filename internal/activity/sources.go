package activity

import (
	"time"
)

// Query is one aggregator read. Zero Since/Until mean unbounded; empty Sort
// is chronological; Limit/Offset page the FILTERED, SORTED result while the
// returned total stays the pre-pagination count.
type Query struct {
	Since     time.Time
	Until     time.Time
	Sort      string `json:"sort,omitempty"` // "chrono"|"task"|"plan" ("" = chrono)
	TaskID    string `json:"task_id,omitempty"`
	PlanID    string `json:"plan_id,omitempty"`
	AgentID   string `json:"agent_id,omitempty"`
	SessionID string `json:"session_id,omitempty"`
	Kinds     []string
	Limit     int
	Offset    int
}

// TaskRow is the storage-agnostic task shape (from internal/task).
type TaskRow struct {
	ID            string
	Name          string
	AssignedAgent string
	CreatedAt     time.Time
}

// StepRow is the storage-agnostic task-step shape (from internal/task).
type StepRow struct {
	ID          string
	TaskID      string
	Description string
	AgentID     string
	State       string
	Result      string
	UpdatedAt   time.Time
}

// SessionRow is the storage-agnostic session shape (from internal/session).
type SessionRow struct {
	ID         string
	EmployeeID string
}

// SessionMessage is the storage-agnostic message shape (from
// internal/session).
type SessionMessage struct {
	SessionID string
	Role      string
	Content   string
	AgentID   string
	Timestamp time.Time
}

// ChainRecord is the storage-agnostic audit-chain record shape (from
// internal/auditlog).
type ChainRecord struct {
	Seq        uint64
	Type       string
	EmployeeID string
	Payload    map[string]string
	At         time.Time
}

// FindingRow is the storage-agnostic audit-finding shape (from
// internal/employee).
type FindingRow struct {
	ID           string
	EmployeeID   string
	PlanID       string
	Severity     string
	ViolatedRule string
	Evidence     string
	Resolved     bool
	DetectedAt   time.Time
}
