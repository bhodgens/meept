package agent

// Goal resolution at chat-path plan creation (goal-completion leaf 03,
// SHARED-CONVENTIONS C6): every chat-created Plan carries a GoalID by
// default —
//
//  1. the session's explicitly bound goal (session metadata goal_id),
//     when set and still active;
//  2. else the employee's single active non-implicit goal, when exactly
//     one exists;
//  3. else a SourceImplicit goal create-or-reused from the plan title.
//
// Employee identity: sessions do NOT all belong to employees and this
// package cannot import internal/employee (goal_loop.go imports agent —
// an import cycle). The daemon injects a GoalAnchorStore adapter over
// *employee.GoalStore + the employee roster, exactly like the
// planManager seam. Employee resolution prefers the session's explicitly
// bound employee (session employee_id column, follow-up #5) and falls
// back to the daemon's sole employee; when neither resolves,
// resolveGoalID returns "" and the plan stays goal-less (legacy-legal;
// chat without employees must keep working — hard requirement).

import (
	"context"
)

// GoalAnchorStore is the narrow surface the dispatcher needs to resolve a
// session's goal at plan-creation time. The daemon adapts
// *employee.GoalStore plus the employee roster onto it; tests inject stubs.
// Every method degrades to its empty answer ("", "") rather than erroring:
// a failed goal anchor must never fail a dispatch.
type GoalAnchorStore interface {
	// SessionGoalID returns the goal explicitly bound to the session
	// (C6 branch 1). Returns "" when unset OR no longer active — a
	// retired/paused bound goal must not anchor new plans.
	SessionGoalID(sessionID string) string
	// SoleEmployeeID returns the employee the daemon attributes chat
	// sessions to: the sole configured employee. Returns "" when zero or
	// multiple employees exist (no reliable per-session binding) — the
	// implicit branch stays off rather than mis-attributing work.
	// Retained for compatibility: resolveGoalID consults
	// SessionEmployeeID first and falls back to this when the session is
	// unbound (single-employee installs keep working unchanged).
	SoleEmployeeID() string
	// SessionEmployeeID resolves the employee a chat session's work is
	// attributed to (goal-completion follow-up #5): the session's
	// explicitly bound employee when set, else the daemon's sole employee,
	// else "" (zero or multiple employees and no binding — the implicit
	// branch stays off rather than mis-attributing work).
	SessionEmployeeID(sessionID string) string
	// SoleActiveGoalID returns the employee's single active non-implicit
	// goal id (C6 branch 2), or "" when zero or multiple exist.
	SoleActiveGoalID(ctx context.Context, employeeID string) string
	// EnsureImplicitGoal create-or-reuses the employee's SourceImplicit
	// goal titled from the plan title (C6 branch 3). Returns "" on error.
	EnsureImplicitGoal(ctx context.Context, employeeID, title string) string
}

// SetGoalAnchor wires the goal-resolution store after construction (same
// post-construction seam as SetPlanManager/SetSessionStore: the daemon
// builds the dispatcher before the employee subsystem). Nil is a no-op.
func (d *Dispatcher) SetGoalAnchor(anchor GoalAnchorStore) {
	if anchor != nil {
		d.goalAnchor = anchor
	}
}

// resolveGoalID implements the C6 precedence for one plan creation.
// Returns the goal id to stamp on the plan, or "" when no goal applies
// (no anchor, no session binding, no resolvable employee).
func (d *Dispatcher) resolveGoalID(ctx context.Context, sessionID, planTitle string) string {
	if d.goalAnchor == nil {
		return ""
	}

	// Branch 1: explicit session goal (validated active by the anchor).
	if gid := d.goalAnchor.SessionGoalID(sessionID); gid != "" {
		return gid
	}

	// Branches 2 and 3 need an employee. Prefer the session's explicitly
	// bound employee (employee_id column; set when a chat session is
	// routed to an employee), falling back to the daemon's sole employee
	// for unbound sessions — single-employee installs keep working
	// unchanged, and "" means neither path resolved (leave the plan
	// unanchored).
	employeeID := d.goalAnchor.SessionEmployeeID(sessionID)
	if employeeID == "" {
		employeeID = d.goalAnchor.SoleEmployeeID()
	}
	if employeeID == "" {
		return ""
	}

	// Branch 2: exactly one active non-implicit goal.
	if gid := d.goalAnchor.SoleActiveGoalID(ctx, employeeID); gid != "" {
		return gid
	}

	// Branch 3: create-or-reuse the implicit goal from the plan title.
	gid := d.goalAnchor.EnsureImplicitGoal(ctx, employeeID, planTitle)
	if gid == "" {
		d.logger.Warn("goal anchor failed to ensure implicit goal; plan stays goal-less",
			"session", sessionID,
			"employee", employeeID,
		)
	}
	return gid
}

// goalAnchor is stored on the Dispatcher via SetGoalAnchor; the field lives
// beside planManager so the wiring story stays one seam.
