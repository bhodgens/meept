package activity

import (
	"context"
	"time"

	"github.com/caimlas/meept/internal/auditlog"
	"github.com/caimlas/meept/internal/employee"
	"github.com/caimlas/meept/internal/session"
	"github.com/caimlas/meept/internal/task"
)

// Adapters map the concrete store types onto this package's narrow read
// seams. They are pure reads — no write path exists here.

// unboundedReadLimit is what the adapters pass where the underlying store
// treats LIMIT 0 as SQL "zero rows" (task.List, session GetMessages). The
// aggregator's own reads are unbounded by contract.
const unboundedReadLimit = 1_000_000

// TaskRegistryAdapter adapts *task.Registry (its Store() list read).
type TaskRegistryAdapter struct{ r *task.Registry }

// NewTaskRegistryAdapter wraps a task registry.
func NewTaskRegistryAdapter(r *task.Registry) TaskLister { return TaskRegistryAdapter{r: r} }

// ListTasks implements TaskLister.
func (a TaskRegistryAdapter) ListTasks() ([]TaskRow, error) {
	tasks, err := a.r.Store().List(nil, unboundedReadLimit)
	if err != nil {
		return nil, err
	}
	rows := make([]TaskRow, 0, len(tasks))
	for _, t := range tasks {
		rows = append(rows, TaskRow{
			ID:            t.ID,
			Name:          t.Name,
			AssignedAgent: t.AssignedAgent,
			CreatedAt:     t.CreatedAt,
		})
	}
	return rows, nil
}

// TaskStoreAdapter adapts *task.Store directly.
type TaskStoreAdapter struct{ s *task.Store }

// NewTaskStoreAdapter wraps a task store.
func NewTaskStoreAdapter(s *task.Store) TaskLister { return TaskStoreAdapter{s: s} }

// ListTasks implements TaskLister.
func (a TaskStoreAdapter) ListTasks() ([]TaskRow, error) {
	tasks, err := a.s.List(nil, unboundedReadLimit)
	if err != nil {
		return nil, err
	}
	rows := make([]TaskRow, 0, len(tasks))
	for _, t := range tasks {
		rows = append(rows, TaskRow{
			ID:            t.ID,
			Name:          t.Name,
			AssignedAgent: t.AssignedAgent,
			CreatedAt:     t.CreatedAt,
		})
	}
	return rows, nil
}

// StepStoreAdapter adapts *task.StepStore.
type StepStoreAdapter struct{ s *task.StepStore }

// NewStepStoreAdapter wraps a task step store.
func NewStepStoreAdapter(s *task.StepStore) StepLister { return StepStoreAdapter{s: s} }

// ListSteps implements StepLister. A nil taskID list means "every step":
// the step store's per-task reads are walked over the task registry's task
// list by the aggregator's callers when needed; here we read per known task
// via the store's own DB-less listing — ListSteps walks ListStepsAll which
// the step store exposes as ListAllSteps.
func (a StepStoreAdapter) ListSteps() ([]StepRow, error) {
	steps, err := a.s.ListAllSteps()
	if err != nil {
		return nil, err
	}
	rows := make([]StepRow, 0, len(steps))
	for _, s := range steps {
		rows = append(rows, StepRow{
			ID:          s.ID,
			TaskID:      s.TaskID,
			Description: s.Description,
			AgentID:     s.AgentID,
			State:       string(s.State),
			Result:      s.Result,
			UpdatedAt:   s.UpdatedAt,
		})
	}
	return rows, nil
}

// SessionStoreAdapter adapts a session store onto the SessionReader seam.
// The contract hands this package a session.Store (interface); the adapter
// accepts the interface and holds the concrete reads it needs.
type SessionStoreAdapter struct{ s session.Store }

// NewSessionStoreAdapter wraps any session.Store implementation (today
// *session.SQLiteStore; the interface shape is the contract's handle type).
func NewSessionStoreAdapter(s session.Store) SessionReader {
	return SessionStoreAdapter{s: s}
}

// List implements SessionReader.
func (a SessionStoreAdapter) List() ([]SessionRow, error) {
	sessions, err := a.s.List()
	if err != nil {
		return nil, err
	}
	rows := make([]SessionRow, 0, len(sessions))
	for _, s := range sessions {
		rows = append(rows, SessionRow{ID: s.ID, EmployeeID: s.EmployeeID})
	}
	return rows, nil
}

// Messages implements SessionReader.
func (a SessionStoreAdapter) Messages(sessionID string, offset, limit int) ([]SessionMessage, error) {
	if limit <= 0 {
		limit = unboundedReadLimit
	}
	msgs, err := a.s.GetMessages(sessionID, offset, limit)
	if err != nil {
		return nil, err
	}
	rows := make([]SessionMessage, 0, len(msgs))
	for _, m := range msgs {
		rows = append(rows, SessionMessage{
			SessionID: m.SessionID,
			Role:      m.Role,
			Content:   m.Content,
			AgentID:   m.AgentID,
			Timestamp: m.Timestamp,
		})
	}
	return rows, nil
}

// ChainStoreAdapter adapts *auditlog.Store onto the ChainLister seam.
type ChainStoreAdapter struct{ s *auditlog.Store }

// NewChainStoreAdapter wraps an auditlog store.
func NewChainStoreAdapter(s *auditlog.Store) ChainLister { return ChainStoreAdapter{s: s} }

// ListRecords implements ChainLister (limit <= 0 = unbounded).
func (a ChainStoreAdapter) ListRecords(ctx context.Context, limit int) ([]ChainRecord, error) {
	recs, err := a.s.List(ctx, limit)
	if err != nil {
		return nil, err
	}
	rows := make([]ChainRecord, 0, len(recs))
	for _, r := range recs {
		payload := make(map[string]string, len(r.Payload))
		for k, v := range r.Payload {
			if s, ok := v.(string); ok {
				payload[k] = s
			}
		}
		rows = append(rows, ChainRecord{
			Seq:        r.Seq,
			Type:       r.Type,
			EmployeeID: r.EmployeeID,
			Payload:    payload,
			At:         r.At,
		})
	}
	return rows, nil
}

// AuditStoreAdapter adapts *employee.AuditStore onto the FindingLister
// seam (dispatch addendum 2026-10-30 #3: since arrives as a timestamp, not
// a duration).
type AuditStoreAdapter struct{ s *employee.AuditStore }

// NewAuditStoreAdapter wraps an employee audit store.
func NewAuditStoreAdapter(s *employee.AuditStore) FindingLister {
	return AuditStoreAdapter{s: s}
}

// ListFindings implements FindingLister.
func (a AuditStoreAdapter) ListFindings(ctx context.Context, employeeID, severity string, since time.Time, limit int) ([]FindingRow, error) {
	findings, err := a.s.List(ctx, employee.AuditListFilter{
		EmployeeID: employeeID,
		Severity:   severity,
		Since:      since,
		Limit:      limit,
	})
	if err != nil {
		return nil, err
	}
	rows := make([]FindingRow, 0, len(findings))
	for _, f := range findings {
		rows = append(rows, FindingRow{
			ID:           f.ID,
			EmployeeID:   f.EmployeeID,
			PlanID:       f.PlanID,
			Severity:     string(f.Severity),
			ViolatedRule: f.ViolatedRule,
			Evidence:     f.Evidence,
			Resolved:     f.ResolvedAt != nil,
			DetectedAt:   f.DetectedAt,
		})
	}
	return rows, nil
}
