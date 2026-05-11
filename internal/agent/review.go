package agent

import (
	"slices"
	"strings"
	"time"

	"github.com/caimlas/meept/internal/task"
)

// ReviewStatus represents the outcome of a review.
type ReviewStatus string

const (
	ReviewApproved  ReviewStatus = "approved"
	ReviewRejected  ReviewStatus = "rejected"
	ReviewNeedsInfo ReviewStatus = "needs_info"
)

// ReviewResult represents the outcome of a review.
type ReviewResult struct {
	Status     ReviewStatus  `json:"status"`
	Feedback   string        `json:"feedback"`
	Issues     []string      `json:"issues,omitempty"`
	Confidence float64       `json:"confidence"`
	ReviewerID string        `json:"reviewer_id"`
	Duration   time.Duration `json:"duration"`
}

// ReviewPolicy determines which steps require review and how.
type ReviewPolicy struct {
	// Tool hints that ALWAYS require review
	RequireReview []string

	// Tool hints that NEVER require review (trusted operations)
	SkipReview []string

	// Agent-specific reviewer mappings
	// e.g., coder → code-reviewer, debugger → debug-reviewer
	ReviewerMapping map[string]string

	// Maximum revision cycles before requiring human intervention
	MaxRevisionCycles int

	// File patterns that auto-approve (low-risk changes)
	AutoApprovePatterns []string

	// Whether review is enabled globally
	Enabled bool
}

// DefaultReviewPolicy returns sensible defaults for review policy.
func DefaultReviewPolicy() *ReviewPolicy {
	return &ReviewPolicy{
		RequireReview: []string{"code", "refactor", "debug", "git", "fix"},
		SkipReview:    []string{"chat", "report", "recall", "search", "analyze"},
		ReviewerMapping: map[string]string{
			"coder":     "code-reviewer",
			"debugger":  "debug-reviewer",
			"planner":   "planner-reviewer",
			"analyst":   "analyst-reviewer",
			"committer": "code-reviewer",
		},
		MaxRevisionCycles: 3,
		AutoApprovePatterns: []string{
			"*.md",
			"LICENSE",
			"*.txt",
			"*.json",
			"*.yaml",
			"*.yml",
		},
		Enabled: true,
	}
}

// NeedsReview determines if a step requires review based on policy.
func (p *ReviewPolicy) NeedsReview(step *task.TaskStep) bool {
	if !p.Enabled {
		return false
	}

	// Check if tool hint is in skip list
	if slices.Contains(p.SkipReview, step.ToolHint) {
		return false
	}

	// Check if tool hint is in require list
	if slices.Contains(p.RequireReview, step.ToolHint) {
		return true
	}

	// Default: review if tool hint is set
	return step.ToolHint != ""
}

// SelectReviewer selects the appropriate reviewer agent for a step.
func (p *ReviewPolicy) SelectReviewer(step *task.TaskStep) string {
	// If step has an agent assigned, map to reviewer
	if step.AgentID != "" {
		if reviewer, ok := p.ReviewerMapping[step.AgentID]; ok {
			return reviewer
		}
	}

	// Map tool hint to reviewer
	switch step.ToolHint {
	case "code", "refactor":
		return "code-reviewer"
	case "debug", "fix":
		return "debug-reviewer"
	case "plan":
		return "planner-reviewer"
	case "analyze", "research":
		return "analyst-reviewer"
	case "git", "commit":
		return "code-reviewer"
	default:
		return "test-reviewer" // Default reviewer
	}
}

// ShouldAutoApprove checks if a step should auto-approve based on its description.
func (p *ReviewPolicy) ShouldAutoApprove(step *task.TaskStep) bool {
	desc := strings.ToLower(step.Description)

	// Check for documentation-only changes
	for _, pattern := range p.AutoApprovePatterns {
		if strings.Contains(desc, pattern) {
			// Only auto-approve if it looks like a simple doc change
			if strings.Contains(desc, "update") || strings.Contains(desc, "fix") {
				// Check if there are code-related keywords
				if !strings.Contains(desc, "code") && !strings.Contains(desc, "function") &&
					!strings.Contains(desc, "class") && !strings.Contains(desc, "implement") {
					return true
				}
			}
		}
	}

	return false
}

// ExceedsMaxRevisions checks if a step has exceeded the maximum revision cycles.
func (p *ReviewPolicy) ExceedsMaxRevisions(step *task.TaskStep) bool {
	if p.MaxRevisionCycles <= 0 {
		return false
	}
	return step.RevisionCount >= p.MaxRevisionCycles
}

// RequiresHumanIntervention returns true if human intervention is needed.
func (p *ReviewPolicy) RequiresHumanIntervention(step *task.TaskStep) bool {
	return p.ExceedsMaxRevisions(step)
}

// ValidationPolicy determines which steps require validation and how.
type ValidationPolicy struct {
	// Enabled globally
	Enabled bool

	// Tool hints that ALWAYS require validation
	RequireValidation []string

	// Tool hints that NEVER require validation (trusted operations)
	SkipValidation []string

	// Maximum validation retry loops before escalation
	MaxValidationLoops int

	// Skip validation for these agents
	SkipValidationAgents []string
}

// DefaultValidationPolicy returns sensible defaults for validation policy.
func DefaultValidationPolicy() *ValidationPolicy {
	return &ValidationPolicy{
		Enabled:              true,
		RequireValidation:    []string{"code", "refactor", "debug", "git", "fix", "commit"},
		SkipValidation:       []string{"chat", "report", "recall", "search", "analyze", "platform"},
		MaxValidationLoops:   3,
		SkipValidationAgents: []string{"chat", "analyst"},
	}
}

// NeedsValidation determines if a step requires validation based on policy.
func (p *ValidationPolicy) NeedsValidation(step *task.TaskStep) bool {
	if !p.Enabled {
		return false
	}

	// Check if tool hint is in skip list
	if slices.Contains(p.SkipValidation, step.ToolHint) {
		return false
	}

	// Check if agent is in skip list
	if slices.Contains(p.SkipValidationAgents, step.AgentID) {
		return false
	}

	// Check if tool hint is in require list
	if slices.Contains(p.RequireValidation, step.ToolHint) {
		return true
	}

	// Default: validation if tool hint is set and not in skip lists
	return step.ToolHint != ""
}

// ExceedsMaxValidationLoops checks if a step has exceeded validation retry loops.
func (p *ValidationPolicy) ExceedsMaxValidationLoops(validationLoops int) bool {
	return p.MaxValidationLoops > 0 && validationLoops >= p.MaxValidationLoops
}
