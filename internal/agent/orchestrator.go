package agent

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/plan"
	"github.com/caimlas/meept/internal/repomap"
	"github.com/caimlas/meept/pkg/models"
)

// Orchestrator coordinates the strategic and tactical layers via bus subscriptions.
type Orchestrator struct {
	strategic            *StrategicPlanner
	tactical             *TacticalScheduler
	pairManager          *PairManager
	busPairOrchestrator  *PairOrchestrator    // bus-channel-based agent pairing (Option C)
	planManager          *plan.PlanManager    // plan system integration for progress tracking
	bus                   *bus.MessageBus
	logger                *slog.Logger
	collaborationEngine   *CollaborationEngine     // optional: enables agent collaboration modes
	ralphLoop            *RalphLoop               // optional: Ralph loop for auto-replanning
	reflectionEngine     *ReflectionEngine       // optional: auto-fix reflection loop
	repoMapGen           *repomap.RepoMapGenerator // optional: repository map for context enrichment

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

// SetRepoMapGenerator sets the repo map generator for context enrichment.
func (o *Orchestrator) SetRepoMapGenerator(gen *repomap.RepoMapGenerator) {
	o.repoMapGen = gen
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
		"tool.execution.complete": o.handleToolExecutionComplete,
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

// SetReflectionEngine sets the reflection engine for auto-fix loop.
// This is called by the daemon after the ReflectionEngine is created.
func (o *Orchestrator) SetReflectionEngine(reflection *ReflectionEngine) {
	if reflection != nil {
		o.reflectionEngine = reflection
	}
}

// ReflectionEngine returns the reflection engine, if configured.
func (o *Orchestrator) ReflectionEngine() *ReflectionEngine {
	return o.reflectionEngine
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
		var payload StepJobPayload
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

// handleToolExecutionComplete handles tool execution complete events to trigger reflection.
// When the reflection engine is configured and a file edit was executed, this handler
// runs the reflection loop to automatically fix lint/test errors.
func (o *Orchestrator) handleToolExecutionComplete(ctx context.Context, msg *models.BusMessage) {
	if o.reflectionEngine == nil {
		return
	}

	var event struct {
		ToolCallID string `json:"tool_call_id"`
		ToolName   string `json:"tool_name"`
		Success    bool   `json:"success"`
		// EditedFiles contains file paths that were modified by the tool
		EditedFiles []string `json:"edited_files,omitempty"`
	}
	if err := json.Unmarshal(msg.Payload, &event); err != nil {
		o.logger.Debug("Failed to parse tool execution complete event", "error", err)
		return
	}

	// Only trigger reflection for file edit operations
	if event.ToolName != "file_edit" || !event.Success {
		return
	}

	o.logger.Info("File edit completed, running reflection loop",
		"tool_call_id", event.ToolCallID,
		"edited_files", len(event.EditedFiles),
	)

	// Run reflection in a goroutine to not block the message bus
	go func() {
		result, err := o.reflectionEngine.RunReflection(ctx, event.EditedFiles)
		if err != nil {
			o.logger.Error("Reflection failed",
				"tool_call_id", event.ToolCallID,
				"error", err,
			)
			return
		}

		// If a pending fix was generated, apply it to the files and re-run reflection
		if result.PendingFix != nil {
			o.logger.Info("Applying pending reflection fix",
				"tool_call_id", event.ToolCallID,
				"files", len(result.PendingFix.Files),
				"fix_length", len(result.PendingFix.FixText),
			)

			// Apply the fix to the target files
			appliedFiles := o.applyFix(ctx, result.PendingFix)
			if len(appliedFiles) > 0 {
				o.logger.Info("Fix applied, re-running reflection",
					"tool_call_id", event.ToolCallID,
					"applied_to", len(appliedFiles),
				)

				// Re-run reflection on the applied fix to check for remaining errors
				retryResult, err := o.reflectionEngine.RunReflection(ctx, appliedFiles)
				if err != nil {
					o.logger.Warn("Reflection re-check failed",
						"tool_call_id", event.ToolCallID,
						"error", err,
					)
				} else {
					// Merge retry results into the main result
					result.LintErrors = append(result.LintErrors, retryResult.LintErrors...)
					result.TestFailures = append(result.TestFailures, retryResult.TestFailures...)
					if retryResult.PendingFix != nil {
						// Still has issues - apply again (single retry pass to avoid infinite loop)
						o.logger.Info("Second fix pending, applying fix attempt",
							"tool_call_id", event.ToolCallID,
							"iteration", result.Iterations+1,
						)
						appliedFiles = o.applyFix(ctx, retryResult.PendingFix)
						result.PendingFix = retryResult.PendingFix
						if len(appliedFiles) > 0 {
							o.logger.Info("Second fix applied",
								"tool_call_id", event.ToolCallID,
								"files", len(appliedFiles),
							)
						}
					}
					result.Fixed = retryResult.Fixed
					result.Iterations += retryResult.Iterations
					result.FinalMessage = retryResult.FinalMessage
					if retryResult.GaveUp {
						result.GaveUp = retryResult.GaveUp
					}
				}
			} else {
				o.logger.Warn("Failed to apply reflection fix to any files",
					"tool_call_id", event.ToolCallID,
				)
			}
		}

		// Log the outcomes
		if result.Fixed {
			o.logger.Info("Reflection completed successfully",
				"tool_call_id", event.ToolCallID,
				"iterations", result.Iterations,
				"message", result.FinalMessage,
			)
		} else if result.GaveUp {
			o.logger.Warn("Reflection gave up after applying fixes",
				"tool_call_id", event.ToolCallID,
				"iterations", result.Iterations,
				"lint_errors", len(result.LintErrors),
				"test_failures", len(result.TestFailures),
				"message", result.FinalMessage,
			)
			// Publish a notification event so other components know about the reflection outcome
			o.publishReflectionEvent(ctx, event.ToolCallID, "reflection_gave_up", result)
		} else {
			o.logger.Debug("Reflection completed with no errors",
				"tool_call_id", event.ToolCallID,
				"iterations", result.Iterations,
			)
		}
	}()
}

// applyFix writes the LLM's proposed fix text to the target files.
// It extracts code from markdown code blocks if present, or writes the
// raw content. Returns the list of files that were successfully written.
func (o *Orchestrator) applyFix(ctx context.Context, fix *FixAttempt) []string {
	if fix == nil || fix.FixText == "" {
		return nil
	}

	// Extract per-file code blocks from markdown.
	blocks := extractCodeBlocksFromMarkdown(fix.FixText)

	var applied []string
	for _, file := range fix.Files {
		// Resolve relative paths
		resolved := file
		if !filepath.IsAbs(file) {
			// Try current working directory first
			if abs, err := filepath.Abs(file); err == nil {
				if _, err2 := os.Stat(abs); err2 == nil {
					resolved = abs
				}
			}
		}

		// Verify file exists before writing
		if _, err := os.Stat(resolved); err != nil {
			o.logger.Debug("Skipping fix application - file not found",
				"file", resolved,
			)
			continue
		}

		// Look up content for this file.
		var content string
		if len(blocks) > 0 {
			// Try full path match first, then basename.
			if c, ok := blocks[file]; ok {
				content = c
			} else if c, ok := blocks[filepath.Base(file)]; ok {
				content = c
			} else if c, ok := blocks[resolved]; ok {
				content = c
			} else if c, ok := blocks[""]; ok {
				// Fallback: unannotated single block.
				content = c
			}
		}
		if content == "" {
			// Final fallback: use the raw fix text.
			content = fix.FixText
		}

		// Attempt to write the fix content to the file
		if err := os.WriteFile(resolved, []byte(content), 0644); err != nil {
			o.logger.Warn("Failed to apply fix to file",
				"file", resolved,
				"error", err,
			)
			continue
		}

		o.logger.Info("Applied fix to file",
			"file", resolved,
			"content_length", len(content),
		)
		applied = append(applied, resolved)
	}

	return applied
}

// extractCodeBlocksFromMarkdown parses ALL ``` code blocks from markdown.
// It looks for file path annotations preceding each block:
//   - "// File: path/to/file.go"
//   - "## path/to/file.go"
//   - "path/to/file.go:"
// Blocks with no annotation are stored under the empty key "".
// Returns a map of filepath -> code content.
func extractCodeBlocksFromMarkdown(markdown string) map[string]string {
	blocks := make(map[string]string)

	// Pattern matches an optional file annotation line, followed by ```...```.
	// Captures: (1) optional file path, (2) optional language specifier, (3) code content.
	re := regexp.MustCompile("(?m)(?:^(?://|##)\\s*[Ff]ile:?\\s*([^\\n]+)\\n|^(\\S[^:]+):\\n)?```(?:\\w+)?\\n(.*?)\\n?```")

	matches := re.FindAllStringSubmatch(markdown, -1)
	if matches == nil {
		return blocks
	}

	for _, match := range matches {
		var filePath string
		var code string

		// match[1] = file path from "// File: ..." or "## File: ..."
		// match[2] = file path from "path/to/file.go:" pattern
		// match[3] = code content
		if len(match) > 1 && strings.TrimSpace(match[1]) != "" {
			filePath = strings.TrimSpace(match[1])
		} else if len(match) > 2 && strings.TrimSpace(match[2]) != "" {
			filePath = strings.TrimSpace(match[2])
		}
		if len(match) > 3 {
			code = match[3]
		}

		if code == "" {
			continue
		}

		if filePath == "" {
			blocks[""] = code
		} else {
			blocks[filePath] = code
		}
	}

	return blocks
}

// extractCodeFromMarkdown tries to extract code content from markdown code blocks.
// It looks for the first ``` block and returns its content.
// If the content is not wrapped in code blocks, it returns an empty string.
func extractCodeFromMarkdown(markdown string) string {
	blocks := extractCodeBlocksFromMarkdown(markdown)
	if code, ok := blocks[""]; ok {
		return code
	}
	// If no unannotated block, return the first block found (if any).
	for _, code := range blocks {
		return code
	}
	return ""
}

// markdownContainsMultipleCodeBlocks returns true if the markdown text contains
// more than one triple-backtick code block.
func markdownContainsMultipleCodeBlocks(markdown string) bool {
	blocks := extractCodeBlocksFromMarkdown(markdown)
	count := len(blocks)
	// A single unannotated block counts as one; multiple distinct keys means multiple blocks.
	if count > 1 {
		return true
	}
	// Check if there are multiple backticks even if parsing didn't yield keys.
	tickCount := 0
	idx := 0
	for {
		i := strings.Index(markdown[idx:], "```")
		if i == -1 {
			break
		}
		tickCount++
		idx += i + 3
	}
	return tickCount > 2
}

// publishReflectionEvent publishes a bus event about reflection results
// so other components (like the agent loop) are aware of the outcome.
func (o *Orchestrator) publishReflectionEvent(ctx context.Context, toolCallID, phase string, result *ReflectionResult) {
	if o.bus == nil {
		return
	}

	payload := map[string]any{
		"tool_call_id": toolCallID,
		"phase":        phase,
		"fixed":        result.Fixed,
		"gave_up":      result.GaveUp,
		"iterations":   result.Iterations,
		"message":      result.FinalMessage,
		"lint_errors":  len(result.LintErrors),
		"test_failures": len(result.TestFailures),
	}

	if result.PendingFix != nil {
		payload["pending_fix"] = map[string]any{
			"prompt":     result.PendingFix.Prompt,
			"fix_length": len(result.PendingFix.FixText),
			"files":      result.PendingFix.Files,
		}
	}

	msg, err := models.NewBusMessage(models.MessageTypeEvent, "orchestrator", payload)
	if err != nil {
		o.logger.Warn("Failed to create reflection event", "error", err)
		return
	}
	o.bus.Publish("reflection.complete", msg)
}

// GenerateRepoMap creates a repository map for context enrichment.
// chatFiles are the files actively being discussed in the conversation.
// mentionedIdentifiers are identifiers (functions, types, etc.) from the conversation.
func (o *Orchestrator) GenerateRepoMap(ctx context.Context, chatFiles, mentionedIdentifiers []string) (*repomap.RenderedMap, error) {
	if o.repoMapGen == nil {
		return nil, nil
	}
	return o.repoMapGen.Generate(ctx, chatFiles, mentionedIdentifiers)
}
