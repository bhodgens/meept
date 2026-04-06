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

	// Publish progress event
	ts.publishEvent("task.progress", map[string]any{
		"task_id":         taskID,
		"scheduled_steps": len(readySteps),
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

		ts.publishEvent("task.completed", map[string]any{
			"task_id":         step.TaskID,
			"name":            t.Name,
			"completed_jobs":  t.CompletedJobs,
			"total_jobs":      t.TotalJobs,
			"linked_sessions": t.LinkedSessions,
		})

		ts.logger.Info("Task completed",
			"task_id", step.TaskID,
			"completed", t.CompletedJobs,
			"total", t.TotalJobs,
		)
	} else {
		// Publish progress update
		ts.publishEvent("task.progress", map[string]any{
			"task_id":        step.TaskID,
			"completed_jobs": t.CompletedJobs,
			"total_jobs":     t.TotalJobs,
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
		// Task is still partially alive
		ts.publishEvent("task.progress", map[string]any{
			"task_id":      step.TaskID,
			"failed_jobs":  t.FailedJobs,
			"completed_jobs": t.CompletedJobs,
			"total_jobs":   t.TotalJobs,
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
