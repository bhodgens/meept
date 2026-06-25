package daemon

import (
	"context"
	"fmt"
	"time"

	"github.com/caimlas/meept/internal/employee"
)

// employeeServiceAdapter wraps *employee.Manager to satisfy the
// services.EmployeeManager interface. The adapter bridges the concrete
// *employee.Manager signatures (which return typed structs) to the
// any-typed interface methods. This is the composition point that
// avoids an import cycle: services never imports employee.
//
// See docs/superpowers/specs/2026-06-23-ai-employee-design.md (Phase 7,
// "Service layer").
type employeeServiceAdapter struct {
	m *employee.Manager
}

// Compile-time guard: employeeServiceAdapter must satisfy
// services.EmployeeManager. The blank assignment is intentionally
// omitted to avoid a daemon -> services cycle in the test; the daemon
// already imports services in daemon.go.

// ListEmployees wraps Manager.ListEmployees, converting []Employee to []any.
func (a employeeServiceAdapter) ListEmployees(ctx context.Context, statusFilter string) ([]any, error) {
	emps, err := a.m.ListEmployees(ctx, statusFilter)
	if err != nil {
		return nil, err
	}
	out := make([]any, len(emps))
	for i, e := range emps {
		out[i] = e
	}
	return out, nil
}

// GetEmployee wraps Manager.GetEmployee, converting *Employee to any.
func (a employeeServiceAdapter) GetEmployee(ctx context.Context, id string) (any, error) {
	return a.m.GetEmployee(ctx, id)
}

// Hire wraps Manager.Hire, type-asserting req to employee.HireRequest.
func (a employeeServiceAdapter) Hire(ctx context.Context, req any) (any, error) {
	hireReq, ok := req.(employee.HireRequest)
	if !ok {
		return nil, fmt.Errorf("hire: expected employee.HireRequest, got %T", req)
	}
	return a.m.Hire(ctx, hireReq)
}

// UpdateEmployee wraps Manager.UpdateEmployee, type-asserting req to
// employee.UpdateRequest.
func (a employeeServiceAdapter) UpdateEmployee(ctx context.Context, req any) (any, error) {
	updateReq, ok := req.(employee.UpdateRequest)
	if !ok {
		return nil, fmt.Errorf("update: expected employee.UpdateRequest, got %T", req)
	}
	return a.m.UpdateEmployee(ctx, updateReq)
}

// Retire wraps Manager.Retire.
func (a employeeServiceAdapter) Retire(ctx context.Context, id string) error {
	return a.m.Retire(ctx, id)
}

// Pause wraps Manager.Pause.
func (a employeeServiceAdapter) Pause(ctx context.Context, id string) error {
	return a.m.Pause(ctx, id)
}

// Resume wraps Manager.Resume.
func (a employeeServiceAdapter) Resume(ctx context.Context, id string) error {
	return a.m.Resume(ctx, id)
}

// Trigger wraps Manager.Trigger, converting *TriggerResult to any.
func (a employeeServiceAdapter) Trigger(ctx context.Context, id string, payload map[string]any) (any, error) {
	return a.m.Trigger(ctx, id, payload)
}

// AmendConstitution wraps Manager.AmendConstitution, building an
// employee.AmendRequest from the individual arguments.
func (a employeeServiceAdapter) AmendConstitution(ctx context.Context, employeeID string, fields map[string]any, reason string) (string, error) {
	req := employee.AmendRequest{
		EmployeeID: employeeID,
		Fields:     fields,
		Reason:     reason,
		By:         "user", // The service layer is always called on behalf of the operator.
	}
	return a.m.AmendConstitution(ctx, req)
}

// ListGoals wraps Manager.ListGoals, converting []*Goal to []any.
func (a employeeServiceAdapter) ListGoals(ctx context.Context, employeeID string) ([]any, error) {
	goals, err := a.m.ListGoals(ctx, employeeID)
	if err != nil {
		return nil, err
	}
	out := make([]any, len(goals))
	for i, g := range goals {
		out[i] = g
	}
	return out, nil
}

// GetGoal wraps Manager.GetGoal, converting *Goal to any.
func (a employeeServiceAdapter) GetGoal(ctx context.Context, id string) (any, error) {
	return a.m.GetGoal(ctx, id)
}

// ApprovePlan wraps Manager.ApprovePlan.
func (a employeeServiceAdapter) ApprovePlan(ctx context.Context, goalID, planID, reason string) error {
	return a.m.ApprovePlan(ctx, goalID, planID, reason)
}

// RejectPlan wraps Manager.RejectPlan.
func (a employeeServiceAdapter) RejectPlan(ctx context.Context, goalID, planID, reason string) error {
	return a.m.RejectPlan(ctx, goalID, planID, reason)
}

// ListAuditFindings wraps Manager.ListAuditFindings, building an
// employee.AuditQuery from the individual arguments and converting
// []AuditFinding to []any.
func (a employeeServiceAdapter) ListAuditFindings(ctx context.Context, employeeID string, since time.Duration, severity string) ([]any, error) {
	q := employee.AuditQuery{
		EmployeeID: employeeID,
		Since:      since,
		Severity:   severity,
	}
	findings, err := a.m.ListAuditFindings(ctx, q)
	if err != nil {
		return nil, err
	}
	out := make([]any, len(findings))
	for i, f := range findings {
		out[i] = f
	}
	return out, nil
}

// ResolveAuditFinding wraps Manager.ResolveAuditFinding.
func (a employeeServiceAdapter) ResolveAuditFinding(ctx context.Context, findingID, resolution, note string) error {
	return a.m.ResolveAuditFinding(ctx, findingID, resolution, note)
}

// Migrate wraps Manager.Migrate, converting []MigrationProposal to []any.
func (a employeeServiceAdapter) Migrate(ctx context.Context) ([]any, error) {
	proposals, err := a.m.Migrate(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]any, len(proposals))
	for i, p := range proposals {
		out[i] = p
	}
	return out, nil
}

// ApplyMigration wraps Manager.ApplyMigration, converting
// *MigrationApplyResult to any.
func (a employeeServiceAdapter) ApplyMigration(ctx context.Context, botID string) (any, error) {
	return a.m.ApplyMigration(ctx, botID)
}
