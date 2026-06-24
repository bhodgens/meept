package services

import (
	"context"
	"log/slog"
	"time"
)

// EmployeeManager is the interface satisfied by employee.Manager. Defined
// locally to avoid an import cycle: services -> employee -> bot -> comm/http
// -> services. The interface mirrors the subset of employee.Manager methods
// needed by the service layer; see internal/employee/manager.go for the
// concrete implementation.
//
// The concrete *employee.Manager is wired by the daemon via NewEmployeeService
// at composition time; the services package never imports employee directly.
type EmployeeManager interface {
	ListEmployees(ctx context.Context, statusFilter string) ([]any, error)
	GetEmployee(ctx context.Context, id string) (any, error)
	Hire(ctx context.Context, req any) (any, error)
	UpdateEmployee(ctx context.Context, req any) (any, error)
	Retire(ctx context.Context, id string) error
	Pause(ctx context.Context, id string) error
	Resume(ctx context.Context, id string) error
	Trigger(ctx context.Context, id string, payload map[string]any) (any, error)
	AmendConstitution(ctx context.Context, employeeID string, fields map[string]any, reason string) (string, error)
	ListGoals(ctx context.Context, employeeID string) ([]any, error)
	GetGoal(ctx context.Context, id string) (any, error)
	ApprovePlan(ctx context.Context, goalID, planID, reason string) error
	RejectPlan(ctx context.Context, goalID, planID, reason string) error
	ListAuditFindings(ctx context.Context, employeeID string, since time.Duration, severity string) ([]any, error)
	ResolveAuditFinding(ctx context.Context, findingID, resolution, note string) error
	Migrate(ctx context.Context) ([]any, error)
	ApplyMigration(ctx context.Context, botID string) (any, error)
}

// EmployeeService handles AI Employee lifecycle operations, wrapping the
// EmployeeManager interface. It follows the same pattern as PlanService: a
// thin service layer that both RPC and HTTP transports call into, so business
// logic stays in one place.
//
// See docs/superpowers/specs/2026-06-23-ai-employee-design.md (Phase 7).
type EmployeeService struct {
	manager EmployeeManager
	logger  *slog.Logger
}

// NewEmployeeService creates an EmployeeService. manager may be nil during
// partial rollout; methods return ErrUnavailable in that case.
func NewEmployeeService(m EmployeeManager) *EmployeeService {
	return &EmployeeService{
		manager: m,
		logger:  slog.Default(),
	}
}

// SetLogger replaces the default logger. Nil is ignored (typed-nil guard per
// CLAUDE.md setter rules).
func (s *EmployeeService) SetLogger(l *slog.Logger) {
	if l == nil {
		return
	}
	s.logger = l
}

// Manager returns the underlying EmployeeManager, or nil when not configured.
// Callers must nil-check before use; the service deliberately does not panic
// so that unwired daemons degrade gracefully.
func (s *EmployeeService) Manager() EmployeeManager {
	return s.manager
}

// List lists employees, optionally filtered by status string ("running",
// "paused", ...). Empty filter returns all employees.
func (s *EmployeeService) List(ctx context.Context, statusFilter string) ([]any, error) {
	if s.manager == nil {
		return nil, wrapError("employee", "List", ErrUnavailable)
	}
	emps, err := s.manager.ListEmployees(ctx, statusFilter)
	if err != nil {
		return nil, wrapError("employee", "List", err)
	}
	return emps, nil
}

// Get retrieves a single employee by ID.
func (s *EmployeeService) Get(ctx context.Context, id string) (any, error) {
	if id == "" {
		return nil, wrapError("employee", "Get", ErrInvalidInput)
	}
	if s.manager == nil {
		return nil, wrapError("employee", "Get", ErrUnavailable)
	}
	emp, err := s.manager.GetEmployee(ctx, id)
	if err != nil {
		return nil, wrapError("employee", "Get", err)
	}
	return emp, nil
}

// Create validates and creates a new employee.
func (s *EmployeeService) Create(ctx context.Context, req any) (any, error) {
	if s.manager == nil {
		return nil, wrapError("employee", "Create", ErrUnavailable)
	}
	emp, err := s.manager.Hire(ctx, req)
	if err != nil {
		return nil, wrapError("employee", "Create", err)
	}
	return emp, nil
}

// Update mutates an existing employee's non-constitution fields.
func (s *EmployeeService) Update(ctx context.Context, req any) (any, error) {
	if s.manager == nil {
		return nil, wrapError("employee", "Update", ErrUnavailable)
	}
	emp, err := s.manager.UpdateEmployee(ctx, req)
	if err != nil {
		return nil, wrapError("employee", "Update", err)
	}
	return emp, nil
}

// Delete retires (soft-deletes) an employee.
func (s *EmployeeService) Delete(ctx context.Context, id string) error {
	if id == "" {
		return wrapError("employee", "Delete", ErrInvalidInput)
	}
	if s.manager == nil {
		return wrapError("employee", "Delete", ErrUnavailable)
	}
	if err := s.manager.Retire(ctx, id); err != nil {
		return wrapError("employee", "Delete", err)
	}
	return nil
}

// Pause disables an employee without removing it.
func (s *EmployeeService) Pause(ctx context.Context, id string) error {
	if id == "" {
		return wrapError("employee", "Pause", ErrInvalidInput)
	}
	if s.manager == nil {
		return wrapError("employee", "Pause", ErrUnavailable)
	}
	if err := s.manager.Pause(ctx, id); err != nil {
		return wrapError("employee", "Pause", err)
	}
	return nil
}

// Resume re-enables a paused employee.
func (s *EmployeeService) Resume(ctx context.Context, id string) error {
	if id == "" {
		return wrapError("employee", "Resume", ErrInvalidInput)
	}
	if s.manager == nil {
		return wrapError("employee", "Resume", ErrUnavailable)
	}
	if err := s.manager.Resume(ctx, id); err != nil {
		return wrapError("employee", "Resume", err)
	}
	return nil
}

// Trigger programmatically invokes an employee.
func (s *EmployeeService) Trigger(ctx context.Context, id string, payload map[string]any) (any, error) {
	if id == "" {
		return nil, wrapError("employee", "Trigger", ErrInvalidInput)
	}
	if s.manager == nil {
		return nil, wrapError("employee", "Trigger", ErrUnavailable)
	}
	result, err := s.manager.Trigger(ctx, id, payload)
	if err != nil {
		return nil, wrapError("employee", "Trigger", err)
	}
	return result, nil
}

// Amend proposes a constitution amendment, routing through Plan signoff.
// Returns the created Plan ID.
func (s *EmployeeService) Amend(ctx context.Context, employeeID string, fields map[string]any, reason string) (string, error) {
	if employeeID == "" {
		return "", wrapError("employee", "Amend", ErrInvalidInput)
	}
	if s.manager == nil {
		return "", wrapError("employee", "Amend", ErrUnavailable)
	}
	planID, err := s.manager.AmendConstitution(ctx, employeeID, fields, reason)
	if err != nil {
		return "", wrapError("employee", "Amend", err)
	}
	return planID, nil
}

// ListGoals lists goals, optionally filtered by employeeID.
func (s *EmployeeService) ListGoals(ctx context.Context, employeeID string) ([]any, error) {
	if s.manager == nil {
		return nil, wrapError("employee", "ListGoals", ErrUnavailable)
	}
	goals, err := s.manager.ListGoals(ctx, employeeID)
	if err != nil {
		return nil, wrapError("employee", "ListGoals", err)
	}
	return goals, nil
}

// GetGoal retrieves a goal by ID.
func (s *EmployeeService) GetGoal(ctx context.Context, id string) (any, error) {
	if id == "" {
		return nil, wrapError("employee", "GetGoal", ErrInvalidInput)
	}
	if s.manager == nil {
		return nil, wrapError("employee", "GetGoal", ErrUnavailable)
	}
	g, err := s.manager.GetGoal(ctx, id)
	if err != nil {
		return nil, wrapError("employee", "GetGoal", err)
	}
	return g, nil
}

// ApprovePlan signs off on a pending plan for a goal.
func (s *EmployeeService) ApprovePlan(ctx context.Context, goalID, planID, reason string) error {
	if goalID == "" || planID == "" {
		return wrapError("employee", "ApprovePlan", ErrInvalidInput)
	}
	if s.manager == nil {
		return wrapError("employee", "ApprovePlan", ErrUnavailable)
	}
	if err := s.manager.ApprovePlan(ctx, goalID, planID, reason); err != nil {
		return wrapError("employee", "ApprovePlan", err)
	}
	return nil
}

// RejectPlan rejects a pending plan for a goal.
func (s *EmployeeService) RejectPlan(ctx context.Context, goalID, planID, reason string) error {
	if goalID == "" || planID == "" {
		return wrapError("employee", "RejectPlan", ErrInvalidInput)
	}
	if s.manager == nil {
		return wrapError("employee", "RejectPlan", ErrUnavailable)
	}
	if err := s.manager.RejectPlan(ctx, goalID, planID, reason); err != nil {
		return wrapError("employee", "RejectPlan", err)
	}
	return nil
}

// ListAuditFindings lists findings for an employee, optionally filtered.
func (s *EmployeeService) ListAuditFindings(ctx context.Context, employeeID string, since time.Duration, severity string) ([]any, error) {
	if employeeID == "" {
		return nil, wrapError("employee", "ListAuditFindings", ErrInvalidInput)
	}
	if s.manager == nil {
		return nil, wrapError("employee", "ListAuditFindings", ErrUnavailable)
	}
	findings, err := s.manager.ListAuditFindings(ctx, employeeID, since, severity)
	if err != nil {
		return nil, wrapError("employee", "ListAuditFindings", err)
	}
	return findings, nil
}

// ResolveAuditFinding marks a finding resolved.
func (s *EmployeeService) ResolveAuditFinding(ctx context.Context, findingID, resolution, note string) error {
	if findingID == "" {
		return wrapError("employee", "ResolveAuditFinding", ErrInvalidInput)
	}
	if s.manager == nil {
		return wrapError("employee", "ResolveAuditFinding", ErrUnavailable)
	}
	if err := s.manager.ResolveAuditFinding(ctx, findingID, resolution, note); err != nil {
		return wrapError("employee", "ResolveAuditFinding", err)
	}
	return nil
}

// Migrate scans ~/.meept/bots/*.json and proposes a constitution for each
// legacy bot. Never refuses to migrate; vague prompts get a minimal
// conservative constitution flagged for human review.
func (s *EmployeeService) Migrate(ctx context.Context) ([]any, error) {
	if s.manager == nil {
		return nil, wrapError("employee", "Migrate", ErrUnavailable)
	}
	proposals, err := s.manager.Migrate(ctx)
	if err != nil {
		return nil, wrapError("employee", "Migrate", err)
	}
	return proposals, nil
}

// ApplyMigration writes the proposed constitution for the given bot ID.
func (s *EmployeeService) ApplyMigration(ctx context.Context, botID string) (any, error) {
	if botID == "" {
		return nil, wrapError("employee", "ApplyMigration", ErrInvalidInput)
	}
	if s.manager == nil {
		return nil, wrapError("employee", "ApplyMigration", ErrUnavailable)
	}
	result, err := s.manager.ApplyMigration(ctx, botID)
	if err != nil {
		return nil, wrapError("employee", "ApplyMigration", err)
	}
	return result, nil
}
