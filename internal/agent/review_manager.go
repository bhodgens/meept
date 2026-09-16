package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"sync"
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
	mu               sync.RWMutex
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

	// Wire the registry into the policy so SelectReviewer can do dynamic
	// reviewer-role lookup by reviews_domain.
	cfg.Policy.Registry = cfg.Registry

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
func (rm *ReviewManager) ReviewStep(ctx context.Context, step *task.TaskStep, spec *TaskSpec) (*ReviewResult, error) {
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

	// Snapshot policy under lock to avoid racing with SetPolicy (S1-17).
	rm.mu.RLock()
	policy := rm.policy
	rm.mu.RUnlock()

	// FAIL: Tool execution failed — nothing meaningful to review. The step
	// must not pass review: an approved error is how "Task completed" stubs
	// lied to users (2026-09-04 finding F2). This gate runs before every
	// policy path (needs-review, auto-approve, heuristic) so an error step
	// can never be laundered into an approval.
	if rm.stepHasError(step) {
		rm.logger.Info("Review rejected: step execution produced an error",
			"step_id", step.ID,
			"tool_hint", step.ToolHint,
		)
		if err := rm.stepStore.SetState(step.ID, task.StepFailed); err != nil {
			rm.logger.Error("Failed to set error step to failed", "error", err)
		}
		return &ReviewResult{
			Status:     ReviewRejected,
			Feedback:   "Rejected: step execution error — " + truncateString(firstLine(step.Result), 200),
			Confidence: 1.0,
		}, nil
	}

	// Check if review is needed based on policy
	if !policy.NeedsReview(step) {
		rm.logger.Debug("Step does not require review", "step_id", step.ID)
		return &ReviewResult{
			Status:     ReviewApproved,
			Feedback:   "Auto-approved (no review required)",
			Confidence: 1.0,
		}, nil
	}

	// Check auto-approve patterns
	if policy.ShouldAutoApprove(step) {
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
	if policy.RequiresHumanIntervention(step) {
		rm.logger.Warn("Step requires human intervention",
			"step_id", step.ID,
			"revision_count", step.RevisionCount,
		)
		feedback := fmt.Sprintf("Maximum revision cycles (%d) exceeded. Human intervention required.", policy.MaxRevisionCycles)
		if spec != nil {
			for _, c := range spec.Criteria {
				if c.StepSequence == step.Sequence {
					feedback += fmt.Sprintf(" Original acceptance criteria: %s", c.AcceptanceCriteria)
					break
				}
			}
		}
		return &ReviewResult{
			Status:     ReviewNeedsInfo,
			Feedback:   feedback,
			Confidence: 1.0,
		}, nil
	}

	// FAIL backstop: Tool execution failed — nothing meaningful to review.
	// Unreachable when the pre-policy gate above fires, but kept as a
	// hard guarantee: no code path in ReviewStep may return ReviewApproved
	// for an error step (2026-09-04 finding F2).
	if rm.stepHasError(step) {
		rm.logger.Info("Review rejected: step execution produced an error",
			"step_id", step.ID,
			"tool_hint", step.ToolHint,
		)
		if err := rm.stepStore.SetState(step.ID, task.StepFailed); err != nil {
			rm.logger.Error("Failed to set error step to failed", "error", err)
		}
		return &ReviewResult{
			Status:     ReviewRejected,
			Feedback:   "Rejected: step execution error — " + truncateString(firstLine(step.Result), 200),
			Confidence: 1.0,
		}, nil
	}

	// SKIP: Trivial task — fewer than 3 steps in the task means low
	// complexity where LLM review cost outweighs benefit. Use a
	// lightweight heuristic check instead.
	if rm.isTrivialTask(step) {
		rm.logger.Info("Skipping full review: trivial task (<3 steps), using heuristic",
			"step_id", step.ID,
			"tool_hint", step.ToolHint,
		)
		if rm.heuristicReviewPasses(step) {
			if err := rm.stepStore.SetState(step.ID, task.StepApproved); err != nil {
				rm.logger.Error("Failed to set step to approved", "error", err)
			}
			// F1 (2026-09-12 bughunt): SetState writes the DB only; the
			// in-memory step still carries its pre-review state, and the
			// full-row Update below writes `state = step.State`
			// (internal/task/step.go) — reverting the approval to a
			// non-terminal state so AreAllCompleted never fires. Assign
			// the state before the Update (the 2026-09-06 fix shape the
			// rejection path already uses).
			step.State = task.StepApproved
			// Mark the step validated only when there is tool-issued
			// evidence for the approval to stand on (F11): a heuristic
			// pass over a step with no evidence verifies nothing, so the
			// record stays Validated=false rather than claiming a
			// verification that never happened. An existing unverified
			// marker is never cleared here.
			if !step.Validated && hasMeaningfulEvidence(step.Evidence) {
				step.Validated = true
			}
			if err := rm.stepStore.Update(step); err != nil {
				rm.logger.Warn("Failed to persist heuristic approval state",
					"step_id", step.ID, "error", err)
			}
			return &ReviewResult{
				Status:     ReviewApproved,
				Feedback:   "Auto-approved (heuristic check: trivial task, non-empty result)",
				Confidence: 1.0,
			}, nil
		}
		return &ReviewResult{
			Status:     ReviewRejected,
			Feedback:   "Heuristic check failed: non-empty result but no meaningful content",
			Confidence: 0.7,
		}, nil
	}

	// Select reviewer agent
	reviewerID := policy.SelectReviewer(step)
	rm.logger.Debug("Selected reviewer",
		"step_id", step.ID,
		"reviewer", reviewerID,
	)

	// Build review prompt
	prompt := rm.buildReviewPrompt(step, spec)

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
func (rm *ReviewManager) buildReviewPrompt(step *task.TaskStep, spec *TaskSpec) string {
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

	// Include acceptance criteria from spec if available
	if spec != nil {
		for _, c := range spec.Criteria {
			if c.StepSequence == step.Sequence {
				sb.WriteString("\nACCEPTANCE CRITERIA (you MUST check these):\n")
				sb.WriteString(c.AcceptanceCriteria)
				sb.WriteString("\n\nEvaluate the work specifically against these criteria.\n")
				break
			}
		}
	}

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
	if result.Feedback == "No explicit feedback provided" && output != "" {
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
func (rm *ReviewManager) analyzeReviewText(output string) (result ReviewStatus, f float64) {
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
func (rm *ReviewManager) HandleReviewResult(ctx context.Context, stepID string, result *ReviewResult, spec *TaskSpec) ([]*task.TaskStep, error) {
	step, err := rm.stepStore.GetByID(stepID)
	if err != nil {
		return nil, fmt.Errorf("failed to get step: %w", err)
	}
	if step == nil {
		return nil, fmt.Errorf("step not found: %s", stepID)
	}

	var revisions []*task.TaskStep
	// outErr accumulates errors from non-fatal SetResult calls (S1-10).
	var outErr error

	switch result.Status {
	case ReviewApproved:
		// Mark as approved (terminal state)
		if err := rm.stepStore.SetState(step.ID, task.StepApproved); err != nil {
			return nil, fmt.Errorf("failed to set approved state: %w", err)
		}
		// F1 (2026-09-12 bughunt): SetState writes the DB only; the step
		// was loaded while still 'reviewing' (SetState at ReviewStep), so
		// the full-row Update below would serialize that stale state back
		// over the approval — leaving the step non-terminal and the task
		// unfinalizable. Assign the state FIRST (the 2026-09-06 fix shape
		// the rejection path below already uses).
		step.State = task.StepApproved
		// Reviewer approval IS the platform's judgment that the step is
		// done (e2e run 13, 2026-09-11) — but only stamp Validated when
		// there is tool-issued evidence for it (F11). An approval with no
		// evidence verifies nothing, so the record keeps Validated=false;
		// a standing unverified marker (ValidationError) is never cleared
		// by an approval.
		if !step.Validated && hasMeaningfulEvidence(step.Evidence) {
			step.Validated = true
		}
		if err := rm.stepStore.Update(step); err != nil {
			rm.logger.Warn("failed to persist approval state", "step_id", step.ID, "error", err)
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
		// Execution-error gate: the step is already failed (terminal).
		// A revision depending on a failed step can never be promoted
		// (IsSuccessfullyTerminal blocks on failed deps), so revising is
		// pointless — keep the failure and record the feedback only.
		if step.State == task.StepFailed {
			rm.logger.Warn("Step rejected after execution error; not creating revision",
				"step_id", step.ID,
			)
			if err := rm.stepStore.SetResult(step.ID, result.Feedback); err != nil {
				rm.logger.Error("Failed to set failure feedback", "error", err)
				outErr = fmt.Errorf("failed to set failure feedback: %w", err)
			}
			break
		}

		// Reject + bump revision count in ONE full-row write. The previous
		// two-step sequence (SetState(rejected) then Update(step) with the
		// stale in-memory state) wrote "reviewing" back over "rejected" and
		// left the revision step depending on a permanently non-terminal
		// original — revisions never scheduled (2026-09-06 live-run finding).
		step.IncrementRevision()
		step.State = task.StepRejected
		if err := rm.stepStore.Update(step); err != nil {
			// AGENT-23 FIX: Return the error so callers that manage the
			// scheduling lifecycle (e.g. TacticalScheduler) can observe
			// revision-count failures and surface them to the operator.
			return nil, fmt.Errorf("failed to persist rejected state: %w", err)
		}
		if err := rm.stepStore.SetResult(step.ID, result.Feedback); err != nil {
			rm.logger.Error("Failed to set rejection feedback", "error", err)
			outErr = fmt.Errorf("failed to set rejection feedback: %w", err)
		}
		rm.logger.Info("Step rejected", "step_id", step.ID, "issues", result.Issues)

		// Create revision step with feedback context
		revisionContext := BuildRevisionContext(result, spec)
		revision := task.CreateRevisionWithContext(step, result.Feedback, revisionContext)
		if err := rm.stepStore.Create(revision); err != nil {
			rm.logger.Error("Failed to create revision step", "error", err)
		} else {
			rm.logger.Info("Created revision step",
				"revision_id", revision.ID,
				"original_id", step.ID,
				"revision_count", step.RevisionCount,
			)
			revisions = append(revisions, revision)

			// Update task TotalJobs to include the new revision step.
			// Atomic increment (A-08 pattern): the previous Get→mutate→Update
			// sequence lost increments when a concurrent step completion
			// wrote back a stale counter snapshot.
			if rm.taskStore != nil {
				if err := rm.taskStore.IncrementTotalJobs(step.TaskID); err != nil {
					rm.logger.Error("Failed to update task TotalJobs for revision", "error", err)
				}
			}
		}

	case ReviewNeedsInfo:
		// Keep in reviewing state, update result with feedback
		if err := rm.stepStore.SetResult(step.ID, result.Feedback); err != nil {
			rm.logger.Error("Failed to set needs_info feedback", "error", err)
			outErr = fmt.Errorf("failed to set needs_info feedback: %w", err)
		}
		rm.logger.Info("Step needs more info", "step_id", step.ID)
	}

	return revisions, outErr
}

// publishReviewEvent publishes a review completion event.
func (rm *ReviewManager) publishReviewEvent(stepID, taskID string, result *ReviewResult, revisionCount int) {
	if rm.bus == nil {
		return
	}

	msg, err := models.NewBusMessage(models.MessageTypeEvent, "review-manager", map[string]any{
		KeyStepID:        stepID,
		KeyTaskID:        taskID,
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

	// Also publish under task.* prefix for backward compatibility with
	// subscribers (TUI, ChatHandler) that subscribe to task.* but not step.*.
	taskMsg, taskErr := models.NewBusMessage(models.MessageTypeEvent, "review-manager", map[string]any{
		KeyStepID:        stepID,
		KeyTaskID:        taskID,
		"status":         string(result.Status),
		"feedback":       result.Feedback,
		"confidence":     result.Confidence,
		"reviewer":       result.ReviewerID,
		"revision_count": revisionCount,
	})
	if taskErr != nil {
		rm.logger.Error("Failed to create task.review_completed message", "error", taskErr)
		return
	}
	rm.bus.Publish("task.review_completed", taskMsg)
}

// SetPolicy updates the review policy.
func (rm *ReviewManager) SetPolicy(policy *ReviewPolicy) {
	if policy == nil {
		return
	}
	rm.mu.Lock()
	rm.policy = policy
	rm.mu.Unlock()
	rm.logger.Info("Review policy updated")
}

// ValidateCompletion checks that all assigned work for a step is actually done.
// It examines the step's result, evidence, and claims to determine if the work
// described in the step was fully completed.
func (rm *ReviewManager) ValidateCompletion(ctx context.Context, step *task.TaskStep, taskDesc string) (*ValidationResult, error) {
	// Snapshot validation policy under lock to avoid racing with
	// SetValidationPolicy (S1-17).
	rm.mu.RLock()
	validationPolicy := rm.validationPolicy
	rm.mu.RUnlock()

	// Check if validation is needed
	if !validationPolicy.NeedsValidation(step) {
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
	if step.ToolHint == string(IntentCode) || step.ToolHint == KeywordRefactor || step.ToolHint == KeywordFix {
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
func (rm *ReviewManager) checkClaimsAgainstDescription(step *task.TaskStep) (verified, missing []string) {
	verified = nil
	missing = nil

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
		errorIndicators := []string{string(MessageTypeError), "failed", "could not", "unable to", "not found"}
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
	if policy == nil {
		return
	}
	rm.mu.Lock()
	rm.validationPolicy = policy
	rm.mu.Unlock()
	rm.logger.Info("Validation policy updated")
}

// GetValidationPolicy returns the current validation policy.
func (rm *ReviewManager) GetValidationPolicy() *ValidationPolicy {
	rm.mu.RLock()
	defer rm.mu.RUnlock()
	return rm.validationPolicy
}

// GetPolicy returns the current review policy.
func (rm *ReviewManager) GetPolicy() *ReviewPolicy {
	rm.mu.RLock()
	defer rm.mu.RUnlock()
	return rm.policy
}

// stepHasError returns true if the step result appears to contain an error
// or failure message, making a review redundant.
//
// The indicators are checked against the STRUCTURED envelope, not free
// prose. The step-job result (components.go) is a JSON envelope whose
// "response" field is the model's own narration: a successful agent that
// merely MENTIONS a past failure ("an initial draft failed to link, so I
// switched to a library package") must not be judged failed by scanning
// its narrative. The sweep found exactly that false positive (e2e
// 2026-09-15: a coder step with passing vet/build/test was rejected
// because its narration contained "failed to link"). Parse the envelope;
// if it parses, judge success by the envelope's "success" flag and keep
// the prose scan only for the structured "error" field. Non-JSON results
// (legacy free-text steps) still get the prose scan.
func (rm *ReviewManager) stepHasError(step *task.TaskStep) bool {
	result := strings.TrimSpace(step.Result)
	if result == "" {
		return false // Empty result: will be caught by later heuristic, not "error"
	}

	// Structured envelope path (preferred): authoritative success flag.
	var envelope struct {
		Success bool   `json:"success"`
		Status  string `json:"status"`
		Error   string `json:"error"`
	}
	if err := json.Unmarshal([]byte(result), &envelope); err == nil {
		if envelope.Error != "" {
			return true
		}
		if envelope.Status == "failed" {
			return true
		}
		// Envelope parsed: the processor's success flag is authoritative.
		// Narrative text inside "response"/"evidence" is not re-scanned —
		// agents legitimately describe past failures there.
		return false
	}

	// Legacy free-text path.
	lower := strings.ToLower(result)
	errorIndicators := []string{
		"message:error",
		"message: error",
		"error executing",
		"error running",
		"error:",
		"failed to",
		"could not",
		"unable to",
		"rpc error",
		"permission denied",
		"access denied",
		"not found",
		"timeout",
		"connection refused",
	}
	for _, indicator := range errorIndicators {
		if strings.Contains(lower, indicator) {
			return true
		}
	}
	return false
}

// isTrivialTask returns true if the task has fewer than 3 steps.
// Low step-count tasks are trivial enough that an LLM review's token cost
// outweighs any benefit from a thorough inspection.
func (rm *ReviewManager) isTrivialTask(step *task.TaskStep) bool {
	if rm.stepStore == nil {
		return false // Cannot determine task size, default to full review
	}
	steps, err := rm.stepStore.ListByTaskID(step.TaskID)
	if err != nil {
		rm.logger.Debug("Failed to list task steps for review gating",
			"task_id", step.TaskID, "error", err)
		return false
	}
	return len(steps) < 3
}

// heuristicReviewPasses performs a lightweight, non-LLM check to determine
// whether a low-risk step should pass review. It returns true when the
// step has a non-empty, meaningful result — the default state for benign
// operations that did not fail. Error-shaped results never pass: a long
// error string is still an error.
func (rm *ReviewManager) heuristicReviewPasses(step *task.TaskStep) bool {
	if rm.stepHasError(step) {
		return false
	}
	result := strings.TrimSpace(step.Result)
	if result == "" {
		return false
	}
	// If result is purely whitespace or very short (no meaningful content)
	if len(result) < 3 {
		return false
	}
	// Reasoning-only guard (2026-09-10, outcome-loop L3 session): the
	// reasoning-watchdog writes "[reasoning-only turn]" assistant markers
	// and its terminate path returns the canned "I stopped after extended
	// thinking" text as the step result. Neither is evidence of work —
	// both are the loop GIVING UP. A canned no-tool termination must not
	// ride the trivial-task heuristic to auto-approval.
	if !reviewHintIsConversational(step.ToolHint) &&
		strings.Contains(result, "stopped after extended thinking") &&
		!hasMeaningfulEvidence(step.Evidence) {
		rm.logger.Warn("Heuristic review: reasoning-only termination with no tool evidence; refusing auto-approve",
			"step_id", step.ID,
			"tool_hint", step.ToolHint,
		)
		return false
	}
	// Claim-without-action guard (e2e run 7, 2026-09-10): the coder returned
	// a structured report asserting file_exists evidence WITHOUT calling any
	// tool (iterations=1, zero tool executions) and this heuristic
	// auto-approved it — a hallucinated completion. An execution step whose
	// report claims artifacts but which shows no tool activity is not
	// verifiable; route it to the full reviewer instead of waving it
	// through. Memory/analysis hints legitimately produce reports without
	// tools, so scope the suspicion to artifact claims only.
	//
	// F9 (2026-09-12 bughunt): the predicate MUST be the structural one.
	// The daemon's step-job envelope encodes `evidence` as a []string of
	// prose, so decoding into []models.Evidence leaves a ONE-element slice
	// holding a zero-value Evidence — len(step.Evidence) == 0 is therefore
	// never true for a job-driven step and this refusal was unreachable.
	if step.ToolHint != "" && !reviewHintIsConversational(step.ToolHint) && !hasMeaningfulEvidence(step.Evidence) {
		if claimsArtifacts(result) {
			rm.logger.Warn("Heuristic review: artifact claims with no tool evidence; refusing auto-approve",
				"step_id", step.ID,
				"tool_hint", step.ToolHint,
			)
			return false
		}
	}
	// Tool-execution-claim guard (agent sweep e2e, 2026-09-15): chat/analyst
	// steps reported "Extraction ran clean on the first pass" — claiming a
	// json_extract tool execution — with zero extraction-model calls having
	// been routed (metrics.db: 0 llm_calls to local-extract). These hints
	// are conversational, so the artifact guard above does not apply; but a
	// CONVERSATIONAL agent claiming it RAN a named tool is the same
	// hallucination shape. Any result asserting a specific tool execution
	// with no meaningful tool evidence routes to full review.
	if !hasMeaningfulEvidence(step.Evidence) && claimsToolExecution(result) {
		rm.logger.Warn("Heuristic review: tool-execution claims with no tool evidence; refusing auto-approve",
			"step_id", step.ID,
			"tool_hint", step.ToolHint,
		)
		return false
	}
	return true
}

// reviewHintIsConversational reports whether a tool hint denotes work whose
// product IS the report (no artifact claims expected).
func reviewHintIsConversational(hint string) bool {
	switch strings.ToLower(strings.TrimSpace(hint)) {
	case "chat", "report", "recall", "research", "analyze", "analyst", "plan":
		return true
	}
	return false
}

// claimsToolExecution reports whether a result asserts that a specific
// NAMED tool was executed ("extraction ran clean", "called json_extract",
// "ran the web_fetch tool"). Sweep e2e 2026-09-15: conversational agents
// fabricated tool-execution reports with zero tool activity behind them.
// Generic phrases like "used my tools" don't match; the claim must name a
// tool-ish noun or a tool-run verb.
func claimsToolExecution(result string) bool {
	lower := strings.ToLower(result)
	for _, marker := range []string{
		"extraction ran", "extraction ran clean",
		"called the json_extract", "called json_extract",
		"ran the json_extract", "json_extract tool",
		"called the web_fetch", "ran web_fetch", "web_fetch tool",
		"called the websearch", "websearch tool",
		"shell_exec tool", "transcript_fetch tool",
		"via json_extract", "via web_fetch", "via websearch",
	} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

// claimsArtifacts reports whether a result string asserts file/artifact
// creation or existence — the shape the run-7 hallucinated report used.
func claimsArtifacts(result string) bool {
	lower := strings.ToLower(result)
	for _, marker := range []string{
		"file_exists", "created file", "wrote file", "file written",
		"created hello", "\"created ", "created the file", "wrote the file",
	} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

// hasMeaningfulEvidence reports whether the step's Evidence slice carries
// at least one entry a tool actually produced (non-zero Type, Subject, or
// Value). A zero-value Evidence is a decode artifact: the step-job result
// envelope encodes `evidence` as a []string of prose, and unmarshaling a
// string into models.Evidence leaves a zero struct IN the slice (Go keeps
// the element with an UnmarshalTypeError but continues decoding). The
// failed-smoke step (task-20260911211112.532862000-0002) carried exactly
// one such zero entry, defeating the len(evidence)==0 check. Structurally
// empty evidence is not evidence.
func hasMeaningfulEvidence(evs []models.Evidence) bool {
	for _, e := range evs {
		if e.Type != "" || e.Subject != "" || e.Value != "" {
			return true
		}
	}
	return false
}
