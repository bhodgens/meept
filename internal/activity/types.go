package activity

import (
	"time"
)

// ActivityRecord is the common read shape every activity silo maps into
// (master.md C1 — byte-identical contract; do not edit field order, types,
// or json tags without updating the master contract).
type ActivityRecord struct {
	Kind      string            `json:"kind"` // "task_step"|"session_message"|"audit_record"|"bus_event"
	At        time.Time         `json:"at"`   // RFC3339; primary sort key
	AgentID   string            `json:"agent_id,omitempty"`
	SessionID string            `json:"session_id,omitempty"`
	TaskID    string            `json:"task_id,omitempty"`
	StepID    string            `json:"step_id,omitempty"`
	PlanID    string            `json:"plan_id,omitempty"` // resolved via plans.task_id walk
	State     string            `json:"state,omitempty"`
	Severity  string            `json:"severity,omitempty"` // info|warning|critical (findings/audit)
	Summary   string            `json:"summary"`            // one-line, envelope-stripped, <=200 runes
	Detail    map[string]string `json:"detail,omitempty"`
}

// Record kinds emitted by the aggregator (bus_event is owned by the ring
// buffer leaf, not this package).
const (
	KindTaskStep       = "task_step"
	KindSessionMessage = "session_message"
	KindAuditRecord    = "audit_record"
)

// Sort modes for Query.Sort. An empty Sort means chronological.
const (
	SortChrono = "chrono"
	SortTask   = "task"
	SortPlan   = "plan"
)

// maxSummaryRunes bounds every Summary to the C1 contract (<=200 runes).
const maxSummaryRunes = 200
