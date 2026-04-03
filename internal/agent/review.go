package agent

import (
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
	Status     ReviewStatus `json:"status"`
	Feedback   string       `json:"feedback"`
	Issues     []string     `json:"issues,omitempty"`
	Confidence float64      `json:"confidence"`
	ReviewerID string       `json:"reviewer_id"`
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
	for _, skip := range p.SkipReview {
		if step.ToolHint == skip {
			return false
		}
	}

	// Check if tool hint is in require list
	for _, req := range p.RequireReview {
		if step.ToolHint == req {
			return true
		}
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
