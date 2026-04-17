package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/task"
	"github.com/caimlas/meept/pkg/models"
)

// ReviewManager orchestrates the review process for task steps.
type ReviewManager struct {
	registry  *AgentRegistry
	stepStore *task.StepStore
	taskStore *task.Store
	policy    *ReviewPolicy
	bus       *bus.MessageBus
	logger    *slog.Logger
}

// ReviewManagerConfig holds configuration for creating a ReviewManager.
type ReviewManagerConfig struct {
	Registry  *AgentRegistry
	StepStore *task.StepStore
	TaskStore *task.Store
	Policy    *ReviewPolicy
	Bus       *bus.MessageBus
	Logger    *slog.Logger
}

// NewReviewManager creates a new review manager.
func NewReviewManager(cfg ReviewManagerConfig) *ReviewManager {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.Policy == nil {
		cfg.Policy = DefaultReviewPolicy()
	}

	return &ReviewManager{
		registry:  cfg.Registry,
		stepStore: cfg.StepStore,
		taskStore: cfg.TaskStore,
		policy:    cfg.Policy,
		bus:       cfg.Bus,
		logger:    cfg.Logger,
	}
}

// ReviewStep initiates review of a completed step.
func (rm *ReviewManager) ReviewStep(ctx context.Context, step *task.TaskStep) (*ReviewResult, error) {
	startTime := time.Now()

	rm.logger.Info("Starting review",
		"step_id", step.ID,
		"task_id", step.TaskID,
		"tool_hint", step.ToolHint,
	)

	// Set step to reviewing state
	if err := rm.stepStore.SetState(step.ID, task.StepReviewing); err != nil {
		rm.logger.Error("Failed to set step to reviewing", "error", err)
	}

	// Check if review is needed based on policy
	if !rm.policy.NeedsReview(step) {
		rm.logger.Debug("Step does not require review", "step_id", step.ID)
		return &ReviewResult{
			Status:     ReviewApproved,
			Feedback:   "Auto-approved (no review required)",
			Confidence: 1.0,
		}, nil
	}

	// Check auto-approve patterns
	if rm.policy.ShouldAutoApprove(step) {
		rm.logger.Debug("Step auto-approved", "step_id", step.ID)
		if err := rm.stepStore.SetState(step.ID, task.StepApproved); err != nil {
			rm.logger.Error("Failed to set step to approved", "error", err)
		}
		return &ReviewResult{
			Status:     ReviewApproved,
			Feedback:   "Auto-approved (low-risk change)",
			Confidence: 1.0,
		}, nil
	}

	// Check if human intervention is needed
	if rm.policy.RequiresHumanIntervention(step) {
		rm.logger.Warn("Step requires human intervention",
			"step_id", step.ID,
			"revision_count", step.RevisionCount,
		)
		return &ReviewResult{
			Status:     ReviewNeedsInfo,
			Feedback:   fmt.Sprintf("Maximum revision cycles (%d) exceeded. Human intervention required.", rm.policy.MaxRevisionCycles),
			Confidence: 1.0,
		}, nil
	}

	// Select reviewer agent
	reviewerID := rm.policy.SelectReviewer(step)
	rm.logger.Debug("Selected reviewer",
		"step_id", step.ID,
		"reviewer", reviewerID,
	)

	// Build review prompt
	prompt := rm.buildReviewPrompt(step)

	// Get reviewer agent loop
	reviewerLoop, err := rm.registry.Get(reviewerID)
	if err != nil {
		rm.logger.Error("Failed to get reviewer agent", "reviewer", reviewerID, "error", err)
		// Fall back to auto-approve if reviewer not available
		return &ReviewResult{
			Status:     ReviewApproved,
			Feedback:   fmt.Sprintf("Reviewer %s not available, auto-approved", reviewerID),
			Confidence: 0.5,
		}, nil
	}

	// Run reviewer agent
	output, err := reviewerLoop.RunOnce(ctx, prompt, step.ID)
	if err != nil {
		rm.logger.Error("Reviewer agent failed", "error", err)
		return nil, fmt.Errorf("reviewer agent failed: %w", err)
	}

	// Parse review result
	result := rm.parseReviewResult(output)
	result.ReviewerID = reviewerID
	result.Duration = time.Since(startTime)

	rm.logger.Info("Review completed",
		"step_id", step.ID,
		"status", result.Status,
		"confidence", result.Confidence,
		"duration", result.Duration,
	)

	// Publish review event
	rm.publishReviewEvent(step.ID, step.TaskID, result)

	return result, nil
}

// buildReviewPrompt creates a review prompt for a step.
func (rm *ReviewManager) buildReviewPrompt(step *task.TaskStep) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("REVIEW TASK STEP\n\n"))
	sb.WriteString(fmt.Sprintf("Step ID: %s\n", step.ID))
	sb.WriteString(fmt.Sprintf("Description: %s\n", step.Description))
	sb.WriteString(fmt.Sprintf("Tool Hint: %s\n", step.ToolHint))
	sb.WriteString(fmt.Sprintf("Agent: %s\n", step.AgentID))
	sb.WriteString(fmt.Sprintf("Result:\n%s\n\n", step.Result))

	sb.WriteString("Your task is to review this work for:\n")
	sb.WriteString("- Correctness: Does the work accomplish what was described?\n")
	sb.WriteString("- Quality: Is the work well-executed and follows best practices?\n")
	sb.WriteString("- Completeness: Is anything missing that should be included?\n")
	sb.WriteString("- Safety: Are there any security issues or potential problems?\n\n")

	sb.WriteString("Respond with a structured review in this JSON format:\n")
	sb.WriteString(`{"status": "approved" | "rejected" | "needs_info", `)
	sb.WriteString(`"feedback": "explanation", `)
	sb.WriteString(`"issues": ["issue1", "issue2"], `)
	sb.WriteString(`"confidence": 0.0-1.0}\n\n`)

	sb.WriteString("If approving, keep feedback brief. If rejecting, provide specific actionable feedback.\n")

	return sb.String()
}

// parseReviewResult extracts the review decision from LLM output.
func (rm *ReviewManager) parseReviewResult(output string) *ReviewResult {
	result := &ReviewResult{
		Status:     ReviewApproved, // Default to approve
		Feedback:   "No explicit feedback provided",
		Confidence: 0.5,
	}

	// Try to extract JSON from the output
	jsonPattern := regexp.MustCompile(`\{[^{}]*"status"\s*:\s*"[^"]*"[^{}]*\}`)
	matches := jsonPattern.FindStringSubmatch(output)

	if len(matches) > 0 {
		var parsed struct {
			Status     string   `json:"status"`
			Feedback   string   `json:"feedback"`
			Issues     []string `json:"issues"`
			Confidence float64  `json:"confidence"`
		}

		if err := json.Unmarshal([]byte(matches[0]), &parsed); err == nil {
			switch parsed.Status {
			case "approved":
				result.Status = ReviewApproved
			case "rejected":
				result.Status = ReviewRejected
			case "needs_info":
				result.Status = ReviewNeedsInfo
			}

			if parsed.Feedback != "" {
				result.Feedback = parsed.Feedback
			}
			if len(parsed.Issues) > 0 {
				result.Issues = parsed.Issues
			}
			if parsed.Confidence > 0 {
				result.Confidence = parsed.Confidence
			}
		}
	}

	// Fallback: analyze text for decision
	outputLower := strings.ToLower(output)
	if strings.Contains(outputLower, "reject") || strings.Contains(outputLower, "needs revision") {
		result.Status = ReviewRejected
	}

	// Extract feedback from non-JSON parts
	if result.Feedback == "No explicit feedback provided" && len(output) > 0 {
		// Remove JSON part if present
		feedback := output
		if len(matches) > 0 {
			feedback = strings.ReplaceAll(output, matches[0], "")
		}
		feedback = strings.TrimSpace(feedback)
		if len(feedback) > 500 {
			feedback = feedback[:500] + "..."
		}
		if feedback != "" {
			result.Feedback = feedback
		}
	}

	return result
}

// HandleReviewResult processes a review result and updates step state.
// Returns any newly-created revision step(s) so that callers with scheduling
// responsibilities (e.g. TacticalScheduler) can enqueue them.
func (rm *ReviewManager) HandleReviewResult(ctx context.Context, stepID string, result *ReviewResult) ([]*task.TaskStep, error) {
	step, err := rm.stepStore.GetByID(stepID)
	if err != nil {
		return nil, fmt.Errorf("failed to get step: %w", err)
	}
	if step == nil {
		return nil, fmt.Errorf("step not found: %s", stepID)
	}

	var revisions []*task.TaskStep

	switch result.Status {
	case ReviewApproved:
		// Mark as approved (terminal state)
		if err := rm.stepStore.SetState(step.ID, task.StepApproved); err != nil {
			return nil, fmt.Errorf("failed to set approved state: %w", err)
		}
		rm.logger.Info("Step approved", "step_id", step.ID, "feedback", result.Feedback)

		// Promote dependent steps
		promoted, err := rm.stepStore.PromoteReadySteps(step.TaskID)
		if err != nil {
			rm.logger.Error("Failed to promote ready steps", "error", err)
		} else if len(promoted) > 0 {
			rm.logger.Info("Promoted dependent steps after approval",
				"count", len(promoted),
				"task_id", step.TaskID,
			)
		}

	case ReviewRejected:
		// Mark as rejected
		if err := rm.stepStore.SetState(step.ID, task.StepRejected); err != nil {
			return nil, fmt.Errorf("failed to set rejected state: %w", err)
		}
		if err := rm.stepStore.SetResult(step.ID, result.Feedback); err != nil {
			rm.logger.Error("Failed to set rejection feedback", "error", err)
		}
		rm.logger.Info("Step rejected", "step_id", step.ID, "issues", result.Issues)

		// Create revision step
		revision := task.CreateRevision(step, result.Feedback)
		if err := rm.stepStore.Create(revision); err != nil {
			rm.logger.Error("Failed to create revision step", "error", err)
		} else {
			rm.logger.Info("Created revision step",
				"revision_id", revision.ID,
				"original_id", step.ID,
			)
			revisions = append(revisions, revision)
		}

	case ReviewNeedsInfo:
		// Keep in reviewing state, update result with feedback
		if err := rm.stepStore.SetResult(step.ID, result.Feedback); err != nil {
			rm.logger.Error("Failed to set needs_info feedback", "error", err)
		}
		rm.logger.Info("Step needs more info", "step_id", step.ID)
	}

	return revisions, nil
}

// publishReviewEvent publishes a review completion event.
func (rm *ReviewManager) publishReviewEvent(stepID, taskID string, result *ReviewResult) {
	if rm.bus == nil {
		return
	}

	msg, err := models.NewBusMessage(models.MessageTypeEvent, "review-manager", map[string]any{
		"step_id":    stepID,
		"task_id":    taskID,
		"status":     string(result.Status),
		"feedback":   result.Feedback,
		"confidence": result.Confidence,
		"reviewer":   result.ReviewerID,
	})
	if err != nil {
		rm.logger.Error("Failed to create review message", "error", err)
		return
	}

	rm.bus.Publish("step.review_completed", msg)
}

// SetPolicy updates the review policy.
func (rm *ReviewManager) SetPolicy(policy *ReviewPolicy) {
	rm.policy = policy
	rm.logger.Info("Review policy updated")
}

// GetPolicy returns the current review policy.
func (rm *ReviewManager) GetPolicy() *ReviewPolicy {
	return rm.policy
}
