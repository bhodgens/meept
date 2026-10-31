package employee

import (
	"context"
	"testing"
)

// TestManagerSatisfiesGoalCreator pins the compile-time fact the
// github-intake-factory tree's intake.GoalCreator seam (CreateGoal +
// ListGoals + UpdateGoal) is satisfied by *Manager. UpdateGoal was
// added as a GoalStore delegate for intake re-dispatch (leaf 02).
func TestManagerSatisfiesGoalCreator(t *testing.T) {
	m := &Manager{}
	var _ interface {
		CreateGoal(ctx context.Context, spec GoalSpec) (*Goal, error)
		ListGoals(ctx context.Context, employeeID string) ([]*Goal, error)
		UpdateGoal(ctx context.Context, goal *Goal) error
	} = m
	_ = m
}
