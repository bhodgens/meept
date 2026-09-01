package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"path/filepath"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/plan"
	"github.com/caimlas/meept/internal/skills/lifecycle"
	"github.com/caimlas/meept/pkg/models"
)

// evolverApprovedPlanTopic is the plan-system event the approval actuator
// consumes. Published by PlanManager.ApprovePlan after the signoff is
// durable — so the actuator runs strictly BEHIND the human gate: a plan is
// applied only after a real approval, never from the auto-apply path
// (skills.evolver.auto_apply stays false and is untouched by this bridge).
const evolverApprovedPlanTopic = "plan.approved"

// EvolverApprovalBridge dispatches approved evolver plans to the actuator.
// The daemon owns both the plan system (which publishes plan.approved on the
// message bus) and the skill evolver (which exposes ApplyApprovedPlan), so
// the bridge lives here: internal/plan publishes a generic event and the
// daemon-side subscriber keeps internal/plan free of any
// internal/skills/lifecycle import (no import cycle by construction).
type EvolverApprovalBridge struct {
	evolver *lifecycle.Evolver
	logger  *slog.Logger
}

// wireEvolverApprovalBridge subscribes the approval bridge to the
// plan.approved event and starts its pump goroutine on the components
// lifecycle context. No-op (nil bridge, nil error) when the evolver is not
// constructed — the wiring degrades gracefully exactly like the other
// evolver seams. Idempotent: a second call returns the existing bridge
// instead of double subscribing.
func (c *Components) wireEvolverApprovalBridge() (*EvolverApprovalBridge, error) {
	if c == nil || c.Logger == nil {
		return nil, fmt.Errorf("evolver approval bridge: components not initialized")
	}
	if c.SkillEvolver == nil || c.msgBus == nil {
		c.Logger.Debug("evolver approval bridge: evolver or message bus absent; bridge not wired")
		return nil, nil
	}
	if c.EvolverPlanApprovalBridge != nil {
		return c.EvolverPlanApprovalBridge, nil
	}

	bridge := &EvolverApprovalBridge{
		evolver: c.SkillEvolver,
		logger:  c.Logger,
	}
	sub := c.msgBus.Subscribe("evolver-approval-bridge", evolverApprovedPlanTopic)
	go c.pumpEvolverApprovals(sub)
	c.EvolverPlanApprovalBridge = bridge
	c.Logger.Info("Skill evolver approval bridge wired",
		"topic", evolverApprovedPlanTopic)
	return bridge, nil
}

// pumpEvolverApprovals drains the plan.approved subscription until the
// components context is cancelled. Each event resolves the plan file through
// the plan manager and, when the file carries evolver provenance, applies it.
// Actuator failures are logged + audited by ApplyApprovedPlan and NEVER fail
// the approval — the signoff was already durable before this event fired.
func (c *Components) pumpEvolverApprovals(sub *bus.Subscriber) {
	for {
		select {
		case <-c.ctx.Done():
			c.msgBus.Unsubscribe(sub)
			return
		case msg, ok := <-sub.Channel:
			if !ok {
				return
			}
			c.handleApprovedEvent(msg)
		}
	}
}

// handleApprovedEvent processes one plan.approved event: resolve the plan,
// check its file for evolver provenance, and apply it. Non-evolver plans are
// the common case here — they fall through with only a debug log.
func (c *Components) handleApprovedEvent(msg *models.BusMessage) {
	if msg == nil || c.EvolverPlanApprovalBridge == nil {
		return
	}
	logger := c.EvolverPlanApprovalBridge.logger

	var payload struct {
		PlanID string `json:"plan_id"`
	}
	if err := json.Unmarshal(msg.Payload, &payload); err != nil || payload.PlanID == "" {
		logger.Warn("evolver approval bridge: undecodable plan.approved payload",
			"error", err)
		return
	}
	if c.PlanManager == nil && c.EvolverPlanManager == nil {
		return
	}
	// Evolver plans are parked in the evolver's (dedicated sink) manager —
	// resolve there FIRST, then fall back to the shared human manager for
	// the degraded wiring (sink store unavailable → shared manager sink).
	var p *plan.Plan
	var err error
	for _, mgr := range []*plan.PlanManager{c.EvolverPlanManager, c.PlanManager} {
		if mgr == nil {
			continue
		}
		p, err = mgr.GetPlan(context.Background(), payload.PlanID)
		if err == nil && p != nil {
			break
		}
		p = nil
	}
	if p == nil || p.FilePath == "" {
		logger.Warn("evolver approval bridge: cannot resolve approved plan",
			"plan_id", payload.PlanID, "error", err)
		return
	}
	// Durable provenance check on the plan FILE — the same source of truth
	// the actuator re-validates. Non-evolver plans are never dispatched.
	if !lifecycle.IsEvolverPlanFile(p.FilePath) {
		logger.Debug("evolver approval bridge: non-evolver plan approved; no action",
			"plan_id", payload.PlanID)
		return
	}
	if err := c.SkillEvolver.ApplyApprovedPlan(filepath.Clean(p.FilePath)); err != nil {
		// The signoff is already recorded; a failed application must not
		// roll it back. The failure is logged here and audited (result=err)
		// inside ApplyApprovedPlan.
		logger.Error("evolver approval bridge: failed to apply approved plan",
			"plan_id", payload.PlanID, "error", err)
	}
}
