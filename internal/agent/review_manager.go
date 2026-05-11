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

// CompletionStatus represents the result of a completion validation.
type CompletionStatus string

const (
	CompletionValid   CompletionStatus = "valid"
	CompletionInvalid CompletionStatus = "invalid"
	CompletionPartial CompletionStatus = "partial"
)

// ValidationResult holds the result of ValidateCompletion().
type ValidationResult struct {
	Status   CompletionStatus `json:"status"`
	Feedback string           `json:"feedback"`
	Missing  []string         `json:"missing,omitempty"`  // Items not completed
	Verified []string         `json:"verified,omitempty"` // Items verified complete
}

// ReviewManager orchestrates the review process for task steps.
type ReviewManager struct {
	registry         *AgentRegistry
	stepStore        *task.StepStore
	taskStore        *task.Store
	policy           *ReviewPolicy
	validationPolicy *ValidationPolicy
	bus              *bus.MessageBus
	logger           *slog.Logger
}

// ReviewManagerConfig holds configuration for creating a ReviewManager.
type ReviewManagerConfig struct {
	Registry         *AgentRegistry
	StepStore        *task.StepStore
	TaskStore        *task.Store
	Policy           *ReviewPolicy
	ValidationPolicy *ValidationPolicy
	Bus              *bus.MessageBus
	Logger           *slog.Logger
}

// NewReviewManager creates a new review manager.
func NewReviewManager(cfg ReviewManagerConfig) *ReviewManager {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.Policy == nil {
		cfg.Policy = DefaultReviewPolicy()
	}
	if cfg.ValidationPolicy == nil {
		cfg.ValidationPolicy = DefaultValidationPolicy()
	}

	return &ReviewManager{
		registry:         cfg.Registry,
		stepStore:        cfg.StepStore,
		taskStore:        cfg.TaskStore,
		policy:           cfg.Policy,
		validationPolicy: cfg.ValidationPolicy,
		bus:              cfg.Bus,
		logger:           cfg.Logger,
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
	rm.publishReviewEvent(step.ID, step.TaskID, result, step.RevisionCount)

	return result, nil
}

// buildReviewPrompt creates a review prompt for a step.
func (rm *ReviewManager) buildReviewPrompt(step *task.TaskStep) string {
	var sb strings.Builder

	sb.WriteString("REVIEW TASK STEP\n\n")
	fmt.Fprintf(&sb, "Step ID: %s\n", step.ID)
	fmt.Fprintf(&sb, "Description: %s\n", step.Description)
	fmt.Fprintf(&sb, "Tool Hint: %s\n", step.ToolHint)
	fmt.Fprintf(&sb, "Agent: %s\n", step.AgentID)
	fmt.Fprintf(&sb, "Result:\n%s\n\n", step.Result)

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

	// Try to extract JSON from the output using multiple strategies
	jsonStr := rm.extractReviewJSON(output)
	jsonParsed := false

	if jsonStr != "" {
		var parsed struct {
			Status     string   `json:"status"`
			Feedback   string   `json:"feedback"`
			Issues     []string `json:"issues"`
			Confidence float64  `json:"confidence"`
		}

		if err := json.Unmarshal([]byte(jsonStr), &parsed); err == nil {
			jsonParsed = true
			switch strings.ToLower(parsed.Status) {
			case "approved", "approve", "pass", "lgtm":
				result.Status = ReviewApproved
			case "rejected", "reject", "fail":
				result.Status = ReviewRejected
			case "needs_info", "needsinfo", "needs-info", "info":
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

	// Fallback: analyze text for decision ONLY if JSON parsing failed
	// Use phrase matching to avoid false positives from negations
	if !jsonParsed {
		result.Status, result.Confidence = rm.analyzeReviewText(output)
	}

	// Extract feedback from non-JSON parts if needed
	if result.Feedback == "No explicit feedback provided" && len(output) > 0 {
		feedback := output
		if jsonStr != "" {
			feedback = strings.ReplaceAll(output, jsonStr, "")
		}
		// Remove markdown code fences
		feedback = regexp.MustCompile("```[\\s\\S]*?```").ReplaceAllString(feedback, "")
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

// extractReviewJSON attempts to extract valid JSON containing a status field from output.
func (rm *ReviewManager) extractReviewJSON(output string) string {
	// Strategy 1: Check if the entire output is valid JSON
	trimmed := strings.TrimSpace(output)
	if strings.HasPrefix(trimmed, "{") && strings.HasSuffix(trimmed, "}") {
		if json.Valid([]byte(trimmed)) && strings.Contains(trimmed, `"status"`) {
			return trimmed
		}
	}

	// Strategy 2: Extract from markdown code fence
	codeBlockPattern := regexp.MustCompile("```(?:json)?\\s*\\n?([\\s\\S]*?)\\n?```")
	if matches := codeBlockPattern.FindStringSubmatch(output); len(matches) > 1 {
		candidate := strings.TrimSpace(matches[1])
		if json.Valid([]byte(candidate)) && strings.Contains(candidate, `"status"`) {
			return candidate
		}
	}

	// Strategy 3: Find JSON object by balanced braces
	start := strings.Index(output, "{")
	if start >= 0 {
		depth := 0
	braceSearch:
		for i := start; i < len(output); i++ {
			switch output[i] {
			case '{':
				depth++
			case '}':
				depth--
				if depth == 0 {
					candidate := output[start : i+1]
					if json.Valid([]byte(candidate)) && strings.Contains(candidate, `"status"`) {
						return candidate
					}
					break braceSearch
				}
			}
		}
	}

	return ""
}

// analyzeReviewText performs text analysis to determine review status.
// It uses phrase matching to avoid false positives from negations.
func (rm *ReviewManager) analyzeReviewText(output string) (ReviewStatus, float64) {
	lower := strings.ToLower(output)

	// Check for explicit rejection phrases (high confidence)
	rejectionPhrases := []string{
		"i reject",
		"this is rejected",
		"status: rejected",
		"my verdict is reject",
		"decision: reject",
		"must be rejected",
		"should be rejected",
		"needs revision",
		"requires revision",
		"cannot approve",
		"cannot be approved",
		"do not approve",
		"fails review",
		"review: fail",
	}
	for _, phrase := range rejectionPhrases {
		if strings.Contains(lower, phrase) {
			return ReviewRejected, 0.8
		}
	}

	// Check for explicit approval phrases (high confidence)
	approvalPhrases := []string{
		"i approve",
		"this is approved",
		"status: approved",
		"my verdict is approve",
		"decision: approve",
		"looks good",
		"lgtm",
		"passes review",
		"review: pass",
	}
	for _, phrase := range approvalPhrases {
		if strings.Contains(lower, phrase) {
			return ReviewApproved, 0.8
		}
	}

	// Check for needs_info phrases
	needsInfoPhrases := []string{
		"need more info",
		"needs more information",
		"unclear",
		"please clarify",
		"cannot determine",
	}
	for _, phrase := range needsInfoPhrases {
		if strings.Contains(lower, phrase) {
			return ReviewNeedsInfo, 0.7
		}
	}

	// Default to approved with low confidence if no clear signal
	return ReviewApproved, 0.3
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

		// Increment original step's revision count BEFORE creating revision
		// This fixes the bug where revision count tracking was always 0
		step.IncrementRevision()
		if err := rm.stepStore.Update(step); err != nil {
			// AGENT-23 FIX: Return the error so callers that manage the
			// scheduling lifecycle (e.g. TacticalScheduler) can observe
			// revision-count failures and surface them to the operator.
			return nil, fmt.Errorf("failed to increment revision count: %w", err)
		}

		// Create revision step
		revision := task.CreateRevision(step, result.Feedback)
		if err := rm.stepStore.Create(revision); err != nil {
			rm.logger.Error("Failed to create revision step", "error", err)
		} else {
			rm.logger.Info("Created revision step",
				"revision_id", revision.ID,
				"original_id", step.ID,
				"revision_count", step.RevisionCount,
			)
			revisions = append(revisions, revision)

			// Update task TotalJobs to include the new revision step
			if rm.taskStore != nil {
				if t, err := rm.taskStore.GetByID(step.TaskID); err == nil && t != nil {
					t.IncrementJobs()
					if err := rm.taskStore.Update(t); err != nil {
						rm.logger.Error("Failed to update task TotalJobs for revision", "error", err)
					}
				}
			}
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
func (rm *ReviewManager) publishReviewEvent(stepID, taskID string, result *ReviewResult, revisionCount int) {
	if rm.bus == nil {
		return
	}

	msg, err := models.NewBusMessage(models.MessageTypeEvent, "review-manager", map[string]any{
		"step_id":        stepID,
		"task_id":        taskID,
		"status":         string(result.Status),
		"feedback":       result.Feedback,
		"confidence":     result.Confidence,
		"reviewer":       result.ReviewerID,
		"revision_count": revisionCount,
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

// ValidateCompletion checks that all assigned work for a step is actually done.
// It examines the step's result, evidence, and claims to determine if the work
// described in the step was fully completed.
func (rm *ReviewManager) ValidateCompletion(ctx context.Context, step *task.TaskStep, taskDesc string) (*ValidationResult, error) {
	// Check if validation is needed
	if !rm.validationPolicy.NeedsValidation(step) {
		return &ValidationResult{
			Status:   CompletionValid,
			Feedback: "Validation not required for this step type",
		}, nil
	}

	rm.logger.Info("Validating step completion",
		"step_id", step.ID,
		"task_id", step.TaskID,
		"tool_hint", step.ToolHint,
	)

	// Check 1: Step has a non-empty result
	if strings.TrimSpace(step.Result) == "" {
		return &ValidationResult{
			Status:   CompletionInvalid,
			Feedback: "Step completed with empty result",
			Missing:  []string{"result content"},
		}, nil
	}

	// Check 2: Evidence was provided (if applicable)
	if step.ToolHint == "code" || step.ToolHint == "refactor" || step.ToolHint == "fix" {
		if len(step.Evidence) == 0 && len(step.Claims) == 0 {
			return &ValidationResult{
				Status:   CompletionPartial,
				Feedback: "Code change completed without evidence or claims - cannot verify",
				Missing:  []string{"evidence", "claims"},
			}, nil
		}
	}

	// Check 3: Verify claims match the step description
	verified, missing := rm.checkClaimsAgainstDescription(step)
	if len(missing) > 0 && len(step.Evidence) == 0 {
		return &ValidationResult{
			Status:   CompletionPartial,
			Feedback: fmt.Sprintf("Step partially completed: %d items verified, %d items missing", len(verified), len(missing)),
			Missing:  missing,
			Verified: verified,
		}, nil
	}

	// Check 4: Cross-reference with original task intent if available
	if taskDesc != "" && len(step.Evidence) > 0 {
		// When evidence is present, only flag if relevance is extremely low
		taskKeywords := extractKeywords(taskDesc)
		resultLower := strings.ToLower(step.Result)
		matchedKeywords := 0
		for _, kw := range taskKeywords {
			if strings.Contains(resultLower, strings.ToLower(kw)) {
				matchedKeywords++
			}
		}

		// If less than 15% of task keywords appear in the result, flag as partial
		if len(taskKeywords) > 0 && float64(matchedKeywords)/float64(len(taskKeywords)) < 0.15 {
			return &ValidationResult{
				Status:   CompletionPartial,
				Feedback: "Step result has low relevance to original task description",
				Missing:  []string{"task-relevant content"},
				Verified: verified,
			}, nil
		}
	} else if taskDesc != "" {
		// No evidence - use stricter relevance check (30% threshold)
		taskKeywords := extractKeywords(taskDesc)
		resultLower := strings.ToLower(step.Result)
		matchedKeywords := 0
		for _, kw := range taskKeywords {
			if strings.Contains(resultLower, strings.ToLower(kw)) {
				matchedKeywords++
			}
		}

		if len(taskKeywords) > 0 && float64(matchedKeywords)/float64(len(taskKeywords)) < 0.3 {
			return &ValidationResult{
				Status:   CompletionPartial,
				Feedback: "Step result has low relevance to original task description",
				Missing:  []string{"task-relevant content"},
				Verified: verified,
			}, nil
		}
	}

	rm.logger.Info("Step validation passed",
		"step_id", step.ID,
		"verified_count", len(verified),
	)

	return &ValidationResult{
		Status:   CompletionValid,
		Feedback: "All assigned work verified complete",
		Verified: verified,
	}, nil
}

// checkClaimsAgainstDescription compares step claims against the step description
// to verify the stated work was completed.
func (rm *ReviewManager) checkClaimsAgainstDescription(step *task.TaskStep) ([]string, []string) {
	var verified []string
	var missing []string

	descLower := strings.ToLower(step.Description)

	// Check if claims are present
	if len(step.Claims) > 0 {
		verified = append(verified, step.Claims...)
	} else {
		// No explicit claims - check if the result mentions completing the description
		resultLower := strings.ToLower(step.Result)
		descKeywords := extractKeywords(step.Description)

		for _, kw := range descKeywords {
			if strings.Contains(resultLower, strings.ToLower(kw)) {
				verified = append(verified, kw)
			} else {
				missing = append(missing, kw)
			}
		}
	}

	// If step had errors, it's not complete
	if strings.Contains(descLower, "fix") || strings.Contains(descLower, "debug") {
		// Check result for error indicators
		resultLower := strings.ToLower(step.Result)
		errorIndicators := []string{"error", "failed", "could not", "unable to", "not found"}
		hasErrors := false
		for _, indicator := range errorIndicators {
			if strings.Contains(resultLower, indicator) && !strings.Contains(resultLower, "fixed") && !strings.Contains(resultLower, "resolved") {
				hasErrors = true
				break
			}
		}
		if hasErrors && len(step.Claims) == 0 {
			missing = append(missing, "error resolution confirmation")
		}
	}

	return verified, missing
}

// extractKeywords extracts meaningful keywords from a description.
func extractKeywords(desc string) []string {
	// Remove common stop words and extract meaningful keywords
	stopWords := map[string]bool{
		"the": true, "a": true, "an": true, "is": true, "are": true,
		"was": true, "were": true, "be": true, "been": true, "being": true,
		"have": true, "has": true, "had": true, "do": true, "does": true,
		"did": true, "will": true, "would": true, "could": true, "should": true,
		"may": true, "might": true, "can": true, "shall": true, "to": true,
		"of": true, "in": true, "for": true, "on": true, "with": true,
		"at": true, "by": true, "from": true, "as": true, "into": true,
		"through": true, "during": true, "before": true, "after": true,
		"and": true, "but": true, "or": true, "nor": true, "not": true,
		"so": true, "yet": true, "both": true, "either": true, "neither": true,
		"this": true, "that": true, "these": true, "those": true,
		"it": true, "its": true, "which": true, "who": true, "whom": true,
		"what": true, "where": true, "when": true, "how": true, "why": true,
	}

	words := strings.Fields(strings.ToLower(desc))
	var keywords []string
	for _, word := range words {
		// Remove punctuation
		word = strings.Trim(word, ".,;:!?'\"()[]{}")
		if len(word) > 2 && !stopWords[word] {
			keywords = append(keywords, word)
		}
	}

	return keywords
}

// SetValidationPolicy updates the validation policy.
func (rm *ReviewManager) SetValidationPolicy(policy *ValidationPolicy) {
	rm.validationPolicy = policy
	rm.logger.Info("Validation policy updated")
}

// GetValidationPolicy returns the current validation policy.
func (rm *ReviewManager) GetValidationPolicy() *ValidationPolicy {
	return rm.validationPolicy
}

// GetPolicy returns the current review policy.
func (rm *ReviewManager) GetPolicy() *ReviewPolicy {
	return rm.policy
}
