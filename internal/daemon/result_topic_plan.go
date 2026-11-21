package daemon

import (
	"context"
	"errors"
	"fmt"

	"github.com/caimlas/meept/internal/employee"
	"github.com/caimlas/meept/internal/plan"
)

// SetPlanResultTopic persists the round's result-topic stamp on the plan
// row (employee.PlanResultTopicSetter). GoalLoop copies the stamp onto the
// PlanRef at construction; this is the store write. A nil manager is an
// error, not a silent skip — the caller logs it and still runs the round.
func (a *planCreatorAdapter) SetPlanResultTopic(ctx context.Context, planID, topic string) error {
	if a == nil || a.planMgr == nil {
		return fmt.Errorf("plan manager not configured")
	}
	return a.planMgr.SetPlanResultTopic(ctx, planID, topic)
}

// PlanResultTopic loads the stamp back for Resume and ApproveAndExecute.
// A missing plan is ( "", false, nil ): the caller uses employee.result
// and logs a warning. It must not treat a miss as bot.result.
func (a *planCreatorAdapter) PlanResultTopic(ctx context.Context, planID string) (string, bool, error) {
	if a == nil || a.planMgr == nil {
		return "", false, fmt.Errorf("plan manager not configured")
	}
	p, err := a.planMgr.GetPlan(ctx, planID)
	if err != nil {
		if errors.Is(err, plan.ErrPlanNotFound) {
			return "", false, nil
		}
		return "", false, err
	}
	if p == nil {
		return "", false, nil
	}
	return p.ResultTopic, true, nil
}

// Compile-time guards: the production planner must satisfy both seams.
// planBriefLookup lives in employee_service_adapter.go (leaf 02) and does
// not copy this column; GoalLoop falls through to PlanResultTopic.
var _ employee.PlanResultTopicSetter = (*planCreatorAdapter)(nil)
var _ employee.PlanResultTopicLookup = (*planCreatorAdapter)(nil)
