package daemon

// goalAnchorAdapter adapts the employee goal store + employee roster onto
// agent.GoalAnchorStore (goal-completion leaf 03 Task 2). It lives in the
// daemon because the resolution rule needs BOTH subsystems: sessions do not
// carry an employee binding, so the "session's employee" resolves to the
// daemon's SOLE configured employee — "" when zero or several exist, which
// keeps the implicit-goal branch off rather than mis-attributing chat work.
// This mirrors the planCreatorAdapter seam: internal/agent must not import
// internal/employee (goal_loop.go imports agent — import cycle).

import (
	"context"
	"log/slog"

	"github.com/caimlas/meept/internal/employee"
	"github.com/caimlas/meept/internal/session"
)

// soleEmployeeSource lists the current employees at call time (the roster
// changes at runtime via hire/retire).
type soleEmployeeSource interface {
	// ListEmployeeIDs returns the ids of all current employees.
	ListEmployeeIDs(ctx context.Context) ([]string, error)
}

// managerEmployeeSource adapts *employee.Manager onto soleEmployeeSource.
type managerEmployeeSource struct {
	m *employee.Manager
}

// ListEmployeeIDs resolves the live roster through Manager.ListEmployees
// (no status filter). Errors surface to the caller, which degrades to "".
func (s managerEmployeeSource) ListEmployeeIDs(ctx context.Context) ([]string, error) {
	if s.m == nil {
		return nil, nil //nolint:nilnil // no manager = no employees, not an error
	}
	emps, err := s.m.ListEmployees(ctx, "")
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(emps))
	for _, e := range emps {
		ids = append(ids, e.ID)
	}
	return ids, nil
}

// goalAnchorAdapter implements agent.GoalAnchorStore.
type goalAnchorAdapter struct {
	sessions *session.SQLiteStore
	goals    *employee.GoalStore
	roster   soleEmployeeSource
	logger   *slog.Logger
}

// Compile-time guard: the adapter must satisfy agent.GoalAnchorStore.
var _ interface {
	SessionGoalID(sessionID string) string
	SoleEmployeeID() string
	SoleActiveGoalID(ctx context.Context, employeeID string) string
	EnsureImplicitGoal(ctx context.Context, employeeID, title string) string
} = (*goalAnchorAdapter)(nil)

// SessionGoalID returns the session's explicitly bound goal, but ONLY while
// the goal is still active (C6 branch 1: "if set and the goal is active").
// A retired/paused binding falls through to branch 2 instead of anchoring.
func (a *goalAnchorAdapter) SessionGoalID(sessionID string) string {
	if a.sessions == nil || sessionID == "" {
		return ""
	}
	sess := a.sessions.Get(sessionID)
	if sess == nil || sess.GoalID == "" {
		return ""
	}
	if a.goals == nil {
		return ""
	}
	goal, err := a.goals.Get(context.Background(), sess.GoalID)
	if err != nil || goal == nil || goal.State != employee.GoalActive {
		return ""
	}
	return sess.GoalID
}

// SoleEmployeeID resolves the session-attributed employee: the daemon's
// sole configured employee. Zero or multiple employees => "" (no reliable
// per-session employee binding exists in the session model today).
func (a *goalAnchorAdapter) SoleEmployeeID() string {
	if a.roster == nil {
		return ""
	}
	ids, err := a.roster.ListEmployeeIDs(context.Background())
	if err != nil || len(ids) != 1 {
		return ""
	}
	return ids[0]
}

// SoleActiveGoalID returns the employee's single active NON-implicit goal
// (C6 branch 2). Zero or multiple => "".
func (a *goalAnchorAdapter) SoleActiveGoalID(ctx context.Context, employeeID string) string {
	if a.goals == nil || employeeID == "" {
		return ""
	}
	goals, err := a.goals.ListActive(ctx, employeeID)
	if err != nil {
		a.logger.Warn("goal anchor: list active failed", "employee_id", employeeID, "error", err)
		return ""
	}
	var sole string
	for _, g := range goals {
		if g == nil || g.Source == employee.SourceImplicit {
			continue
		}
		if sole != "" {
			return "" // more than one non-implicit active goal
		}
		sole = g.ID
	}
	return sole
}

// EnsureImplicitGoal create-or-reuses the employee's SourceImplicit goal
// titled from the plan title (C6 branch 3). Errors degrade to "" so a goal
// anchor failure never fails a dispatch (the plan stays goal-less).
func (a *goalAnchorAdapter) EnsureImplicitGoal(ctx context.Context, employeeID, title string) string {
	if a.goals == nil || employeeID == "" {
		return ""
	}
	goal, err := a.goals.EnsureImplicitGoal(ctx, employeeID, title)
	if err != nil {
		a.logger.Warn("goal anchor: ensure implicit goal failed",
			"employee_id", employeeID, "error", err)
		return ""
	}
	if goal == nil {
		return ""
	}
	return goal.ID
}

// newGoalAnchorAdapter builds the adapter; any nil subsystem keeps the
// corresponding branch inert (sessions==nil disables branch 1; goals==nil
// or roster==nil disables branches 2-3).
func newGoalAnchorAdapter(sessions *session.SQLiteStore, goals *employee.GoalStore, roster soleEmployeeSource, logger *slog.Logger) *goalAnchorAdapter {
	if logger == nil {
		logger = slog.Default()
	}
	return &goalAnchorAdapter{sessions: sessions, goals: goals, roster: roster, logger: logger}
}
