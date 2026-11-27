package agent

// Plan-approve execution resumer (issue #177 owner ruling B, 2026-11-26).
//
// A quick_plan brief parked by approval_requested carries zero phases by
// construction. Before the ruling, an operator plan.approve ran the normal
// ApprovePlan -> Synthesize tail, and the zero-phase completion guard
// completed the contract INSTANTLY — the brief was never executed (the M11
// lie resurfacing at approve time). Ruling B: approve must run the SAME
// lane as the dispatcher's auto-approve branch — mint the strategic task,
// let the planner decompose, bind it with AdoptTask (row -> executing), and
// hand the task to the orchestrator pipeline; completion then rolls the plan
// up like any other quick_plan contract (D2).
//
// The execution half lives HERE (dispatcher/agent layer) because createTask
// and the orchestrator.plan request shape are agent-layer machinery the plan
// package must not import. PlanManager.ApprovePlan's tagged branch invokes
// the resumer seam (SetPlanExecutionResumer); this file installs the real
// resumer when the daemon wires the plan manager into the dispatcher
// (SetPlanManager), so no daemon-side edit is needed for the wiring to be
// live — the setter call already exists in the daemon boot sequence.
//
// Failure posture mirrors C-5/D5: a resumer failure never un-approves the
// row (the approval already committed) and never fails the approve RPC —
// the row stays approved for a re-drive, and the adopt half stamps its own
// adopt-prefixed SynthesisError exactly like the dispatch lane does.

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/plan"
	"github.com/caimlas/meept/pkg/models"
)

// SourcePlanApproveResumer is the bus message Source on orchestrator.plan
// requests published by the approve resumer (distinguishes approve-driven
// dispatches from chat-dispatch ones in bus forensics).
const SourcePlanApproveResumer = "dispatcher-plan-approve"

// planApproveResumer is the resumer closure installed onto the plan
// manager. It re-reads the row (the caller holds only the id), refuses
// foreign shapes loudly, then runs the createTask + AdoptTask + dispatch
// sequence with the C-5 retry pair.
func (d *Dispatcher) planApproveResumer(ctx context.Context, planID string) error {
	if d.planManager == nil {
		return fmt.Errorf("plan approve resumer: dispatcher has no plan manager")
	}
	p, err := d.planManager.GetPlan(ctx, planID)
	if err != nil {
		return fmt.Errorf("plan approve resumer: get plan: %w", err)
	}
	// Bughunt 2026-11-27 M1: the registry/bus guards run BEFORE any state
	// change. Above createTask they previously sat at the tail, so a rig
	// without a bus minted a pending task and bound the row executing
	// before discovering nothing could ever dispatch it — the stranded
	// executing-with-pending-task shape. Fail first, before any state
	// change: the row stays approved/unbound and the reclaimer's
	// quick-plan arm re-drives it.
	if d.taskRegistry == nil {
		return fmt.Errorf("plan approve resumer: no task registry wired, cannot dispatch plan %s", planID)
	}
	if d.taskRegistry.Bus() == nil {
		return fmt.Errorf("plan approve resumer: no bus wired, cannot dispatch plan %s", planID)
	}
	// Shape guards: only a quick-plan-tagged, task-less row is this
	// resumer's business. A bound/executing row already rides execution
	// (crash re-drive says nil); a foreign lane must never be
	// re-dispatched from here.
	if p.Kind != plan.QuickPlanKind {
		return fmt.Errorf("plan approve resumer: plan %s kind %q is not a quick_plan contract", p.ID, p.Kind)
	}
	if p.TaskID != "" || p.State == plan.StateExecuting {
		return nil // crash re-drive: AdoptTask already bound the task
	}
	switch p.State {
	case plan.StateApproved:
		// Normal operator-approve path: ApprovePlan committed the
		// approval before firing this resumer.
	case plan.StatePendingApproval:
		// Re-drive caller (or a seam user driving a still-parked row):
		// commit the approval first via the ruling-B primitive — the
		// exact ApprovePlan CAS/signoff/columns/event sequence WITHOUT
		// the Synthesize tail.
		if err := d.planManager.ApprovePlanForExecution(ctx, p.ID, "system", p.SourceSession); err != nil {
			return fmt.Errorf("plan approve resumer: approve re-drive for %s: %w", p.ID, err)
		}
		if fresh, ferr := d.planManager.GetPlan(ctx, p.ID); ferr == nil {
			p = fresh
		}
	default:
		return fmt.Errorf("plan approve resumer: plan %s state %s, want approved/pending_approval", p.ID, p.State)
	}

	// The brief's execution input: the dispatch wrote the FULL user input
	// into Description (CreatePlanWithGoal(summary, input, ...)); Prompt
	// and Title are the fallbacks so a hand-parked row still runs
	// something meaningful rather than the empty mandate.
	input := p.Description
	if input == "" {
		input = p.Prompt
	}
	if input == "" {
		input = p.Title
	}
	sessionID := p.SourceSession

	// The quickplan-lane intent shape (mirrors the classifier quickplan
	// verdict and routeSequentialCompoundAsQuickPlan: AgentType
	// "orchestrator", RequiresPlanning true). OriginalInput keeps the full
	// text (M4 lossless-resume convention) so downstream waits see it.
	intent := &Intent{
		Type:             string(IntentQuickPlan),
		Confidence:       1.0,
		AgentType:        "orchestrator",
		RequiresPlanning: true,
		Summary:          p.Title,
		OriginalInput:    input,
		Method:           "plan_approve_resume",
	}

	created := d.createTask(ctx, input, intent, sessionID, intent.AgentType)
	if created == nil {
		return fmt.Errorf("plan approve resumer: createTask failed for plan %s", p.ID)
	}

	// C-5/D5 retry pair, identical posture to the dispatch lane: one
	// retry, then MarkAdoptFailure stamps the row and the task still runs
	// (the contract row is degraded, not the execution).
	adoptErr := d.planManager.AdoptTask(ctx, p.ID, created.ID)
	if adoptErr != nil {
		d.logger.Warn("quick_plan approve adopt failed; retrying once",
			"plan_id", p.ID,
			"task_id", created.ID,
			"error", adoptErr,
		)
		adoptErr = d.planManager.AdoptTask(ctx, p.ID, created.ID)
	}
	if adoptErr != nil {
		if markErr := d.planManager.MarkAdoptFailure(ctx, p.ID, adoptErr); markErr != nil {
			d.logger.Warn("failed to stamp quick_plan adopt failure on approved contract row",
				"plan_id", p.ID, "task_id", created.ID, "error", markErr)
		}
		d.logger.Warn("quick_plan approve task remains unbound after adopt retry; dispatching execution anyway",
			"plan_id", p.ID, "task_id", created.ID, "error", adoptErr)
	}

	// Dispatch: the SAME orchestrator.plan request the chat handler builds
	// for an async quick_plan dispatch (publishPlanRequest), so the
	// strategic planner decomposes and the tactical scheduler runs the
	// task. Mode quick_plan + DeriveDifficulty over the frozen table
	// (R7) reproduces the dispatch-lane arm exactly.
	req := PlanRequest{
		TaskID:         created.ID,
		SessionID:      sessionID,
		Input:          created.Description,
		Intent:         string(IntentQuickPlan),
		Mode:           IntentQuickPlan.SuggestedMode(),
		SessionContext: d.BuildPlanSessionContext(context.WithoutCancel(ctx), sessionID, created.ID),
		AssignedAgent:  created.AssignedAgent,
		Difficulty:     llm.DeriveDifficulty(IntentQuickPlan.SuggestedMode(), true, len(input)),
	}
	payload, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("plan approve resumer: marshal orchestrator.plan request: %w", err)
	}
	msg := &models.BusMessage{
		ID:        generateMessageID(),
		Type:      models.MessageTypeRequest,
		Topic:     "orchestrator.plan",
		Source:    SourcePlanApproveResumer,
		Timestamp: time.Now().UTC(),
		Payload:   payload,
	}
	// nil-guard: rigs without a task registry have no reachable bus
	// (calling Bus() on a nil *task.Registry would deref a nil receiver).
	// The pre-flight guards above already returned for this shape; the
	// check stays as the defensive last line before Publish.
	if d.taskRegistry == nil {
		return fmt.Errorf("plan approve resumer: no task registry wired, orchestrator.plan not published (task %s)", created.ID)
	}
	bus := d.taskRegistry.Bus()
	if bus == nil {
		return fmt.Errorf("plan approve resumer: no bus wired, orchestrator.plan not published (task %s)", created.ID)
	}
	if delivered := bus.Publish("orchestrator.plan", msg); delivered == 0 {
		d.logger.Warn("orchestrator.plan approve request published with no subscribers",
			"task_id", created.ID, "plan_id", p.ID)
	} else {
		d.logger.Info("quick_plan contract released by operator approve: task dispatched",
			"plan_id", p.ID,
			"task_id", created.ID,
			"session", sessionID,
		)
	}
	return nil
}

// wirePlanApproveResumer installs the ruling-B resumer onto the wired plan
// manager. Called from SetPlanManager, so the existing daemon wiring line
// (components.Dispatcher.SetPlanManager(planManagerInst)) arms the seam with
// zero daemon-side changes. The evolver SINK manager is deliberately not
// touched: sink plans are never quick-plan-tagged.
func (d *Dispatcher) wirePlanApproveResumer() {
	if d.planManager == nil {
		return
	}
	d.planManager.SetPlanExecutionResumer(d.planApproveResumer)
}
