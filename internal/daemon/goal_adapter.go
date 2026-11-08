package daemon

import (
	"context"
	"errors"
	"fmt"

	"github.com/caimlas/meept/internal/employee"
	"github.com/caimlas/meept/internal/tools/builtin"
)

// goal_adapter.go — adapts the LIVE *employee.Manager onto
// builtin.GoalManager (the goal tool store seam). internal/employee cannot be
// imported by internal/tools/builtin (employee → bot → … → tools/builtin), so
// the daemon supplies the mapping through builtin.SetGoalProvider.
//
// The adapter never invents behavior: every mutation goes through the
// employee Manager/GoalStore and the CAS revision is advanced by the store on
// write (UpdateGoal bumps revision exactly once per persist).

// errGoalStoreUnavailable is the fail-closed error the goal adapter returns
// when the live manager is absent (a nil *employee.Manager boxed into the
// interface — the typed-nil wiring hazard the SetGoalProvider reflectNil guard
// is built to catch). It matches the tool's "goal store not available" shape
// so an unwired seam reads as a clean error result, never a panic.
var errGoalStoreUnavailable = errors.New("goal store not available")

// goalEmployeeManagerAdapter maps *employee.Manager onto builtin.GoalManager.
// The goalManager interface name is unexported (hardened cross-package
// wiring), so interface satisfaction is checked structurally where the
// adapter is passed to builtin.SetGoalProvider — there is no name we can pin.
type goalEmployeeManagerAdapter struct {
	m *employee.Manager
}

// CreateGoal maps the builtin-local GoalCreateSpec onto employee.GoalSpec and
// persists through Manager.CreateGoal. Objective becomes both the title and
// the mandate (the tool carries no separate title); MaxRounds flows through
// GoalSpec.MaxRounds so the created goal carries the round cap at revision 1.
// The created goal is armed for continuation (matching the tool contract's
// armed-on-create behavior and the dsh active-continuation default).
func (a *goalEmployeeManagerAdapter) CreateGoal(ctx context.Context, spec builtin.GoalCreateSpec) (*builtin.GoalRecord, error) {
	if a == nil || a.m == nil {
		return nil, errGoalStoreUnavailable
	}
	if spec.Owner == "" {
		return nil, fmt.Errorf("create goal: owner is required")
	}
	g, err := a.m.CreateGoal(ctx, employee.GoalSpec{
		EmployeeID: spec.Owner,
		Title:      spec.Objective,
		Mandate:    spec.Objective,
		Source:     string(employee.SourceUser),
		MaxRounds:  spec.MaxRounds,
	})
	if err != nil {
		return nil, err
	}
	if g == nil {
		return nil, fmt.Errorf("create goal: nil goal")
	}
	// Process-local arming (matching the tool contract's armed-on-create):
	// the goal is armed for automatic continuation by the round driver.
	g.Activate()
	return goalRecordFromGoal(g), nil
}

// GetGoal returns the goal by ID through the live manager.
func (a *goalEmployeeManagerAdapter) GetGoal(ctx context.Context, id string) (*builtin.GoalRecord, error) {
	if a == nil || a.m == nil {
		return nil, errGoalStoreUnavailable
	}
	g, err := a.m.GetGoal(ctx, id)
	if err != nil {
		return nil, err
	}
	if g == nil {
		return nil, nil //nolint:nilnil // documented not-found contract: the tool layer maps nil goal to ErrGoalToolNotFound
	}
	return goalRecordFromGoal(g), nil
}

// ListActive returns the owner's active (non-retired) goals, ordered
// oldest-first, via the live manager.
func (a *goalEmployeeManagerAdapter) ListActive(ctx context.Context, owner string) ([]*builtin.GoalRecord, error) {
	if a == nil || a.m == nil {
		return nil, errGoalStoreUnavailable
	}
	goals, err := a.m.ListGoals(ctx, owner)
	if err != nil {
		return nil, err
	}
	out := make([]*builtin.GoalRecord, 0, len(goals))
	for _, g := range goals {
		if g != nil {
			out = append(out, goalRecordFromGoal(g))
		}
	}
	return out, nil
}

// UpdateGoal applies the tool's mutations back onto the live *employee.Goal
// and persists through Manager.UpdateGoal (which advances the CAS revision).
func (a *goalEmployeeManagerAdapter) UpdateGoal(ctx context.Context, rec *builtin.GoalRecord) error {
	if a == nil || a.m == nil {
		return errGoalStoreUnavailable
	}
	if rec == nil || rec.ID == "" {
		return fmt.Errorf("update goal: goal id is required")
	}
	g, err := a.m.GetGoal(ctx, rec.ID)
	if err != nil {
		return err
	}
	if g == nil {
		return employee.ErrGoalNotFound
	}
	applyGoalRecord(g, rec)
	return a.m.UpdateGoal(ctx, g)
}

// goalRecordFromGoal maps an *employee.Goal onto the builtin-local GoalRecord
// view shared with the goal tools. Phase uses the employee phase string;
// Objective is the mandate; Armed is the process-local activation flag the
// manager instance carries.
func goalRecordFromGoal(g *employee.Goal) *builtin.GoalRecord {
	if g == nil {
		return nil
	}
	return &builtin.GoalRecord{
		ID:            g.ID,
		Revision:      g.Revision,
		Objective:     g.Mandate,
		Phase:         g.State.String(),
		RoundsStarted: g.RoundsStarted,
		MaxRounds:     g.MaxRounds,
		BlockedReason: blockedReasonView(g.BlockedReason),
		Armed:         g.IsArmed(),
	}
}

// applyGoalRecord writes the tool's mutated fields back onto the live
// *employee.Goal before persisting. Phase/objective/rounds/maxRounds/blocked
// map directly; Armed maps to the process-local Activate/Disarm. The CAS
// revision is NOT written here — the store bumps it on the manager's Update.
func applyGoalRecord(g *employee.Goal, rec *builtin.GoalRecord) {
	if g == nil || rec == nil {
		return
	}
	if rec.Objective != "" {
		g.Mandate = rec.Objective
	}
	// Only map a known phase string; empty means "leave unchanged" (the tool
	// always sets a phase on write, so this is defensive).
	if rec.Phase != "" {
		g.State = goalStateFromPhase(rec.Phase)
	}
	g.RoundsStarted = rec.RoundsStarted
	if rec.MaxRounds > 0 {
		g.MaxRounds = rec.MaxRounds
	}
	g.BlockedReason = goalBlockedReason(rec.BlockedReason, g.BlockedReason)
	if rec.Armed {
		g.Activate()
	} else {
		g.Disarm()
	}
}

// goalStateFromPhase parses the tool's phase string back into an
// employee.GoalState. Unknown strings degrade to GoalActive (matching
// employee.ParseGoalState's defensive default) — the tool never emits one.
func goalStateFromPhase(s string) employee.GoalState {
	st, err := employee.ParseGoalState(s)
	if err != nil {
		return employee.GoalActive
	}
	return st
}

// blockedReasonView collapses *employee.BlockedReason into the tool's single
// string form (code prefix when present, else just the message). Nil maps to
// nil.
func blockedReasonView(br *employee.BlockedReason) *string {
	if br == nil {
		return nil
	}
	if br.Code != "" {
		out := br.Code + ": " + br.Message
		return &out
	}
	msg := br.Message
	return &msg
}

// goalBlockedReason maps the tool's single reason string back onto an
// *employee.BlockedReason, preserving an existing code (so a round-limit code
// survives an edit round-trip); a model/tool self-report without a code gets
// "model-reported".
func goalBlockedReason(s *string, existing *employee.BlockedReason) *employee.BlockedReason {
	if s == nil || *s == "" {
		return nil
	}
	code := "model-reported"
	if existing != nil && existing.Code != "" {
		code = existing.Code
	}
	return &employee.BlockedReason{Code: code, Message: *s}
}
