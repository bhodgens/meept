// gate_park_wiring.go wires the checkpointed gate parker (hitl-notifications
// leaf 02, contract C5) into the daemon:
//
//   - ONE dedicated TurnParker scoped ParkKindGate over the SHARED
//     parks.db ParkStore (kind-scoped rows, same table) -- no second park
//     path, no second persistence mechanism;
//   - gate PARKS ride the plan.submitting bus event: when a plan enters
//     pending_approval (the human gate), the gate checkpoint persists
//     with the plan id as GateID;
//   - gate RESUMES ride TWO pinned choke points: the plan.approved bus
//     event (every plan approval surface -- RPC plan.approve, HTTP
//     PlanService.Approve, CLI plans approve, manager auto-approve --
//     funnels through PlanManager.ApprovePlan, which publishes it) and
//     the agents.goals.approve RPC path (Manager.ApprovePlan -> the same
//     plan.approved event covers tier-2 goal plans, since the employee
//     PlanDisposer delegates to the same PlanManager);
//   - the TurnParker's Start re-arm (components.Start, after goal-loop
//     registration) re-arms gate rows parked before a restart, so a
//     late approval still resumes (T3).
package daemon

import (
	"context"
	"encoding/json"
	"time"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/pkg/models"
)

// wireGateParker constructs the gate TurnParker + GateParker on the
// components. The resume callback is nil at construction: resume is
// event-driven (ResumeByKey from the approval pump below), never
// time-driven, so Start's poller simply finds nothing due. A missing
// park store leaves the gate parker nil (checkpointing off, logged).
func (c *Components) wireGateParker() {
	if c == nil || c.Logger == nil {
		return
	}
	if c.ParkStore == nil {
		c.Logger.Info("gate checkpointing disabled: park store unavailable")
		return
	}
	parker := agent.NewGateTurnParker(
		c.Logger.With("component", "gate-turn-parker"),
		func(ctx context.Context, rec agent.ParkedTurnRecord) {
			// Resume path (approval via ResumeByKey): the approval side
			// (PlanManager.ApprovePlan) already advanced the plan; the
			// checkpoint's job here is the terminal log line (leaves
			// 03/04 attach the user-facing notification). A re-park here
			// would resurrect the gate after its approval.
			data, _, err := agent.DecodeGatePark(rec)
			if err != nil {
				c.Logger.Warn("gate park resumed with undecodable payload -- dropped",
					"session_id", rec.SessionID, "error", err)
				return
			}
			c.Logger.Info("gate checkpoint resumed",
				"gate_kind", data.GateKind,
				"gate_id", data.GateID,
				"session_id", data.SessionID,
			)
		},
	)
	parker.SetParkPersistence(c.ParkStore, agent.ParkKindGate)
	c.GateParker = agent.NewGateParker(c.Logger.With("component", "gate-parker"), parker)
}

// pumpGateApprovals drains the plan.approved subscription and resumes
// the matching gate park. Mirrors the evolver approval bridge's pump
// shape: one subscriber, components-context lifetime, undecodable
// payloads logged and skipped.
func (c *Components) pumpGateApprovals(ctx context.Context, sub *bus.Subscriber) {
	for {
		select {
		case <-ctx.Done():
			c.msgBus.Unsubscribe(sub)
			return
		case msg, ok := <-sub.Channel:
			if !ok {
				return
			}
			c.handleGateApproved(msg)
		}
	}
}

// handleGateApproved processes one plan.approved event: resume the gate
// park keyed to that plan. A missing park (plan approved without a
// checkpoint, e.g. auto-approve mode) is a no-op.
func (c *Components) handleGateApproved(msg *models.BusMessage) {
	if msg == nil || c.GateParker == nil {
		return
	}
	var payload struct {
		PlanID string `json:"plan_id"`
	}
	if err := json.Unmarshal(msg.Payload, &payload); err != nil || payload.PlanID == "" {
		c.Logger.Warn("gate resume: undecodable plan.approved payload", "error", err)
		return
	}
	if c.GateParker.ResumeByKey(context.Background(), "plan", payload.PlanID) {
		c.Logger.Info("gate resumed on plan approval",
			"gate_kind", "plan",
			"gate_id", payload.PlanID,
		)
	}
}

// pumpGateSubmits drains the plan.submitting subscription and parks the
// gate checkpoint for every plan that enters pending_approval.
func (c *Components) pumpGateSubmits(ctx context.Context, sub *bus.Subscriber) {
	for {
		select {
		case <-ctx.Done():
			c.msgBus.Unsubscribe(sub)
			return
		case msg, ok := <-sub.Channel:
			if !ok {
				return
			}
			c.handleGateSubmit(msg)
		}
	}
}

// handleGateSubmit processes one plan.submitting event: checkpoint the
// gate. The payload carries plan_id, title, and session_id (the
// plan manager publishes session_id for exactly this chat-scoping
// purpose); TurnReentry preserves the minimal re-entry set the resume
// path needs to attribute the resumed turn.
func (c *Components) handleGateSubmit(msg *models.BusMessage) {
	if msg == nil || c.GateParker == nil {
		return
	}
	var payload struct {
		PlanID    string `json:"plan_id"`
		Title     string `json:"title"`
		SessionID string `json:"session_id"`
	}
	if err := json.Unmarshal(msg.Payload, &payload); err != nil || payload.PlanID == "" {
		c.Logger.Warn("gate park: undecodable plan.submitting payload", "error", err)
		return
	}
	// Session-less sink plans (evolver) carry session_id "" and are NOT
	// user gates: skip them so the park table never fills with
	// machine plans no human can approve through a chat surface.
	if payload.SessionID == "" {
		c.Logger.Debug("gate park skipped: session-less plan (sink/auto path)",
			"plan_id", payload.PlanID)
		return
	}
	if !c.GateParker.ParkGate(agent.GateParkData{
		GateKind:  "plan",
		GateID:    payload.PlanID,
		SessionID: payload.SessionID,
		TurnReentry: map[string]any{
			"plan_id": payload.PlanID,
			"title":   payload.Title,
		},
		RequestedAt: timeNowUTC(),
	}) {
		c.Logger.Warn("gate park refused",
			"gate_kind", "plan",
			"gate_id", payload.PlanID,
		)
	}
}

// StartGateParkPump subscribes the gate park/resume pumps on the
// components lifecycle context and Starts the gate parker (its re-arm
// loads gate rows parked before this boot). Called from Components.Start
// after the goal-loop parker block: bus subscriptions need c.msgBus,
// the pumps need c.GateParker, and Start's re-arm needs the park store --
// all wired by then. Idempotent posture: nil bus or parker is a no-op.
func (c *Components) StartGateParkPump(ctx context.Context) {
	if c == nil || c.msgBus == nil || c.GateParker == nil {
		return
	}
	subApproved := c.msgBus.Subscribe("gate-resume-pump", "plan.approved")
	go func() { c.pumpGateApprovals(ctx, subApproved) }()
	subSubmit := c.msgBus.Subscribe("gate-park-pump", "plan.submitting")
	go func() { c.pumpGateSubmits(ctx, subSubmit) }()
	c.GateParker.Turns().Start(ctx)
	c.Logger.Info("gate checkpoint pump wired",
		"park_topic", "plan.submitting",
		"resume_topic", "plan.approved",
	)
}

// timeNowUTC is the clock seam for park stamps (kept as a function so a
// future test can pin it without touching the wiring flow).
func timeNowUTC() time.Time { return time.Now().UTC() }

// stopGateParker halts the gate parker's poll loop; surviving rows stay
// in the park store and re-arm on the next Start.
func (c *Components) stopGateParker() {
	if c == nil || c.GateParker == nil {
		return
	}
	c.GateParker.Turns().Stop()
}
