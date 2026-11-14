// employee_resume_wiring.go wires the employee resume subscriber (tree
// docs/plans/20261113-escalation-resume-and-hardening, leaf 02, Contract C2)
// into the daemon:
//
//   - ONE subscriber "employee-plan-resume" on bus topic "plan.approved":
//     when any approval surface (CLI plans approve, agents.goals.approve,
//     HTTP/GUI) funnels through PlanManager.ApprovePlan and publishes the
//     event, the subscriber resolves the plan's goal owner and invokes
//     Manager.ResumeApprovedPlan (Contract C1, leaf 01) so the employee's
//     GoalLoop resumes execution without a parked episode;
//   - the seam is the resumeApprovedPlan func field on Components. The
//     committed gate test (employee_resume_wiring_test.go) injects a
//     two-argument recorder, so the field is func(planID, callerID string)
//     error and the production wiring closes over the components lifecycle
//     context to call the frozen C1 signature ResumeApprovedPlan(ctx,
//     planID, callerID). nil seam = the employee subsystem is absent and
//     the subscriber is not armed (the gate pump and the evolver approval
//     bridge remain untouched);
//   - fan-out stays best-effort: a malformed payload or a resume error is
//     a logged WARN + drop — never a panic, never a retry, never a
//     re-publish. The approval flow that triggered the event must not
//     fail because of this subscriber.
package daemon

import (
	"context"
	"encoding/json"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/pkg/models"
)

// employeeResumeSubscriberID is the bus subscriber id (Contract C2 pins
// it exactly).
const employeeResumeSubscriberID = "employee-plan-resume"

// wireEmployeeResume arms the resume seam from the employee manager. The
// two-arg field shape is pinned by the gate test's recorder injection;
// the closure binds the frozen C1 signature with the components lifecycle
// context so an in-flight resume cancels with the daemon. A nil manager
// (typed-nil rule: concrete pointer checked before the assignment) leaves
// the seam nil and the subscriber unarmed.
func (c *Components) wireEmployeeResume(ctx context.Context) {
	if c == nil || c.EmployeeManager == nil {
		return
	}
	m := c.EmployeeManager
	c.resumeApprovedPlan = func(planID, callerID string) error {
		return m.ResumeApprovedPlan(ctx, planID, callerID)
	}
}

// pumpEmployeeResume drains the plan.approved subscription and drives the
// employee resume seam. Mirrors the gate park pump shape (one subscriber,
// components-context lifetime, undecodable payloads logged and skipped).
func (c *Components) pumpEmployeeResume(ctx context.Context, sub *bus.Subscriber) {
	for {
		select {
		case <-ctx.Done():
			c.msgBus.Unsubscribe(sub)
			return
		case msg, ok := <-sub.Channel:
			if !ok {
				return
			}
			c.handleEmployeePlanApproved(msg)
		}
	}
}

// handleEmployeePlanApproved processes one plan.approved event: decode
// {plan_id, by} (the internal/plan manager publisher shape), then invoke
// the resume seam. The goal-owner resolution (plan record GoalID / goal
// store linkage) lives inside Contract C1's ResumeApprovedPlan — the
// subscriber stays a thin transport: every deliberate skip is a
// structured log line carrying the subscriber id, and nothing here
// retries or re-publishes.
func (c *Components) handleEmployeePlanApproved(msg *models.BusMessage) {
	if msg == nil || c.resumeApprovedPlan == nil {
		return
	}
	log := c.Logger.With("subscriber", employeeResumeSubscriberID)
	var payload struct {
		PlanID string `json:"plan_id"`
		By     string `json:"by"`
	}
	if err := json.Unmarshal(msg.Payload, &payload); err != nil || payload.PlanID == "" {
		log.Warn("employee resume: undecodable plan.approved payload -- dropped",
			"error", err)
		return
	}
	if err := c.resumeApprovedPlan(payload.PlanID, payload.By); err != nil {
		log.Warn("employee resume failed -- dropped (no retry)",
			"plan_id", payload.PlanID, "error", err)
		return
	}
	log.Info("employee resume invoked on plan approval",
		"plan_id", payload.PlanID,
		"by", payload.By,
	)
}

// startEmployeeResumeSubscriber subscribes the employee resume pump on the
// components lifecycle context and returns the subscriber so tests can
// observe its identity. nil seam or bus is a no-op returning nil: the
// subscriber must not be armed in a shape that cannot resume.
func (c *Components) startEmployeeResumeSubscriber(ctx context.Context) *bus.Subscriber {
	if c == nil || c.msgBus == nil || c.resumeApprovedPlan == nil {
		return nil
	}
	sub := c.msgBus.Subscribe(employeeResumeSubscriberID, "plan.approved")
	c.employeeResumeSubscriber = sub
	go func() { c.pumpEmployeeResume(ctx, sub) }()
	c.Logger.Info("employee resume subscriber wired",
		"subscriber", employeeResumeSubscriberID,
		"topic", "plan.approved",
	)
	return sub
}

// stopEmployeeResumeSubscriber unsubscribes the employee resume pump.
// The pump goroutine exits on its channel close or the context cancel;
// this only detaches the bus registration so late approvals cannot queue
// during shutdown. Safe to call even if Start never ran.
func (c *Components) stopEmployeeResumeSubscriber() {
	if c == nil || c.msgBus == nil {
		return
	}
	if c.employeeResumeSubscriber != nil {
		c.msgBus.Unsubscribe(c.employeeResumeSubscriber)
		c.employeeResumeSubscriber = nil
	}
}
