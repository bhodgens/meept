package agent

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/plan"
	"github.com/caimlas/meept/pkg/models"
)

// Orchestrator coordinates the strategic and tactical layers via bus subscriptions.
type Orchestrator struct {
	strategic           *StrategicPlanner
	tactical            *TacticalScheduler
	pairManager         *PairManager
	busPairOrchestrator *PairOrchestrator    // bus-channel-based agent pairing (Option C)
	planManager         *plan.PlanManager    // plan system integration for progress tracking
	bus                  *bus.MessageBus
	logger               *slog.Logger
	collaborationEngine  *CollaborationEngine     // optional: enables agent collaboration modes
	ralphLoop           *RalphLoop               // optional: Ralph loop for auto-replanning

	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// OrchestratorDeps holds dependencies for the orchestrator.
type OrchestratorDeps struct {
	Strategic           *StrategicPlanner
	Tactical            *TacticalScheduler
	PairManager         *PairManager
	BusPairOrchestrator *PairOrchestrator    // optional: enables channel-based pairing (Option C)
	PlanManager         *plan.PlanManager    // optional: plan system integration
	CollaborationEngine *CollaborationEngine     // optional: enables agent collaboration modes
	RalphLoop           *RalphLoop               // optional: Ralph loop for auto-replanning
	Bus                 *bus.MessageBus
	Logger              *slog.Logger
}

// NewOrchestrator creates a new orchestrator.
func NewOrchestrator(deps OrchestratorDeps) *Orchestrator {
	if deps.Logger == nil {
		deps.Logger = slog.Default()
	}

	return &Orchestrator{
		strategic:           deps.Strategic,
		tactical:            deps.Tactical,
		pairManager:         deps.PairManager,
		busPairOrchestrator: deps.BusPairOrchestrator,
		planManager:         deps.PlanManager,
		collaborationEngine: deps.CollaborationEngine,
		ralphLoop:           deps.RalphLoop,
		bus:                 deps.Bus,
		logger:              deps.Logger,
	}
}

// Start subscribes to orchestrator bus topics and begins processing.
func (o *Orchestrator) Start(ctx context.Context) error {
	ctx, o.cancel = context.WithCancel(ctx)

	topics := map[string]func(context.Context, *models.BusMessage){
		"orchestrator.plan":     o.handlePlanRequest,
		"orchestrator.schedule": o.handleScheduleRequest,
		"orchestrator.handoff":  o.handleHandoff,
		"queue.job.completed":   o.handleJobCompleted,
		"queue.job.failed":      o.handleJobFailed,
		"task.amend.applied":    o.handleAmendmentApplied,
		"task.amend.rejected":   o.handleAmendmentRejected,
		"pair.session_created":  o.handlePairSessionCreated,
		"pair.converged":        o.handlePairConverged,
		"pair.exhausted":        o.handlePairExhausted,
		"pair.round_failed":     o.handlePairRoundFailed,
		"collaboration.session_created": o.handleCollabSessionCreated,
		"collaboration.consensus_reached": o.handleCollabConsensus,
		"collaboration.divergence": o.handleCollabDivergence,
		"collaboration.result": o.handleCollabResult,
		"collaboration.error": o.handleCollabError,
		"collaboration.requested": o.handleCollabRequested,
		"team.result":           o.handleTeamResult,
		"team.error":            o.handleTeamError,
	}

	for topic, handler := range topics {
		sub := o.bus.Subscribe("orchestrator-"+topic, topic)
		o.wg.Add(1)
		go o.runSubscription(ctx, sub, handler)
	}

	o.logger.Info("Orchestrator started",
		"subscriptions", len(topics),
	)

	// Start bus pair orchestrator if configured
	if o.busPairOrchestrator != nil {
		if err := o.busPairOrchestrator.Start(ctx); err != nil {
			o.logger.Error("Failed to start bus pair orchestrator", "error", err)
		} else {
			o.logger.Info("Bus pair orchestrator started")
		}
	}

	return nil
}

// Stop gracefully stops the orchestrator.
func (o *Orchestrator) Stop(ctx context.Context) error {
	if o.cancel != nil {
		o.cancel()
	}

	// Stop bus pair orchestrator if running
	if o.busPairOrchestrator != nil {
		if err := o.busPairOrchestrator.Stop(ctx); err != nil {
			o.logger.Warn("Bus pair orchestrator stop error", "error", err)
		}
	}

	done := make(chan struct{})
	go func() {
		o.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		o.logger.Info("Orchestrator stopped")
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Name returns the component name.
func (o *Orchestrator) Name() string {
	return "orchestrator"
}

// SetPlanManager sets the plan manager for plan system integration.
// This is called by the daemon after the PlanManager is created, since the
// plan system is initialized after the agent components.
func (o *Orchestrator) SetPlanManager(pm *plan.PlanManager) {
	if pm != nil {
		o.planManager = pm
	}
}

// PlanManager returns the plan manager, if configured.
func (o *Orchestrator) PlanManager() *plan.PlanManager {
	return o.planManager
}

func (o *Orchestrator) runSubscription(ctx context.Context, sub *bus.Subscriber, handler func(context.Context, *models.BusMessage)) {
	defer o.wg.Done()
	for {
		select {
		case <-ctx.Done():
			o.bus.Unsubscribe(sub)
			return
		case msg, ok := <-sub.Channel:
			if !ok {
				return
			}
			handler(ctx, msg)
		}
	}
}

func (o *Orchestrator) handlePlanRequest(ctx context.Context, msg *models.BusMessage) {
	var req PlanRequest
	if err := json.Unmarshal(msg.Payload, &req); err != nil {
		o.logger.Error("Failed to parse plan request", "error", err)
		return
	}

	o.logger.Info("Received plan request",
		"task_id", req.TaskID,
		"session_id", req.SessionID,
	)

	if err := o.strategic.Plan(ctx, req); err != nil {
		o.logger.Error("Strategic planning failed",
			"task_id", req.TaskID,
			"error", err,
		)
	}
}

func (o *Orchestrator) handleScheduleRequest(ctx context.Context, msg *models.BusMessage) {
	var req struct {
		TaskID string `json:"task_id"`
	}
	if err := json.Unmarshal(msg.Payload, &req); err != nil {
		o.logger.Error("Failed to parse schedule request", "error", err)
		return
	}

	o.logger.Info("Received schedule request", "task_id", req.TaskID)

	if err := o.tactical.ScheduleReadySteps(ctx, req.TaskID); err != nil {
		o.logger.Error("Tactical scheduling failed",
			"task_id", req.TaskID,
			"error", err,
		)
	}
}

func (o *Orchestrator) handleHandoff(ctx context.Context, msg *models.BusMessage) {
	if err := o.tactical.HandleHandoff(ctx, msg); err != nil {
		o.logger.Error("Failed to handle handoff request", "error", err)
	}
}

func (o *Orchestrator) handleJobCompleted(ctx context.Context, msg *models.BusMessage) {
	var event struct {
		JobID  string          `json:"job_id"`
		Result json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(msg.Payload, &event); err != nil {
		o.logger.Error("Failed to parse job completed event", "error", err)
		return
	}

	o.logger.Info("Job completed event received", "job_id", event.JobID)

	// Ralph loop completion check: verify task completion and trigger replan if needed
	if o.ralphLoop != nil {
		// Extract task_id from job (jobs are linked to steps which are linked to tasks)
		stepID, taskID := o.extractTaskIDFromJob(ctx, event.JobID)
		if taskID != "" {
			isComplete, evidence, needsReplan := o.ralphLoop.CheckCompletion(ctx, taskID, event.Result)
			if needsReplan && !isComplete {
				o.logger.Info("Ralph loop: task incomplete, triggering replan",
					"task_id", taskID,
					"step_id", stepID,
					"iteration", o.ralphLoop.GetIterationCount(taskID))
				if err := o.ralphLoop.TriggerReplan(ctx, taskID, evidence); err != nil {
					o.logger.Error("Failed to trigger replan", "error", err)
				}
				return // Skip normal completion processing
			}
			if isComplete {
				// Reset iteration counter on successful completion
				o.ralphLoop.Reset(taskID)
			}
		}
	}

	if err := o.tactical.OnJobCompleted(ctx, event.JobID, event.Result); err != nil {
		o.logger.Error("Failed to handle job completion",
			"job_id", event.JobID,
			"error", err,
		)
	}
}

// extractTaskIDFromJob extracts the task ID from a job ID by looking up the job.
// Jobs created for task steps have the task_id embedded in their payload.
func (o *Orchestrator) extractTaskIDFromJob(ctx context.Context, jobID string) (stepID string, taskID string) {
	// Get the job from the queue to extract task_id
	// The tactical scheduler has access to the queue
	if o.tactical == nil {
		return "", ""
	}

	// Look up job by ID - the queue interface has Get method
	job, err := o.tactical.GetJobByID(ctx, jobID)
	if err != nil || job == nil {
		o.logger.Debug("Job not found for task extraction", "job_id", jobID, "error", err)
		return "", ""
	}

	// Job has task_id directly if it was created as part of a task
	if job.TaskID != "" {
		// Also extract step_id from the payload if present
		var payload struct {
			StepID string `json:"step_id"`
		}
		if err := json.Unmarshal(job.Payload, &payload); err == nil && payload.StepID != "" {
			stepID = payload.StepID
		}
		return stepID, job.TaskID
	}

	return "", ""
}

func (o *Orchestrator) handleJobFailed(ctx context.Context, msg *models.BusMessage) {
	var event struct {
		JobID string `json:"job_id"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(msg.Payload, &event); err != nil {
		o.logger.Error("Failed to parse job failed event", "error", err)
		return
	}

	o.logger.Info("Job failed event received",
		"job_id", event.JobID,
		"error", event.Error,
	)

	if err := o.tactical.OnJobFailed(ctx, event.JobID, event.Error); err != nil {
		o.logger.Error("Failed to handle job failure",
			"job_id", event.JobID,
			"error", err,
		)
	}
}

// handleAmendmentApplied handles events when an amendment is successfully applied.
func (o *Orchestrator) handleAmendmentApplied(ctx context.Context, msg *models.BusMessage) {
	var req struct {
		ID     string `json:"id"`
		TaskID string `json:"task_id"`
		Type   string `json:"type"`
		StepID string `json:"step_id,omitempty"`
		Result string `json:"result,omitempty"`
	}
	if err := json.Unmarshal(msg.Payload, &req); err != nil {
		o.logger.Error("Failed to parse amendment applied event", "error", err)
		return
	}

	o.logger.Info("Amendment applied",
		"request_id", req.ID,
		"task_id", req.TaskID,
		"type", req.Type,
		"step_id", req.StepID,
	)

	// If the amendment affects steps, trigger re-scheduling of ready steps
	if req.StepID != "" || req.Type == "add_step" || req.Type == "skip_step" || req.Type == "reprioritize" {
		if err := o.tactical.ScheduleReadySteps(ctx, req.TaskID); err != nil {
			o.logger.Error("Failed to re-schedule steps after amendment",
				"task_id", req.TaskID,
				"error", err,
			)
		}
	}
}

// handleAmendmentRejected handles events when an amendment is rejected.
func (o *Orchestrator) handleAmendmentRejected(_ context.Context, msg *models.BusMessage) {
	var req struct {
		ID     string `json:"id"`
		TaskID string `json:"task_id"`
		Type   string `json:"type"`
		Reason string `json:"reason,omitempty"`
	}
	if err := json.Unmarshal(msg.Payload, &req); err != nil {
		o.logger.Error("Failed to parse amendment rejected event", "error", err)
		return
	}

	o.logger.Warn("Amendment rejected",
		"request_id", req.ID,
		"task_id", req.TaskID,
		"type", req.Type,
		"reason", req.Reason,
	)
}

// handlePairSessionCreated is called when a new pair session is created.
// It logs the event and prepares for the pair-driven scheduling loop.
func (o *Orchestrator) handlePairSessionCreated(_ context.Context, msg *models.BusMessage) {
	var event struct {
		TaskID    string   `json:"task_id"`
		SessionID string   `json:"session_id"`
		Actor     string   `json:"actor"`
		Reviewer  string   `json:"reviewer"`
		MaxRounds int      `json:"max_rounds"`
		Criteria  []string `json:"criteria"`
	}
	if err := json.Unmarshal(msg.Payload, &event); err != nil {
		o.logger.Error("Failed to parse pair session created event", "error", err)
		return
	}

	o.logger.Info("Pair session created",
		KeyTaskID, event.TaskID,
		"session_id", event.SessionID,
		"actor", event.Actor,
		"reviewer", event.Reviewer,
		"max_rounds", event.MaxRounds,
		"criteria_count", len(event.Criteria),
	)
}

// handlePairConverged is called when a pair session converges (all criteria satisfied).
func (o *Orchestrator) handlePairConverged(_ context.Context, msg *models.BusMessage) {
	var event struct {
		SessionID string `json:"session_id"`
		TaskID    string `json:"task_id"`
		Rounds    int    `json:"rounds"`
	}
	if err := json.Unmarshal(msg.Payload, &event); err != nil {
		o.logger.Error("Failed to parse pair converged event", "error", err)
		return
	}

	o.logger.Info("Pair session converged",
		"session_id", event.SessionID,
		KeyTaskID, event.TaskID,
		"rounds", event.Rounds,
	)
}

// handlePairExhausted is called when a pair session reaches max rounds without convergence.
func (o *Orchestrator) handlePairExhausted(_ context.Context, msg *models.BusMessage) {
	var event struct {
		SessionID string `json:"session_id"`
		TaskID    string `json:"task_id"`
		Rounds    int    `json:"rounds"`
		MaxRounds int    `json:"max_rounds"`
	}
	if err := json.Unmarshal(msg.Payload, &event); err != nil {
		o.logger.Error("Failed to parse pair exhausted event", "error", err)
		return
	}

	o.logger.Warn("Pair session exhausted without convergence",
		"session_id", event.SessionID,
		KeyTaskID, event.TaskID,
		"rounds", event.Rounds,
		"max_rounds", event.MaxRounds,
	)
}

// handlePairRoundFailed is called when a pair session round fails.
func (o *Orchestrator) handlePairRoundFailed(_ context.Context, msg *models.BusMessage) {
	var event struct {
		SessionID string `json:"session_id"`
		TaskID    string `json:"task_id"`
		Round     int    `json:"round"`
		Phase     string `json:"phase"`
		Error     string `json:"error"`
	}
	if err := json.Unmarshal(msg.Payload, &event); err != nil {
		o.logger.Error("Failed to parse pair round failed event", "error", err)
		return
	}

	o.logger.Error("Pair session round failed",
		"session_id", event.SessionID,
		KeyTaskID, event.TaskID,
		"round", event.Round,
		"phase", event.Phase,
		"error", event.Error,
	)
}

// handleCollabSessionCreated handles collaboration session creation events.
func (o *Orchestrator) handleCollabSessionCreated(_ context.Context, msg *models.BusMessage) {
	var event struct {
		SessionID    string   `json:"session_id"`
		Mode         string   `json:"mode"`
		Participants []string `json:"participants"`
		TaskID       string   `json:"task_id"`
	}
	if err := json.Unmarshal(msg.Payload, &event); err != nil {
		o.logger.Error("Failed to parse collaboration session created event", "error", err)
		return
	}
	if o.collaborationEngine != nil {
		o.logger.Info("Collaboration session created",
			"session_id", event.SessionID,
			"mode", event.Mode,
			"participants", event.Participants,
			KeyTaskID, event.TaskID,
		)
	}
}

// handleCollabConsensus handles collaboration consensus reached events.
func (o *Orchestrator) handleCollabConsensus(_ context.Context, msg *models.BusMessage) {
	var event struct {
		SessionID string `json:"session_id"`
		Turns     int    `json:"turns"`
	}
	if err := json.Unmarshal(msg.Payload, &event); err != nil {
		o.logger.Error("Failed to parse collaboration consensus event", "error", err)
		return
	}
	o.logger.Info("Collaboration consensus reached",
		"session_id", event.SessionID,
		"turns", event.Turns,
	)
}

// handleCollabDivergence handles collaboration divergence events.
func (o *Orchestrator) handleCollabDivergence(_ context.Context, msg *models.BusMessage) {
	var event struct {
		SessionID string `json:"session_id"`
	}
	if err := json.Unmarshal(msg.Payload, &event); err != nil {
		o.logger.Error("Failed to parse collaboration divergence event", "error", err)
		return
	}
	o.logger.Warn("Collaboration divergence detected",
		"session_id", event.SessionID,
	)
}

// handleCollabResult handles collaboration result events.
func (o *Orchestrator) handleCollabResult(_ context.Context, msg *models.BusMessage) {
	var event struct {
		SessionID string `json:"session_id"`
		State     string `json:"state"`
		TurnCount int    `json:"turn_count"`
		Workspace string `json:"workspace,omitempty"`
	}
	if err := json.Unmarshal(msg.Payload, &event); err != nil {
		o.logger.Error("Failed to parse collaboration result event", "error", err)
		return
	}
	o.logger.Info("Collaboration result",
		"session_id", event.SessionID,
		"state", event.State,
		"turns", event.TurnCount,
	)
}

// handleCollabError handles collaboration error events.
func (o *Orchestrator) handleCollabError(_ context.Context, msg *models.BusMessage) {
	var event struct {
		SessionID string `json:"session_id"`
		Error     string `json:"error"`
		Phase     string `json:"phase,omitempty"`
	}
	if err := json.Unmarshal(msg.Payload, &event); err != nil {
		o.logger.Error("Failed to parse collaboration error event", "error", err)
		return
	}
	o.logger.Error("Collaboration error",
		"session_id", event.SessionID,
		"phase", event.Phase,
		"error", event.Error,
	)
}

// handleCollabRequested handles agent-initiated collaboration request events.
func (o *Orchestrator) handleCollabRequested(_ context.Context, msg *models.BusMessage) {
	var event struct {
		ParentSessionID string   `json:"parent_session_id"`
		SessionID       string   `json:"session_id"`
		Mode            string   `json:"mode"`
		TaskDescription string   `json:"task_description"`
		PreferredAgents []string `json:"preferred_agents"`
	}
	if err := json.Unmarshal(msg.Payload, &event); err != nil {
		o.logger.Error("Failed to parse collaboration requested event", "error", err)
		return
	}
	o.logger.Info("Collaboration requested by agent",
		"parent_session_id", event.ParentSessionID,
		"session_id", event.SessionID,
		"mode", event.Mode,
		"preferred_agents", event.PreferredAgents,
	)
}

// handleTeamResult handles team completion events published by the TeamOrchestrator.
func (o *Orchestrator) handleTeamResult(_ context.Context, msg *models.BusMessage) {
	var event TeamSessionState
	if err := json.Unmarshal(msg.Payload, &event); err != nil {
		o.logger.Error("Failed to parse team result event", "error", err)
		return
	}

	o.logger.Info("Team session completed",
		"session_id", event.SessionID,
		KeyTaskID, event.TaskID,
		"lead", event.LeadAgent,
		"phase", event.Phase,
		"members", len(event.Roster),
	)
}

// handleTeamError handles team error events published by the TeamOrchestrator.
func (o *Orchestrator) handleTeamError(_ context.Context, msg *models.BusMessage) {
	var event struct {
		SessionID string `json:"session_id"`
		Error     string `json:"error"`
	}
	if err := json.Unmarshal(msg.Payload, &event); err != nil {
		o.logger.Error("Failed to parse team error event", "error", err)
		return
	}

	o.logger.Error("Team session error",
		"session_id", event.SessionID,
		"error", event.Error,
	)
}
