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
	ApprovePlan(ctx context.Context, goalID, planID, callerID, reason string) error
	RejectPlan(ctx context.Context, goalID, planID, callerID, reason string) error
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

// SetLogger replaces the default logger. Nil is ignored (typed-nil guard per
// CLAUDE.md setter rules).
func (s *EmployeeService) SetLogger(l *slog.Logger) {
	if l == nil {
		return
	}
	s.logger = l
}
