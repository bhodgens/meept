package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/queue"
	"github.com/caimlas/meept/internal/task"
	"github.com/caimlas/meept/internal/validator"
	"github.com/caimlas/meept/pkg/models"
)

// StepJobPayload is the payload stored in a queue job for a task step.
type StepJobPayload struct {
	StepID               string   `json:"step_id"`
	TaskID               string   `json:"task_id"`
	Description          string   `json:"description"`
	ToolHint             string   `json:"tool_hint,omitempty"`
	MemoryRefs           []string `json:"memory_refs,omitempty"`
	AccumulatedContext   string   `json:"accumulated_context,omitempty"`
	ValidationRetryCount int      `json:"validation_retry_count,omitempty"`
}

// TacticalScheduler schedules ready steps as queue jobs and handles completion callbacks.
type TacticalScheduler struct {
	stepStore              *task.StepStore
	taskStore              *task.Store
	queue                  queue.Queue
	registry               *AgentRegistry
	bus                    *bus.MessageBus
	pairManager            *PairManager
	reviewManager          *ReviewManager
	validatorManager       *validator.ValidatorManager
	escalationManager      *EscalationManager
	logger                 *slog.Logger
	globalSemaphore        chan struct{}            // Global execution limit
	agentSemaphore         map[string]chan struct{} // Per-agent concurrency slots
	semaphoreMu            sync.Mutex               // Protects agentSemaphore map
	validationGateInterval int                      // Run validation gate every N steps
	validationGateCounter  map[string]int           // Per-task validation gate counter
	validationGateMu       sync.Mutex               // Protects validationGateCounter
}

// TacticalSchedulerConfig holds configuration for the tactical scheduler.
type TacticalSchedulerConfig struct {
	StepStore              *task.StepStore
	TaskStore              *task.Store
	Queue                  queue.Queue
	Registry               *AgentRegistry
	Bus                    *bus.MessageBus
	PairManager            *PairManager
	ReviewManager          *ReviewManager
	ValidatorManager       *validator.ValidatorManager
	EscalationManager      *EscalationManager
	Logger                 *slog.Logger
	MaxConcurrentJobs      int // Global concurrent job limit (default: 10)
	MaxConcurrentPerAgent  int // Per-agent concurrent job limit (default: 3)
	ValidationGateInterval int // Run validation gate every N steps (default: 3, 0 to disable)
}

// NewTacticalScheduler creates a new tactical scheduler.
func NewTacticalScheduler(cfg TacticalSchedulerConfig) *TacticalScheduler {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}

	// Set defaults for concurrency limits
	maxConcurrentJobs := cfg.MaxConcurrentJobs
	if maxConcurrentJobs <= 0 {
		maxConcurrentJobs = 10
	}
	maxConcurrentPerAgent := cfg.MaxConcurrentPerAgent
	if maxConcurrentPerAgent <= 0 {
		maxConcurrentPerAgent = 3
	}
	// Set default validation gate interval (every 3 steps)
	validationGateInterval := cfg.ValidationGateInterval
	if validationGateInterval <= 0 {
		validationGateInterval = 3
	}

	// Initialize semaphores
	globalSemaphore := make(chan struct{}, maxConcurrentJobs)
	agentSemaphore := make(map[string]chan struct{})

	// Pre-initialize semaphores for known agents
	knownAgents := []string{config.AgentIDCoder, config.AgentIDDebugger, config.AgentIDPlanner, config.AgentIDAnalyst, config.AgentIDCommitter, config.AgentIDScheduler, config.AgentIDChat}
	for _, agentID := range knownAgents {
		agentSemaphore[agentID] = make(chan struct{}, maxConcurrentPerAgent)
	}

	return &TacticalScheduler{
		stepStore:              cfg.StepStore,
		taskStore:              cfg.TaskStore,
		queue:                  cfg.Queue,
		registry:               cfg.Registry,
		bus:                    cfg.Bus,
		pairManager:            cfg.PairManager,
		reviewManager:          cfg.ReviewManager,
		validatorManager:       cfg.ValidatorManager,
		escalationManager:      cfg.EscalationManager,
		logger:                 cfg.Logger,
		globalSemaphore:        globalSemaphore,
		agentSemaphore:         agentSemaphore,
		semaphoreMu:            sync.Mutex{},
		validationGateInterval: validationGateInterval,
		validationGateCounter:  make(map[string]int),
		validationGateMu:       sync.Mutex{},
	}
}

// ScheduleReadySteps finds ready steps for a task and enqueues them as jobs.
// Steps that cannot be scheduled due to semaphore limits remain in "ready" state
// and will be retried on the next scheduling cycle.
func (ts *TacticalScheduler) ScheduleReadySteps(ctx context.Context, taskID string) error {
	readySteps, err := ts.stepStore.GetReadySteps(taskID)
	if err != nil {
		return fmt.Errorf("failed to get ready steps: %w", err)
	}

	if len(readySteps) == 0 {
		ts.logger.Debug("No ready steps to schedule", "task_id", taskID)
		return nil
	}

	ts.logger.Info("Scheduling ready steps",
		"task_id", taskID,
		"count", len(readySteps),
	)

	scheduledCount := 0
	semaphoreBlockedCount := 0

	for _, step := range readySteps {
		// Skip steps managed by a pair session -- the PairManager drives them
		if ts.pairManager != nil {
			if _, isPair := ts.pairManager.GetSessionByStep(step.ID); isPair {
				ts.logger.Debug("Skipping pair-managed step in tactical scheduling",
					"step_id", step.ID,
					KeyTaskID, taskID,
				)
				continue
			}
		}

		if err := ts.scheduleStep(ctx, step); err != nil {
			// Check if this was a semaphore block (expected, not an error)
			if strings.Contains(err.Error(), "no available execution slot") {
				semaphoreBlockedCount++
				ts.logger.Debug("Step blocked due to execution limit",
					"step_id", step.ID,
					"task_id", taskID,
					"agent_id", step.AgentID,
				)
				continue
			}
			ts.logger.Error("Failed to schedule step",
				"step_id", step.ID,
				"task_id", taskID,
				"error", err,
			)
			continue
		}
		scheduledCount++
	}

	ts.logger.Debug("Scheduling complete",
		"task_id", taskID,
		"scheduled", scheduledCount,
		"blocked_by_semaphore", semaphoreBlockedCount,
		"total_ready", len(readySteps),
	)

	// Publish progress event with current step info (chat_visible=true so UI displays in chat)
	currentStepDesc := ""
	if scheduledCount > 0 {
		for _, step := range readySteps {
			if step.State == task.StepScheduled {
				currentStepDesc = step.Description
				break
			}
		}
	}
	ts.publishEvent("task.progress", map[string]any{
		KeyTaskID:         taskID,
		"scheduled_steps": scheduledCount,
		"current_step":    currentStepDesc,
		KeyChatVisible:    true,
		KeyTokenUsage:     0, // No token data available at scheduling time
	})

	return nil
}

// scheduleStep creates a queue job for a single step.
// It validates that all dependencies are satisfied before scheduling.
// Returns errSemaphoreUnavailable if no semaphore slot is available.
func (ts *TacticalScheduler) scheduleStep(ctx context.Context, step *task.TaskStep) error {
	// Validate dependencies before scheduling (defense in depth)
	if len(step.DependsOn) > 0 {
		allSteps, err := ts.stepStore.ListByTaskID(step.TaskID)
		if err != nil {
			return fmt.Errorf("failed to list steps for dependency check: %w", err)
		}

		stateMap := make(map[string]task.StepState)
		for _, s := range allSteps {
			stateMap[s.ID] = s.State
		}

		for _, depID := range step.DependsOn {
			depState, ok := stateMap[depID]
			if !ok {
				ts.logger.Warn("Step dependency not found, skipping schedule",
					"step_id", step.ID,
					"missing_dep", depID,
				)
				return fmt.Errorf("dependency %s not found", depID)
			}
			if !depState.IsTerminal() {
				ts.logger.Warn("Step dependency not terminal, skipping schedule",
					"step_id", step.ID,
					"dep_id", depID,
					"dep_state", depState,
				)
				return fmt.Errorf("dependency %s not terminal (state: %s)", depID, depState)
			}
			if depState == task.StepFailed {
				ts.logger.Warn("Step dependency failed, skipping schedule",
					"step_id", step.ID,
					"dep_id", depID,
				)
				return fmt.Errorf("dependency %s failed", depID)
			}
		}
	}

	// Select agent based on tool hint
	agentID := ts.selectAgent(step)
	step.AgentID = agentID

	// Acquire semaphore slots (non-blocking)
	if !ts.acquireSlots(agentID) {
		return fmt.Errorf("no available execution slot for agent %s", agentID)
	}

	// Create job payload with step context
	payload := StepJobPayload{
		StepID:             step.ID,
		TaskID:             step.TaskID,
		Description:        step.Description,
		ToolHint:           step.ToolHint,
		MemoryRefs:         step.MemoryRefs,
		AccumulatedContext: step.AccumulatedContext,
	}

	job, err := queue.NewJob(queue.JobTypeProjectTask, payload)
	if err != nil {
		ts.releaseSlots(agentID)
		return fmt.Errorf("failed to create job: %w", err)
	}

	job.WithTaskID(step.TaskID).
		WithAgentID(agentID)

	// Enqueue the job
	if err := ts.queue.Enqueue(ctx, job); err != nil {
		ts.releaseSlots(agentID)
		return fmt.Errorf("failed to enqueue job: %w", err)
	}

	// Update step state, agent, and job reference
	if err := ts.stepStore.SetAgentID(step.ID, agentID); err != nil {
		ts.logger.Error("Failed to set step agent_id", "step_id", step.ID, "error", err)
	}
	if err := ts.stepStore.SetJobID(step.ID, job.ID); err != nil {
		ts.logger.Error("Failed to set step job_id", "step_id", step.ID, "error", err)
	}
	if err := ts.stepStore.SetState(step.ID, task.StepScheduled); err != nil {
		ts.logger.Error("Failed to set step state to scheduled", "step_id", step.ID, "error", err)
	}

	ts.logger.Info("ASSIGN step scheduled",
		"step_id", step.ID,
		"job_id", job.ID,
		"agent_id", agentID,
		"task_id", step.TaskID,
		"tool_hint", step.ToolHint,
		"description", truncateString(step.Description, 80),
	)

	return nil
}

// acquireSlots attempts to acquire both global and per-agent semaphore slots.
// Returns true if both acquired, false otherwise (no blocking).
func (ts *TacticalScheduler) acquireSlots(agentID string) bool {
	// Get or create per-agent semaphore with single lock hold
	ts.semaphoreMu.Lock()
	defer ts.semaphoreMu.Unlock()

	agentSem, ok := ts.agentSemaphore[agentID]
	if !ok {
		// Create semaphore for unknown agents
		maxPerAgent := cap(ts.globalSemaphore) / 3 // Rough heuristic
		if maxPerAgent < 1 {
			maxPerAgent = 3
		}
		agentSem = make(chan struct{}, maxPerAgent)
		ts.agentSemaphore[agentID] = agentSem
	}
	// Note: We keep the lock held until after semaphore acquisition to prevent races
	// Try to acquire global slot (non-blocking)
	select {
	case ts.globalSemaphore <- struct{}{}:
		// Got global slot
	default:
		return false // Global semaphore full
	}

	// Try to acquire per-agent slot (non-blocking)
	select {
	case agentSem <- struct{}{}:
		// Got agent slot
	default:
		<-ts.globalSemaphore // Release global slot
		return false         // Agent semaphore full
	}

	return true
}

// releaseSlots releases both global and per-agent semaphore slots.
func (ts *TacticalScheduler) releaseSlots(agentID string) {
	ts.semaphoreMu.Lock()
	agentSem := ts.agentSemaphore[agentID]
	ts.semaphoreMu.Unlock()

	// Release per-agent slot
	if agentSem != nil {
		select {
		case <-agentSem:
		default:
		}
	}

	// Release global slot
	select {
	case <-ts.globalSemaphore:
	default:
	}
}

// OnJobCompleted handles a completed job by updating the step, promoting
// newly unblocked steps, and checking task completion.
func (ts *TacticalScheduler) OnJobCompleted(ctx context.Context, jobID string, result json.RawMessage) error {
	startTime := time.Now()

	// Find step by job ID
	step, err := ts.stepStore.GetByJobID(jobID)
	if err != nil {
		return fmt.Errorf("failed to find step for job %s: %w", jobID, err)
	}
	if step == nil {
		ts.logger.Debug("No step found for completed job", "job_id", jobID)
		return nil // Not a step-backed job, ignore
	}

	// Check if this step belongs to a pair session
	if ts.pairManager != nil {
		if session, isPair := ts.pairManager.GetSessionByStep(step.ID); isPair {
			ts.logger.Info("Pair-managed step completed, delegating to PairManager",
				"step_id", step.ID,
				KeyTaskID, step.TaskID,
				"session_id", session.ID,
			)

			// Release semaphore slots
			ts.releaseSlots(step.AgentID)

			// Store result
			resultStr := ""
			if result != nil {
				resultStr = string(result)
			}
			if err := ts.stepStore.SetResult(step.ID, resultStr); err != nil {
				ts.logger.Error("Failed to set pair step result", "step_id", step.ID, "error", err)
			}

			// Mark step completed
			if err := ts.stepStore.SetState(step.ID, task.StepCompleted); err != nil {
				ts.logger.Error("Failed to set pair step completed", "step_id", step.ID, "error", err)
			}

			// Run next pair round asynchronously
			go ts.pairManager.RunRound(context.Background(), session.ID)

			return nil
		}
	}

	// Release semaphore slots for this completed job
	defer ts.releaseSlots(step.AgentID)

	// Store the result and extract evidence
	resultStr := ""
	if result != nil {
		resultStr = string(result)
	}
	if err := ts.stepStore.SetResult(step.ID, resultStr); err != nil {
		ts.logger.Error("Failed to set step result", "step_id", step.ID, "error", err)
	} else {
		step.Result = resultStr
	}

	// NEW: Extract evidence from result before validation
	var execResult struct {
		Success  bool              `json:"success"`
		Result   any               `json:"result,omitempty"`
		Error    string            `json:"error,omitempty"`
		Evidence []models.Evidence `json:"evidence,omitempty"`
	}
	if err := json.Unmarshal(result, &execResult); err != nil {
		ts.logger.Debug("Failed to parse execution result", "step_id", step.ID, "error", err)
	}

	// Update step with evidence before validation
	if len(execResult.Evidence) > 0 {
		step.Evidence = execResult.Evidence
		// Persist evidence to step store
		if err := ts.stepStore.Update(step); err != nil {
			ts.logger.Error("Failed to persist step evidence", "step_id", step.ID, "error", err)
		}
		ts.logger.Debug("Extracted evidence from execution result",
			"step_id", step.ID,
			"evidence_count", len(execResult.Evidence),
		)
	}

	// NEW: Validation gate - validate evidence before proceeding
	if ts.validatorManager != nil {
		validationErr := ts.validatorManager.ValidateStep(ctx, step)
		if validationErr != nil {
			ts.logger.Error("Validation failed", "step_id", step.ID, "error", validationErr)
			step.Validated = false
			step.ValidationError = validationErr.Error()

			// Determine max validation retries from policy (default 2)
			maxRetries := 2
			if ts.reviewManager != nil {
				policy := ts.reviewManager.GetValidationPolicy()
				if policy.MaxValidationLoops > 0 {
					maxRetries = policy.MaxValidationLoops - 1 // MaxValidationLoops is total attempts; retries = attempts - 1
				}
			}
			if maxRetries < 1 {
				maxRetries = 1
			}

			if step.ValidationRetryCount < maxRetries {
				// Re-queue step for validation retry
				step.ValidationRetryCount++
				if err := ts.stepStore.Update(step); err != nil {
					ts.logger.Warn("failed to persist step retry count", "step_id", step.ID, "error", err)
				}

				retryPayload := StepJobPayload{
					StepID:               step.ID,
					TaskID:               step.TaskID,
					Description:          step.Description,
					ToolHint:             step.ToolHint,
					MemoryRefs:           step.MemoryRefs,
					AccumulatedContext:   step.AccumulatedContext,
					ValidationRetryCount: step.ValidationRetryCount,
				}
				retryJob, jobErr := queue.NewJob(queue.JobTypeProjectTask, retryPayload)
				if jobErr != nil {
					ts.logger.Error("Failed to create validation-retry job", "step_id", step.ID, "error", jobErr)
					return fmt.Errorf("validation failed and retry job creation failed: %w", validationErr)
				}
				retryJob.WithTaskID(step.TaskID).WithAgentID(step.AgentID)

				if enqueueErr := ts.queue.Enqueue(ctx, retryJob); enqueueErr != nil {
					ts.logger.Error("Failed to enqueue validation-retry job", "step_id", step.ID, "error", enqueueErr)
					return fmt.Errorf("validation failed and retry enqueue failed: %w", validationErr)
				}

				// Reset step state to scheduled for retry
				if err := ts.stepStore.SetState(step.ID, task.StepScheduled); err != nil {
					ts.logger.Error("Failed to reset step state for validation retry", "step_id", step.ID, "error", err)
				}
				if err := ts.stepStore.SetJobID(step.ID, retryJob.ID); err != nil {
					ts.logger.Error("Failed to update step job_id for validation retry", "step_id", step.ID, "error", err)
				}

				ts.logger.Info("Validation retry enqueued",
					"step_id", step.ID,
					"retry_count", step.ValidationRetryCount,
					"max_retries", maxRetries,
				)
				ts.publishEvent("task.validation_retry", map[string]any{
					KeyTaskID:                step.TaskID,
					KeyStepID:                step.ID,
					"retry_count":            step.ValidationRetryCount,
					"max_retries":            maxRetries,
					string(MessageTypeError): validationErr.Error(),
				})
				return nil // Don't proceed to completion; step will be retried
			}

			// Max retries exceeded - mark step as needs_info for human review
			ts.logger.Warn("Validation max retries exceeded",
				"step_id", step.ID,
				"retry_count", step.ValidationRetryCount,
				"max_retries", maxRetries,
			)
			if err := ts.stepStore.Update(step); err != nil {
				ts.logger.Warn("failed to persist step", "step_id", step.ID, "error", err)
			}
			return validationErr // Don't proceed to completion
		}
		step.Validated = true
		step.ValidationError = ""
	}

	// Publish step completed event with details
	ts.publishEvent("task.step_completed", map[string]any{
		KeyTaskID:     step.TaskID,
		KeyStepID:     step.ID,
		"description": step.Description,
		KeyAgentID:    step.AgentID,
		"result":      truncateString(resultStr, 200),
		"state":       string(task.StepCompleted),
		"duration":    time.Since(startTime).String(),
	})

	// Check if review is needed
	if ts.reviewManager != nil && ts.reviewManager.GetPolicy().Enabled {
		// Trigger review process
		ts.logger.Debug("Triggering review for step", "step_id", step.ID)

		// Publish review request event
		ts.publishEvent("step.review_requested", map[string]any{
			KeyStepID:   step.ID,
			KeyTaskID:   step.TaskID,
			"tool_hint": step.ToolHint,
			KeyAgentID:  step.AgentID,
		})

		// Also publish under task.* prefix for backward compatibility with
		// subscribers (TUI, ChatHandler) that subscribe to task.* but not step.*.
		ts.publishEvent("task.review_requested", map[string]any{
			KeyStepID:   step.ID,
			KeyTaskID:   step.TaskID,
			"tool_hint": step.ToolHint,
			KeyAgentID:  step.AgentID,
		})

		// Load task to extract spec for spec-driven review
		var reviewSpec *TaskSpec
		if ts.taskStore != nil {
			if t, err := ts.taskStore.GetByID(step.TaskID); err == nil && t != nil {
				reviewSpec = ExtractSpecFromTask(t)
			}
		}

		// Perform review (synchronously for now)
		reviewResult, err := ts.reviewManager.ReviewStep(ctx, step, reviewSpec)
		if err != nil {
			ts.logger.Error("Review failed", "step_id", step.ID, "error", err)
			// Continue without review - mark as completed
			if err := ts.stepStore.SetState(step.ID, task.StepCompleted); err != nil {
				ts.logger.Error("Failed to set step to completed after review failure", "error", err)
			}
		} else {
			// Handle review result
			if err := ts.handleReviewResult(ctx, step, reviewResult); err != nil {
				ts.logger.Error("Failed to handle review result", "error", err)
			}
		}
	} else {
		// No review manager or review disabled - mark completed directly
		if err := ts.stepStore.SetState(step.ID, task.StepCompleted); err != nil {
			ts.logger.Error("Failed to set step state to completed", "step_id", step.ID, "error", err)
		}
	}

	// Propagate context to next ready steps
	if err := ts.propagateContextToNextSteps(ctx, step); err != nil {
		ts.logger.Error("Failed to propagate context to next steps",
			"step_id", step.ID,
			"error", err,
		)
	}

	// NEW: Run validation gate if interval reached
	ts.runValidationGateIfDue(ctx, step.TaskID)

	// Update parent task's completed jobs counter
	t, err := ts.taskStore.GetByID(step.TaskID)
	if err != nil || t == nil {
		ts.logger.Error("Failed to get parent task", "task_id", step.TaskID, "error", err)
		return nil
	}
	t.CompleteJob()

	// Aggregate token usage from step to parent task
	if step.TokenUsage > 0 {
		t.AddTokenUsage(step.TokenUsage)
		ts.logger.Debug("Aggregated token usage from step to task",
			"step_id", step.ID,
			"step_tokens", step.TokenUsage,
			"task_total_tokens", t.TokenUsage,
		)
	}

	if err := ts.taskStore.Update(t); err != nil {
		ts.logger.Error("Failed to update task after job completion", "error", err)
	}

	// Publish token progress event
	if step.TokenUsage > 0 {
		ts.publishTokenProgress(t)
	}

	// Check for newly unblocked steps (only if step was approved/completed)
	step, err = ts.stepStore.GetByID(step.ID) // Refresh step state
	if err != nil {
		ts.logger.Error("Failed to refresh step state", "step_id", step.ID, "error", err)
		return nil
	}
	if step.State == task.StepCompleted || step.State == task.StepApproved {
		promoted, err := ts.stepStore.PromoteReadySteps(step.TaskID)
		if err != nil {
			ts.logger.Error("Failed to promote ready steps", "error", err)
		}
		if len(promoted) > 0 {
			ts.logger.Info("Promoted newly unblocked steps",
				"task_id", step.TaskID,
				"count", len(promoted),
			)
		}
		// FIX #0024: Always schedule ready steps after job completion (not just newly promoted)
		// This ensures semaphore-blocked steps get re-scheduled when slots free up
		if err := ts.ScheduleReadySteps(ctx, step.TaskID); err != nil {
			ts.logger.Error("Failed to schedule ready steps", "error", err)
		}
	}

	// Check if all steps are completed/approved
	allDone, err := ts.stepStore.AreAllCompleted(step.TaskID)
	if err != nil {
		ts.logger.Error("Failed to check task completion", "error", err)
		return nil
	}

	if allDone {
		// NEW: Task-level validation before marking complete
		steps, err := ts.stepStore.ListByTaskID(step.TaskID)
		if err != nil {
			ts.logger.Error("Failed to list steps for task validation", "error", err)
		} else {
			var validationErrors []string
			for _, s := range steps {
				if s.State.IsSuccessfullyTerminal() && !s.Validated {
					validationErrors = append(validationErrors,
						fmt.Sprintf("step %s completed but not validated", s.ID))
				}
				if s.ValidationError != "" {
					validationErrors = append(validationErrors,
						fmt.Sprintf("step %s has validation error: %s", s.ID, s.ValidationError))
				}
			}
			if len(validationErrors) > 0 {
				ts.logger.Error("Task validation incomplete - blocking completion",
					"task_id", step.TaskID,
					"errors", strings.Join(validationErrors, ", "))
				return fmt.Errorf("task validation incomplete: %s", strings.Join(validationErrors, ", "))
			}
		}

		// Clean up validation gate counter for completed task
		ts.cleanupValidationGateCounter(step.TaskID)

		t.SetState(task.StateCompleted)
		if err := ts.taskStore.Update(t); err != nil {
			ts.logger.Error("Failed to set task completed", "error", err)
		}

		// Clear escalation tracking for completed task
		if ts.escalationManager != nil {
			ts.escalationManager.ClearEscalation(step.TaskID)
		}

		// Build step summaries for the completion event
		stepSummaries := ts.buildStepSummaries(step.TaskID)
		executionTime := t.ExecutionTime().Round(time.Second).String()
		resultSummary := ts.buildResultSummary(stepSummaries)

		// Extract unique agents used
		agentSet := make(map[string]struct{})
		for _, s := range stepSummaries {
			if agentID, ok := s["agent_id"].(string); ok && agentID != "" {
				agentSet[agentID] = struct{}{}
			}
		}
		agentsUsed := make([]string, 0, len(agentSet))
		for agent := range agentSet {
			agentsUsed = append(agentsUsed, agent)
		}

		ts.publishEvent("task.completed", map[string]any{
			KeyTaskID:         step.TaskID,
			"name":            t.Name,
			KeyCompletedJobs:  t.CompletedJobs,
			KeyTotalJobs:      t.TotalJobs,
			"linked_sessions": t.LinkedSessions,
			"steps":           stepSummaries,
			"execution_time":  executionTime,
			"result":          resultSummary,
			"agents_used":     agentsUsed,
			KeyTokenUsage:     t.TokenUsage,
		})

		ts.logger.Info("Task completed",
			"task_id", step.TaskID,
			"steps_completed", t.CompletedJobs,
			"steps_total", t.TotalJobs,
			"agents_used", agentsUsed,
			"duration", executionTime,
		)
	} else {
		// Get next step description for progress update
		nextStepDesc := ""
		readySteps, _ := ts.stepStore.GetReadySteps(step.TaskID)
		if len(readySteps) > 0 {
			nextStepDesc = readySteps[0].Description
		}

		// Publish progress update (chat_visible=true so UI shows in chat)
		ts.publishEvent("task.progress", map[string]any{
			KeyTaskID:        step.TaskID,
			KeyCompletedJobs: t.CompletedJobs,
			KeyTotalJobs:     t.TotalJobs,
			"current_step":   nextStepDesc,
			KeyChatVisible:   true,
		})
	}

	return nil
}

// handleReviewResult processes a review result and updates step state accordingly.
// It delegates the state-machine logic to ReviewManager.HandleReviewResult
// (the canonical implementation) and then adds tactical-specific side
// effects: promoting and scheduling revision steps through the proper
// dependency-checking flow, and forcing a NeedsInfo step into the completed
// state so the task can proceed while humans optionally intervene.
func (ts *TacticalScheduler) handleReviewResult(ctx context.Context, step *task.TaskStep, result *ReviewResult) error {
	if ts.reviewManager == nil {
		return fmt.Errorf("tactical scheduler has no ReviewManager")
	}

	// Load spec from task for feedback propagation
	var spec *TaskSpec
	if ts.taskStore != nil {
		if t, tErr := ts.taskStore.GetByID(step.TaskID); tErr == nil && t != nil {
			spec = ExtractSpecFromTask(t)
		}
	}

	revisions, err := ts.reviewManager.HandleReviewResult(ctx, step.ID, result, spec)
	if err != nil {
		return err
	}

	// NeedsInfo: tactical forces completion so the overall task can proceed;
	// humans can intervene out-of-band if needed.
	if result.Status == ReviewNeedsInfo {
		if err := ts.stepStore.SetState(step.ID, task.StepCompleted); err != nil {
			ts.logger.Error("Failed to set step to completed", "error", err)
		}
	}

	// If revisions were created, use proper promotion flow to respect dependencies.
	// Revision steps depend on the rejected step (now terminal) and possibly other
	// dependencies from the original step that may not yet be complete.
	if len(revisions) > 0 {
		// Promote any steps that are now ready (all dependencies terminal)
		promoted, err := ts.stepStore.PromoteReadySteps(step.TaskID)
		if err != nil {
			ts.logger.Error("Failed to promote ready steps after review",
				"task_id", step.TaskID,
				"error", err,
			)
		} else if len(promoted) > 0 {
			ts.logger.Info("Promoted steps after review",
				"task_id", step.TaskID,
				"count", len(promoted),
			)
		}

		// Schedule any newly ready steps (may include revisions)
		if err := ts.ScheduleReadySteps(ctx, step.TaskID); err != nil {
			ts.logger.Error("Failed to schedule ready steps after review",
				"task_id", step.TaskID,
				"error", err,
			)
		}
	}

	return nil
}

// OnJobFailed handles a failed job by updating the step and potentially
// marking the task as failed.
func (ts *TacticalScheduler) OnJobFailed(ctx context.Context, jobID, jobErr string) error {
	step, err := ts.stepStore.GetByJobID(jobID)
	if err != nil {
		return fmt.Errorf("failed to find step for job %s: %w", jobID, err)
	}
	if step == nil {
		return nil // Not a step-backed job
	}

	// Check if this step belongs to a pair session
	if ts.pairManager != nil {
		if session, isPair := ts.pairManager.GetSessionByStep(step.ID); isPair {
			ts.logger.Warn("Pair-managed step failed",
				"step_id", step.ID,
				KeyTaskID, step.TaskID,
				"session_id", session.ID,
				"error", jobErr,
			)

			// Release semaphore slots
			ts.releaseSlots(step.AgentID)

			// Mark step failed and session failed
			if err := ts.stepStore.SetResult(step.ID, jobErr); err != nil {
				ts.logger.Error("Failed to set pair step error", "step_id", step.ID, "error", err)
			}
			if err := ts.stepStore.SetState(step.ID, task.StepFailed); err != nil {
				ts.logger.Error("Failed to set pair step failed", "step_id", step.ID, "error", err)
			}
			session.MarkFailed()

			return nil
		}
	}

	// Release semaphore slots for this failed job
	defer ts.releaseSlots(step.AgentID)

	// Publish error to chat immediately (not silent)
	ts.publishEvent("task.error", map[string]any{
		KeyTaskID:                step.TaskID,
		KeyStepID:                step.ID,
		string(MessageTypeError): jobErr,
		KeyChatVisible:           true, // Errors always visible
	})

	// Check if this is a retryable error (rate limit or transient failure)
	if ts.isRetryableError(jobErr) {
		// Get the job from queue
		job, err := ts.queue.Get(ctx, jobID)
		switch {
		case err != nil:
			ts.logger.Error("Failed to get job for retry", "job_id", jobID, "error", err)
		case job != nil && job.CanRetry():
			// Determine retry reason
			reason := "transient_error"
			if ts.isRateLimitError(jobErr) {
				reason = "rate_limit"
			}

			// Retry with exponential backoff
			ts.logger.Info("Retryable error detected, retrying job with backoff",
				"job_id", jobID,
				"step_id", step.ID,
				"retry_count", job.RetryCount+1,
				"reason", reason,
			)
			if err := ts.queue.Retry(ctx, jobID); err != nil {
				ts.logger.Error("Failed to retry job", "job_id", jobID, "error", err)
			} else {
				// Reset step state to scheduled for retry
				if err := ts.stepStore.SetState(step.ID, task.StepScheduled); err != nil {
					ts.logger.Error("Failed to reset step state for retry", "step_id", step.ID, "error", err)
				}
				// Clear error result since we're retrying
				if err := ts.stepStore.SetResult(step.ID, ""); err != nil {
					ts.logger.Error("Failed to clear step result for retry", "step_id", step.ID, "error", err)
				} else {
					step.Result = ""
				}
				// Publish retry event
				ts.publishEvent("queue.job.retry", map[string]any{
					"job_id": jobID,
					"reason": reason,
				})
				return nil // Job has been requeued, don't mark as failed
			}
		default:
			ts.logger.Warn("Job cannot be retried",
				"job_id", jobID,
				"step_id", step.ID,
				"can_retry", job != nil && job.CanRetry(),
				"error", jobErr,
			)
		}
	}

	// Mark step failed
	if err := ts.stepStore.SetResult(step.ID, jobErr); err != nil {
		ts.logger.Error("Failed to set step error result", "step_id", step.ID, "error", err)
	} else {
		step.Result = jobErr
	}
	if err := ts.stepStore.SetState(step.ID, task.StepFailed); err != nil {
		ts.logger.Error("Failed to set step state to failed", "step_id", step.ID, "error", err)
	}

	// Update parent task's failed jobs counter
	t, err := ts.taskStore.GetByID(step.TaskID)
	if err != nil || t == nil {
		ts.logger.Error("Failed to get parent task", "task_id", step.TaskID, "error", err)
		return nil
	}
	t.FailJob()
	if err := ts.taskStore.Update(t); err != nil {
		ts.logger.Error("Failed to update task after job failure", "error", err)
	}

	// Trigger escalation for failed step if escalation manager is configured.
	// The escalation manager may re-plan the task or request human intervention.
	if ts.escalationManager != nil {
		failureCtx := FailureContext{
			TaskID:    step.TaskID,
			StepID:    step.ID,
			AgentID:   step.AgentID,
			Error:     jobErr,
			Stage:     "execution",
			Timestamp: time.Now(),
		}
		if escalErr := ts.escalationManager.Escalate(ctx, failureCtx); escalErr != nil {
			ts.logger.Warn("Escalation failed",
				"task_id", step.TaskID,
				"step_id", step.ID,
				"error", escalErr,
			)
		}
	}

	// Check if all paths are blocked (no more pending/ready steps that don't
	// transitively depend on the failed step)
	allSteps, err := ts.stepStore.ListByTaskID(step.TaskID)
	if err != nil {
		ts.logger.Error("Failed to list steps for failure check", "error", err)
		return nil
	}

	hasLiveSteps := false
	for _, s := range allSteps {
		if s.State == task.StepRunning || s.State == task.StepScheduled ||
			s.State == task.StepReady {
			hasLiveSteps = true
			break
		}
	}

	// Check if any pending steps can still be promoted
	if !hasLiveSteps {
		promoted, _ := ts.stepStore.PromoteReadySteps(step.TaskID)
		hasLiveSteps = len(promoted) > 0
	}

	if !hasLiveSteps {
		// No more work can be done, mark task as failed
		t.SetState(task.StateFailed)
		if err := ts.taskStore.Update(t); err != nil {
			ts.logger.Error("Failed to set task failed", "error", err)
		}

		// Clean up validation gate counter for failed task
		ts.cleanupValidationGateCounter(step.TaskID)

		ts.publishEvent("task.failed", map[string]any{
			KeyTaskID:                step.TaskID,
			"name":                   t.Name,
			"failed_jobs":            t.FailedJobs,
			KeyCompletedJobs:         t.CompletedJobs,
			KeyTotalJobs:             t.TotalJobs,
			"failed_step":            step.ID,
			string(MessageTypeError): jobErr,
			"linked_sessions":        t.LinkedSessions,
		})

		ts.logger.Info("Task failed - no remaining live steps",
			"task_id", step.TaskID,
			"failed", t.FailedJobs,
			"completed", t.CompletedJobs,
			"total", t.TotalJobs,
		)
	} else {
		// Task is still partially alive - get next step for progress
		nextStepDesc := ""
		readySteps, _ := ts.stepStore.GetReadySteps(step.TaskID)
		if len(readySteps) > 0 {
			nextStepDesc = readySteps[0].Description
		}

		ts.publishEvent("task.progress", map[string]any{
			KeyTaskID:        step.TaskID,
			"failed_jobs":    t.FailedJobs,
			KeyCompletedJobs: t.CompletedJobs,
			KeyTotalJobs:     t.TotalJobs,
			"current_step":   nextStepDesc,
			KeyChatVisible:   true,
		})
	}

	return nil
}

// selectAgent maps a step's ToolHint to the appropriate agent ID.
func (ts *TacticalScheduler) selectAgent(step *task.TaskStep) string {
	switch step.ToolHint {
	case string(IntentCode), KeywordRefactor:
		return config.AgentIDCoder
	case string(IntentDebug), KeywordFix:
		return config.AgentIDDebugger
	case string(IntentAnalyze), string(IntentResearch):
		return config.AgentIDAnalyst
	case string(IntentGit), KeywordCommit:
		return config.AgentIDCommitter
	case string(IntentSchedule):
		return config.AgentIDScheduler
	case string(IntentPlan):
		return config.AgentIDPlanner
	default:
		return config.AgentIDChat
	}
}

func (ts *TacticalScheduler) publishEvent(topic string, data map[string]any) {
	if ts.bus == nil {
		return
	}

	msg, err := models.NewBusMessage(models.MessageTypeEvent, "tactical-scheduler", data)
	if err != nil {
		ts.logger.Error("Failed to create bus message", "error", err)
		return
	}

	ts.bus.Publish(topic, msg)
}

// publishTokenProgress publishes a task.progress event with token_usage data.
func (ts *TacticalScheduler) publishTokenProgress(t *task.Task) {
	ts.publishEvent("task.progress", map[string]any{
		KeyTaskID:        t.ID,
		KeyCompletedJobs: t.CompletedJobs,
		KeyTotalJobs:     t.TotalJobs,
		KeyTokenUsage:    t.TokenUsage,
	})
}

// isRateLimitError checks if an error message indicates a rate limit error.
func (ts *TacticalScheduler) isRateLimitError(errMsg string) bool {
	// Use the llm package helper if available
	// First try to see if we can parse it as an LLM error
	return llm.IsRateLimitErrorMessage(errMsg)
}

// isRetryableError checks if an error is transient and worth retrying.
// This includes rate limits, timeouts, network errors, and other temporary failures.
func (ts *TacticalScheduler) isRetryableError(errMsg string) bool {
	// Non-retryable errors should never be retried (FIX #0042)
	if strings.Contains(errMsg, "token budget exceeded") ||
		strings.Contains(errMsg, "budget exceeded") ||
		strings.Contains(errMsg, "context size") ||
		strings.Contains(errMsg, "context window") {
		return false
	}

	// Always retry rate limits
	if ts.isRateLimitError(errMsg) {
		return true
	}

	// Check for transient error patterns
	transientPatterns := []string{
		"timeout",
		"temporary",
		"connection refused",
		"connection reset",
		"broken pipe",
		"network",
		"busy",
		"lock",
		"deadlock",
		"unavailable",
		"try again later",
	}

	lowerErr := strings.ToLower(errMsg)
	for _, pattern := range transientPatterns {
		if strings.Contains(lowerErr, pattern) {
			return true
		}
	}

	return false
}

// runValidationGateIfDue increments the validation counter for a task and runs
// the validation gate if the interval has been reached.
func (ts *TacticalScheduler) runValidationGateIfDue(ctx context.Context, taskID string) {
	ts.validationGateMu.Lock()
	defer ts.validationGateMu.Unlock()

	if ts.validationGateInterval <= 0 {
		return // Validation gate disabled
	}

	ts.validationGateCounter[taskID]++
	if ts.validationGateCounter[taskID] >= ts.validationGateInterval {
		// Run validation gate
		if err := ts.runValidationGate(ctx, taskID); err != nil {
			ts.logger.Warn("Validation gate detected issues",
				"task_id", taskID,
				"error", err,
			)
			// Don't block execution - just log warning as per design
		}
		// Reset counter after running gate
		ts.validationGateCounter[taskID] = 0
	}
}

// runValidationGate checks all completed steps for a task are validated.
// Returns error if any completed step lacks validation.
func (ts *TacticalScheduler) runValidationGate(_ context.Context, taskID string) error {
	steps, err := ts.stepStore.ListByTaskID(taskID)
	if err != nil {
		return fmt.Errorf("failed to list steps for validation gate: %w", err)
	}

	var unvalidatedSteps []string
	for _, step := range steps {
		if step.State.IsSuccessfullyTerminal() && !step.Validated {
			unvalidatedSteps = append(unvalidatedSteps, step.ID)
		}
		if step.ValidationError != "" {
			ts.logger.Warn("Step has validation error",
				"step_id", step.ID,
				"error", step.ValidationError,
			)
		}
	}

	if len(unvalidatedSteps) > 0 {
		return fmt.Errorf("validation gate: %d completed steps not validated: %v",
			len(unvalidatedSteps), unvalidatedSteps)
	}

	ts.logger.Debug("Validation gate passed",
		"task_id", taskID,
		"steps_checked", len(steps),
	)
	return nil
}

// buildStepSummaries creates an array of step summaries for task completion events.
func (ts *TacticalScheduler) buildStepSummaries(taskID string) []map[string]any {
	allSteps, err := ts.stepStore.ListByTaskID(taskID)
	if err != nil {
		ts.logger.Error("Failed to list steps for summary", "error", err)
		return nil
	}

	summaries := make([]map[string]any, len(allSteps))
	for i, s := range allSteps {
		summaries[i] = map[string]any{
			"id":                  s.ID,
			"description":         s.Description,
			"state":               string(s.State),
			"result":              truncateString(s.Result, 100),
			KeyAgentID:            s.AgentID,
			"accumulated_context": truncateString(s.AccumulatedContext, 200),
		}
	}
	return summaries
}

// buildResultSummary creates a human-readable summary of what was accomplished.
func (ts *TacticalScheduler) buildResultSummary(steps []map[string]any) string {
	if len(steps) == 0 {
		return "Task completed."
	}

	var sb strings.Builder
	completedCount := 0
	for _, s := range steps {
		if s["state"] == string(task.StepCompleted) || s["state"] == string(task.StepApproved) {
			completedCount++
		}
	}

	fmt.Fprintf(&sb, "Completed %d/%d steps: ", completedCount, len(steps))

	// List the first few completed step descriptions
	shown := 0
	for _, s := range steps {
		if shown >= 3 {
			sb.WriteString("...")
			break
		}
		if s["state"] == string(task.StepCompleted) || s["state"] == string(task.StepApproved) {
			if shown > 0 {
				sb.WriteString(", ")
			}
			desc := s["description"].(string)
			sb.WriteString(truncateString(desc, 40))
			shown++
		}
	}

	return sb.String()
}

// propagateContextToNextSteps copies completed step's result and MemoryRefs to next ready steps.
func (ts *TacticalScheduler) propagateContextToNextSteps(_ context.Context, completedStep *task.TaskStep) error {
	// Get next ready steps
	readySteps, err := ts.stepStore.GetReadySteps(completedStep.TaskID)
	if err != nil {
		return fmt.Errorf("failed to get ready steps: %w", err)
	}
	if len(readySteps) == 0 {
		return nil // No steps to propagate to
	}

	// Build context content from completed step
	contextContent := fmt.Sprintf("## Step completed: %s\n\n**Result:** %s",
		completedStep.Description,
		truncateString(completedStep.Result, 500),
	)

	// Append context and copy MemoryRefs to each ready step
	for _, step := range readySteps {
		// Copy MemoryRefs from completed step
		for _, ref := range completedStep.MemoryRefs {
			step.AddMemoryRef(ref)
		}

		// Append to accumulated context
		step.AppendToContext(contextContent)

		// Persist updates
		if err := ts.stepStore.Update(step); err != nil {
			ts.logger.Error("Failed to update step context",
				"step_id", step.ID,
				"error", err,
			)
		}
	}

	ts.logger.Info("Propagated context to next steps",
		"step_id", completedStep.ID,
		"next_steps", len(readySteps),
	)

	return nil
}

// cleanupValidationGateCounter removes the validation gate counter entry for a task.
// Called when a task completes or fails to prevent unbounded map growth.
func (ts *TacticalScheduler) cleanupValidationGateCounter(taskID string) {
	ts.validationGateMu.Lock()
	defer ts.validationGateMu.Unlock()
	delete(ts.validationGateCounter, taskID)
}

// HandoffRequest represents a handoff request payload from the request_handoff tool.
type HandoffRequest struct {
	TaskID        string `json:"task_id"`
	FromStepID    string `json:"from_step_id"`
	FromAgentID   string `json:"from_agent_id"`
	ToAgentID     string `json:"to_agent_id"`
	Description   string `json:"description"`
	ToolHint      string `json:"tool_hint,omitempty"`
	Reason        string `json:"reason,omitempty"`
	PartialResult string `json:"partial_result,omitempty"`
	InjectAfter   bool   `json:"inject_after"`
}

// HandleHandoff processes a handoff request: creates a new task step for the target
// agent, optionally rewires downstream dependencies, and publishes a bus event.
func (ts *TacticalScheduler) HandleHandoff(ctx context.Context, msg *models.BusMessage) error {
	// 1. Parse payload
	var req HandoffRequest
	if err := json.Unmarshal(msg.Payload, &req); err != nil {
		return fmt.Errorf("failed to parse handoff request: %w", err)
	}

	// 2. Validate task exists
	t, err := ts.taskStore.GetByID(req.TaskID)
	if err != nil || t == nil {
		return fmt.Errorf("task %s not found: %w", req.TaskID, err)
	}

	// 3. Validate originating step exists (warn but continue if not found)
	fromStep, err := ts.stepStore.GetByID(req.FromStepID)
	if err != nil {
		ts.logger.Warn("Originating step not found for handoff, continuing",
			KeyStepID, req.FromStepID,
			KeyTaskID, req.TaskID,
		)
	}

	// 4. Derive tool_hint from target agent if not provided
	toolHint := req.ToolHint
	if toolHint == "" {
		toolHint = agentIDToToolHint(req.ToAgentID)
	}

	// 5. Build accumulated context from handoff request
	var contextParts []string
	if req.FromAgentID != "" || req.PartialResult != "" {
		fromLabel := req.FromAgentID
		if fromLabel == "" {
			fromLabel = "unknown agent"
		}
		if req.PartialResult != "" {
			contextParts = append(contextParts, fmt.Sprintf("[Handoff from %s]: %s", fromLabel, req.PartialResult))
		} else {
			contextParts = append(contextParts, fmt.Sprintf("[Handoff from %s]", fromLabel))
		}
	}
	if req.Reason != "" {
		contextParts = append(contextParts, fmt.Sprintf("Reason: %s", req.Reason))
	}
	accumulatedContext := strings.Join(contextParts, "\n")

	// 6. Create new task step
	// Use a high sequence number so the step sorts after existing steps
	sequence := int(9000 + time.Now().UnixNano()%1000)
	newStep := task.NewTaskStep(req.TaskID, req.Description, sequence)
	newStep.ToolHint = toolHint
	newStep.AccumulatedContext = accumulatedContext

	// Set dependency on from step if inject_after and step exists
	if req.InjectAfter && fromStep != nil {
		newStep.DependsOn = []string{req.FromStepID}
	}

	// 7. Persist
	if err := ts.stepStore.Create(newStep); err != nil {
		return fmt.Errorf("failed to create handoff step: %w", err)
	}

	// 8. Update task's TotalJobs count
	t.TotalJobs++
	if err := ts.taskStore.Update(t); err != nil {
		ts.logger.Error("Failed to update task TotalJobs after handoff",
			KeyTaskID, req.TaskID,
			"error", err,
		)
	}

	// 9. Rewire downstream dependencies (Task 5)
	if fromStep != nil && req.InjectAfter {
		if err := ts.rewireDownstreamDeps(req.TaskID, req.FromStepID, newStep.ID); err != nil {
			ts.logger.Error("Failed to rewire downstream dependencies",
				KeyTaskID, req.TaskID,
				KeyStepID, newStep.ID,
				"error", err,
			)
		}
	}

	// 10. Promote ready steps
	promoted, err := ts.stepStore.PromoteReadySteps(req.TaskID)
	if err != nil {
		ts.logger.Error("Failed to promote ready steps after handoff",
			KeyTaskID, req.TaskID,
			"error", err,
		)
	}

	// 11. Schedule if steps were promoted
	if len(promoted) > 0 {
		if err := ts.ScheduleReadySteps(ctx, req.TaskID); err != nil {
			ts.logger.Error("Failed to schedule ready steps after handoff",
				KeyTaskID, req.TaskID,
				"error", err,
			)
		}
	}

	// 12. Publish event
	ts.publishEvent("task.handoff_created", map[string]any{
		KeyTaskID:    req.TaskID,
		KeyStepID:    newStep.ID,
		KeyAgentID:   req.ToAgentID,
		"from_agent": req.FromAgentID,
	})

	ts.logger.Info("Handoff step created",
		KeyTaskID,     req.TaskID,
		KeyStepID,     newStep.ID,
		KeyAgentID,    req.ToAgentID,
		"from_step",   req.FromStepID,
		"description", req.Description,
	)

	return nil
}

// rewireDownstreamDeps replaces the fromStepID dependency with newStepID in all
// downstream steps. This ensures that steps that previously depended on the
// from step now depend on the injected step instead.
func (ts *TacticalScheduler) rewireDownstreamDeps(taskID, fromStepID, newStepID string) error {
	allSteps, err := ts.stepStore.ListByTaskID(taskID)
	if err != nil {
		return fmt.Errorf("failed to list steps for rewiring: %w", err)
	}

	rewired := 0
	for _, ds := range allSteps {
		// Skip the new step and the from step
		if ds.ID == newStepID || ds.ID == fromStepID {
			continue
		}

		// Check if this step depends on the from step
		found := false
		for i, dep := range ds.DependsOn {
			if dep == fromStepID {
				ds.DependsOn[i] = newStepID
				found = true
				break
			}
		}

		if found {
			if err := ts.stepStore.Update(ds); err != nil {
				ts.logger.Error("Failed to update downstream step dependency",
					KeyStepID, ds.ID,
					"error", err,
				)
				continue
			}
			rewired++
			ts.logger.Info("Rewired downstream dependency",
				KeyStepID,    ds.ID,
				"old_dep",    fromStepID,
				"new_dep",    newStepID,
				"task_id",    taskID,
			)
		}
	}

	if rewired > 0 {
		ts.logger.Info("Rewired downstream dependencies",
			KeyTaskID,    taskID,
			"count",      rewired,
			"from_step",  fromStepID,
			"new_step",   newStepID,
		)
	}

	return nil
}

// agentIDToToolHint maps an agent ID to the corresponding tool hint/intent type.
func agentIDToToolHint(agentID string) string {
	switch agentID {
	case config.AgentIDCoder:
		return string(IntentCode)
	case config.AgentIDDebugger:
		return string(IntentDebug)
	case config.AgentIDAnalyst:
		return string(IntentAnalyze)
	case config.AgentIDCommitter:
		return string(IntentGit)
	case config.AgentIDScheduler:
		return string(IntentSchedule)
	case config.AgentIDPlanner:
		return string(IntentPlan)
	default:
		return "chat"
	}
}
