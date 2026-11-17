// goal_gate_wiring.go wires the goal-loop HUMAN-approval gate (audit
// HITL-1) through the EXISTING gate checkpoint parker — no second parker:
//
//   - PARK: the daemon wraps c.GateParker.ParkGate with GateKind "goal"
//     (the parker already scopes every kind behind ParkKindGate and the
//     gateParkKey <gate_kind>|<gate_id> identity) and injects it into the
//     employee manager via Manager.SetGoalGateParker; the manager hands
//     the SAME wrapper to every registered goal loop (and to loops
//     registered later). The loop parks the turn inside Reflect when its
//     effective gate config has Type "approval".
//   - RESUME (two coordinated halves, both at-most-once):
//     1. DRAIN+DISPATCH: agents.goals.approve_gate →
//     Manager.ResumeGoalGateParked → GateParker.ResumeByKey("goal",
//     goal id) — delete-before-callback — whose resume callback
//     (wireGateParker) sees GateKind "goal" and calls
//     resumeGoalGateFromDrain: the decoded row's TurnReentry (plan
//     id, prompt, recorded result, employee_id) re-enters the owning
//     loop via GoalLoop.ResumeGoalGateApproval, which re-runs Reflect
//     with the approval short-circuit.
//     2. COORDINATION: the manager seam reports whether a row was
//     drained so the RPC answers resumed=true/false honestly; the
//     re-entry itself rides the drain callback (the decode happened
//     there, so the payload never needs a second read).
//   - Restart re-arm is the parker's existing Start (a goal gate parked
//     before a boot is resumable by a late approval).
//
// Notification note: the resume callback's notification copy is
// plan-worded for both kinds; goal-specific copy is a follow-up (fan-out
// path is kind-agnostic).
package daemon

import (
	"context"

	"github.com/caimlas/meept/internal/agent"
)

// goalGateApprovalKind matches agent.GateParkData.GateKind for goal gates
// (the parker's key namespace is <gate_kind>|<gate_id>).
const goalGateApprovalKind = "goal"

// goalGateParkAdapter wraps the components' GateParker into the
// Manager.SetGoalGateParker park seam. Nil parker → the seam reports false
// (parking disabled: approval gates degrade to at_risk in the loop).
func (c *Components) goalGateParkAdapter(data agent.GateParkData) bool {
	if c == nil || c.GateParker == nil {
		return false
	}
	return c.GateParker.ParkGate(data)
}

// resumeGoalGateFromDrain re-enters the owning loop with the drained
// goal-gate row's re-entry payload (called from the parker's resume
// callback AFTER the persistence row was deleted — at-most-once). The
// employee id rides the row's TurnReentry (stamped at park time); a row
// without it (older park) is logged and dropped — a later round re-raises
// the gate.
func (c *Components) resumeGoalGateFromDrain(data agent.GateParkData) {
	if c == nil || c.EmployeeManager == nil {
		return
	}
	employeeID, _ := data.TurnReentry["employee_id"].(string)
	if employeeID == "" {
		c.Logger.Warn("goal gate resumed without employee_id — re-entry dropped",
			"goal_id", data.GateID)
		return
	}
	loop := c.EmployeeManager.GetGoalLoop(employeeID)
	if loop == nil {
		c.Logger.Warn("goal gate resumed but no loop registered — re-entry dropped",
			"goal_id", data.GateID,
			"employee_id", employeeID,
		)
		return
	}
	loop.ResumeGoalGate(context.Background(), data.TurnReentry)
}

// wireGoalGateApproval installs the approval-gate park seam on the
// employee manager (the resume dispatch lives in the parker's resume
// callback via resumeGoalGateFromDrain). Idempotent posture: nil manager
// or nil GateParker is a no-op (approval gates degrade in the loop, never
// panic).
//
// Called from StartGateParkPump (which already guarantees c.GateParker is
// wired or deliberately absent) so the seam lands exactly once per boot,
// after the park store exists. Loop registration happens BEFORE this in
// Components.Start, and RegisterGoalLoop forwards the park seam to late
// loops, so ordering is safe.
func (c *Components) wireGoalGateApproval() {
	if c == nil || c.EmployeeManager == nil || c.GateParker == nil {
		if c != nil && c.Logger != nil {
			c.Logger.Debug("goal approval gate seam not wired: manager or gate parker absent")
		}
		return
	}
	c.EmployeeManager.SetGoalGateParker(c.goalGateParkAdapter)
	c.EmployeeManager.SetGoalGateResumeFn(func(ctx context.Context, goalID string) bool {
		if c.GateParker == nil {
			return false
		}
		return c.GateParker.ResumeByKey(ctx, goalGateApprovalKind, goalID)
	})
	c.Logger.Info("goal approval gate seams wired",
		"gate_kind", goalGateApprovalKind,
		"resume_route", "agents.goals.approve_gate",
	)
}

// StartGoalGateApprovalPump arms the goal-gate approval surface: it wires
// the park seam onto the employee manager right after the gate park pump
// starts (parker constructed, park store live, Start re-arm armed). No-op
// on nil wiring.
func (c *Components) StartGoalGateApprovalPump(ctx context.Context) {
	if c == nil {
		return
	}
	c.wireGoalGateApproval()
	_ = ctx // lifecycle context kept for pump-shape symmetry with StartGateParkPump
}
