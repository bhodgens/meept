package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/queue"
	"github.com/caimlas/meept/internal/task"
	"github.com/caimlas/meept/pkg/models"
)

// StepJobPayload is the payload stored in a queue job for a task step.
type StepJobPayload struct {
	StepID      string `json:"step_id"`
	TaskID      string `json:"task_id"`
	Description string `json:"description"`
	ToolHint    string `json:"tool_hint,omitempty"`
}

// TacticalScheduler schedules ready steps as queue jobs and handles completion callbacks.
type TacticalScheduler struct {
	stepStore     *task.StepStore
	taskStore     *task.Store
	queue         queue.Queue
	registry      *AgentRegistry
	bus           *bus.MessageBus
	reviewManager *ReviewManager
	logger        *slog.Logger
}

// TacticalSchedulerConfig holds configuration for the tactical scheduler.
type TacticalSchedulerConfig struct {
	StepStore     *task.StepStore
	TaskStore     *task.Store
	Queue         queue.Queue
	Registry      *AgentRegistry
	Bus           *bus.MessageBus
	ReviewManager *ReviewManager
	Logger        *slog.Logger
}

// NewTacticalScheduler creates a new tactical scheduler.
func NewTacticalScheduler(cfg TacticalSchedulerConfig) *TacticalScheduler {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}

	return &TacticalScheduler{
		stepStore:     cfg.StepStore,
		taskStore:     cfg.TaskStore,
		queue:         cfg.Queue,
		registry:      cfg.Registry,
		bus:           cfg.Bus,
		reviewManager: cfg.ReviewManager,
		logger:        cfg.Logger,
	}
}

// ScheduleReadySteps finds ready steps for a task and enqueues them as jobs.
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

	for _, step := range readySteps {
		if err := ts.scheduleStep(ctx, step); err != nil {
			ts.logger.Error("Failed to schedule step",
				"step_id", step.ID,
				"task_id", taskID,
				"error", err,
			)
			continue
		}
	}

	// Publish progress event with current step info
	currentStepDesc := ""
	if len(readySteps) > 0 {
		currentStepDesc = readySteps[0].Description
	}
	ts.publishEvent("task.progress", map[string]any{
		"task_id":         taskID,
		"scheduled_steps": len(readySteps),
		"current_step":    currentStepDesc,
	})

	return nil
}

// scheduleStep creates a queue job for a single step.
func (ts *TacticalScheduler) scheduleStep(ctx context.Context, step *task.TaskStep) error {
	// Select agent based on tool hint
	agentID := ts.selectAgent(step)
	step.AgentID = agentID

	// Create job payload
	payload := StepJobPayload{
		StepID:      step.ID,
		TaskID:      step.TaskID,
		Description: step.Description,
		ToolHint:    step.ToolHint,
	}

	job, err := queue.NewJob(queue.JobTypeProjectTask, payload)
	if err != nil {
		return fmt.Errorf("failed to create job: %w", err)
	}

	job.WithTaskID(step.TaskID).
		WithAgentID(agentID)

	// Enqueue the job
	if err := ts.queue.Enqueue(ctx, job); err != nil {
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

	ts.logger.Info("Step scheduled as job",
		"step_id", step.ID,
		"job_id", job.ID,
		"agent_id", agentID,
		"task_id", step.TaskID,
	)

	return nil
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

	// Store the result
	resultStr := ""
	if result != nil {
		resultStr = string(result)
	}
	if err := ts.stepStore.SetResult(step.ID, resultStr); err != nil {
		ts.logger.Error("Failed to set step result", "step_id", step.ID, "error", err)
	}

	// Publish step completed event with details
	ts.publishEvent("task.step_completed", map[string]any{
		"task_id":     step.TaskID,
		"step_id":     step.ID,
		"description": step.Description,
		"agent_id":    step.AgentID,
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
			"step_id":   step.ID,
			"task_id":   step.TaskID,
			"tool_hint": step.ToolHint,
			"agent_id":  step.AgentID,
		})

		// Perform review (synchronously for now)
		reviewResult, err := ts.reviewManager.ReviewStep(ctx, step)
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

	// Update parent task's completed jobs counter
	t, err := ts.taskStore.GetByID(step.TaskID)
	if err != nil || t == nil {
		ts.logger.Error("Failed to get parent task", "task_id", step.TaskID, "error", err)
		return nil
	}
	t.CompleteJob()
	if err := ts.taskStore.Update(t); err != nil {
		ts.logger.Error("Failed to update task after job completion", "error", err)
	}

	// Check for newly unblocked steps (only if step was approved/completed)
	step, _ = ts.stepStore.GetByID(step.ID) // Refresh step state
	if step.State == task.StepCompleted || step.State == task.StepApproved {
		promoted, err := ts.stepStore.PromoteReadySteps(step.TaskID)
		if err != nil {
			ts.logger.Error("Failed to promote ready steps", "error", err)
		} else if len(promoted) > 0 {
			ts.logger.Info("Promoted newly unblocked steps",
				"task_id", step.TaskID,
				"count", len(promoted),
			)
			// Schedule the newly unblocked steps
			if err := ts.ScheduleReadySteps(ctx, step.TaskID); err != nil {
				ts.logger.Error("Failed to schedule unblocked steps", "error", err)
			}
		}
	}

	// Check if all steps are completed/approved
	allDone, err := ts.stepStore.AreAllCompleted(step.TaskID)
	if err != nil {
		ts.logger.Error("Failed to check task completion", "error", err)
		return nil
	}

	if allDone {
		t.SetState(task.StateCompleted)
		if err := ts.taskStore.Update(t); err != nil {
			ts.logger.Error("Failed to set task completed", "error", err)
		}

		// Build step summaries for the completion event
		stepSummaries := ts.buildStepSummaries(step.TaskID)
		executionTime := t.ExecutionTime().Round(time.Second).String()
		resultSummary := ts.buildResultSummary(stepSummaries)

		ts.publishEvent("task.completed", map[string]any{
			"task_id":         step.TaskID,
			"name":            t.Name,
			"completed_jobs":  t.CompletedJobs,
			"total_jobs":      t.TotalJobs,
			"linked_sessions": t.LinkedSessions,
			"steps":           stepSummaries,
			"execution_time":  executionTime,
			"result":          resultSummary,
		})

		ts.logger.Info("Task completed",
			"task_id", step.TaskID,
			"completed", t.CompletedJobs,
			"total", t.TotalJobs,
		)
	} else {
		// Get next step description for progress update
		nextStepDesc := ""
		readySteps, _ := ts.stepStore.GetReadySteps(step.TaskID)
		if len(readySteps) > 0 {
			nextStepDesc = readySteps[0].Description
		}

		// Publish progress update
		ts.publishEvent("task.progress", map[string]any{
			"task_id":        step.TaskID,
			"completed_jobs": t.CompletedJobs,
			"total_jobs":     t.TotalJobs,
			"current_step":   nextStepDesc,
		})
	}

	return nil
}

// handleReviewResult processes a review result and updates step state accordingly.
func (ts *TacticalScheduler) handleReviewResult(ctx context.Context, step *task.TaskStep, result *ReviewResult) error {
	switch result.Status {
	case ReviewApproved:
		// Mark as approved (terminal state)
		if err := ts.stepStore.SetState(step.ID, task.StepApproved); err != nil {
			return fmt.Errorf("failed to set approved state: %w", err)
		}
		ts.logger.Info("Step approved", "step_id", step.ID, "feedback", result.Feedback)

	case ReviewRejected:
		// Mark as rejected and create revision
		if err := ts.stepStore.SetState(step.ID, task.StepRejected); err != nil {
			return fmt.Errorf("failed to set rejected state: %w", err)
		}
		if err := ts.stepStore.SetResult(step.ID, result.Feedback); err != nil {
			ts.logger.Error("Failed to set rejection feedback", "error", err)
		}
		ts.logger.Info("Step rejected, creating revision", "step_id", step.ID, "issues", result.Issues)

		// Create revision step
		revision := task.CreateRevision(step, result.Feedback)
		if err := ts.stepStore.Create(revision); err != nil {
			ts.logger.Error("Failed to create revision step", "error", err)
		} else {
			ts.logger.Info("Created revision step",
				"revision_id", revision.ID,
				"original_id", step.ID,
			)
			// Schedule the revision
			if err := ts.scheduleStep(ctx, revision); err != nil {
				ts.logger.Error("Failed to schedule revision step", "error", err)
			}
		}

	case ReviewNeedsInfo:
		// Keep in reviewing state, update with feedback
		if err := ts.stepStore.SetResult(step.ID, result.Feedback); err != nil {
			ts.logger.Error("Failed to set needs_info feedback", "error", err)
		}
		ts.logger.Info("Step needs more info", "step_id", step.ID)

		// Mark as completed to allow task to proceed
		// (human can intervene if needed)
		if err := ts.stepStore.SetState(step.ID, task.StepCompleted); err != nil {
			ts.logger.Error("Failed to set step to completed", "error", err)
		}
	}

	return nil
}

// OnJobFailed handles a failed job by updating the step and potentially
// marking the task as failed.
func (ts *TacticalScheduler) OnJobFailed(ctx context.Context, jobID string, jobErr string) error {
	step, err := ts.stepStore.GetByJobID(jobID)
	if err != nil {
		return fmt.Errorf("failed to find step for job %s: %w", jobID, err)
	}
	if step == nil {
		return nil // Not a step-backed job
	}

	// Check if this is a rate limit error
	if ts.isRateLimitError(jobErr) {
		// Get the job from queue
		job, err := ts.queue.Get(ctx, jobID)
		if err != nil {
			ts.logger.Error("Failed to get job for retry", "job_id", jobID, "error", err)
		} else if job != nil && job.CanRetry() {
			// Retry with exponential backoff
			ts.logger.Info("Rate limit error detected, retrying job with backoff",
				"job_id", jobID,
				"step_id", step.ID,
				"retry_count", job.RetryCount+1,
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
				}
				// Publish retry event
				ts.publishEvent("queue.job.retry", map[string]any{
					"job_id": jobID,
					"reason": "rate_limit",
				})
				return nil // Job has been requeued, don't mark as failed
			}
		} else {
			ts.logger.Warn("Job cannot be retried due to rate limit",
				"job_id", jobID,
				"step_id", step.ID,
				"can_retry", job != nil && job.CanRetry(),
			)
		}
	}

	// Mark step failed
	if err := ts.stepStore.SetResult(step.ID, jobErr); err != nil {
		ts.logger.Error("Failed to set step error result", "step_id", step.ID, "error", err)
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

		ts.publishEvent("task.failed", map[string]any{
			"task_id":         step.TaskID,
			"name":            t.Name,
			"failed_jobs":     t.FailedJobs,
			"completed_jobs":  t.CompletedJobs,
			"total_jobs":      t.TotalJobs,
			"failed_step":     step.ID,
			"error":           jobErr,
			"linked_sessions": t.LinkedSessions,
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
			"task_id":        step.TaskID,
			"failed_jobs":    t.FailedJobs,
			"completed_jobs": t.CompletedJobs,
			"total_jobs":     t.TotalJobs,
			"current_step":   nextStepDesc,
		})
	}

	return nil
}

// selectAgent maps a step's ToolHint to the appropriate agent ID.
func (ts *TacticalScheduler) selectAgent(step *task.TaskStep) string {
	switch step.ToolHint {
	case "code", "refactor":
		return "coder"
	case "debug", "fix":
		return "debugger"
	case "analyze", "research":
		return "analyst"
	case "git", "commit":
		return "committer"
	case "schedule":
		return "scheduler"
	case "plan":
		return "planner"
	default:
		return "chat"
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

// isRateLimitError checks if an error message indicates a rate limit error.
func (ts *TacticalScheduler) isRateLimitError(errMsg string) bool {
	// Use the llm package helper if available
	// First try to see if we can parse it as an LLM error
	return llm.IsRateLimitErrorMessage(errMsg)
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
			"id":          s.ID,
			"description": s.Description,
			"state":       string(s.State),
			"result":      truncateString(s.Result, 100),
			"agent_id":    s.AgentID,
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

	sb.WriteString(fmt.Sprintf("Completed %d/%d steps: ", completedCount, len(steps)))

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
