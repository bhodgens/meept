package agent

import (
	"slices"
	"strings"
	"time"

	"github.com/caimlas/meept/internal/config"
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

	// Agent-specific reviewer mappings.
	//
	// Deprecated: reviewer routing is now dynamic via ReviewPolicy.Registry
	// and the reviews_domain field on reviewer-role agents. ReviewerMapping
	// is kept as an override escape hatch — entries here take precedence
	// over dynamic discovery so callers can pin specific reviewers.
	ReviewerMapping map[string]string

	// Maximum revision cycles before requiring human intervention
	MaxRevisionCycles int

	// File patterns that auto-approve (low-risk changes)
	AutoApprovePatterns []string

	// Whether review is enabled globally
	Enabled bool

	// Registry, when non-nil, is consulted by SelectReviewer to find
	// reviewer-role agents dynamically by reviews_domain. When nil,
	// SelectReviewer falls back to a tool-hint → domain mapping and
	// looks up reviewers in the registry if available, else returns
	// the test-reviewer fallback.
	Registry *AgentRegistry
}

// DefaultReviewPolicy returns sensible defaults for review policy.
//
// ReviewerMapping is empty by default: reviewer routing is dynamic via
// ReviewPolicy.Registry and the reviews_domain field on reviewer agents.
func DefaultReviewPolicy() *ReviewPolicy {
	return &ReviewPolicy{
		RequireReview: []string{string(IntentCode), KeywordRefactor, string(IntentDebug), string(IntentGit), KeywordFix},
		SkipReview:    []string{string(IntentChat), string(IntentReport), string(IntentRecall), string(IntentSearch), string(IntentAnalyze)},
		ReviewerMapping: map[string]string{},
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

// agentDomainMap maps originating agent IDs to their review domain. This is
// the fallback used when ReviewPolicy.Registry is nil or has no matching
// reviewer-role agent. Domains match the reviews_domain declared on the
// bundled reviewer AGENT.md files.
var agentDomainMap = map[string]string{
	config.AgentIDCoder:     "code",
	config.AgentIDDebugger:  "debug",
	config.AgentIDPlanner:   "plan",
	config.AgentIDAnalyst:   "analysis",
	config.AgentIDCommitter: "code",
	config.AgentIDResearcher: "analysis",
	config.AgentIDChat:      "test",
}

// toolHintDomainMap maps tool hints / intent keywords to review domains.
var toolHintDomainMap = map[string]string{
	string(IntentCode):     "code",
	KeywordRefactor:        "code",
	string(IntentDebug):    "debug",
	KeywordFix:             "debug",
	string(IntentPlan):     "plan",
	string(IntentAnalyze):  "analysis",
	string(IntentResearch): "analysis",
	string(IntentGit):      "code",
	KeywordCommit:          "code",
}

// SelectReviewer selects the appropriate reviewer agent for a step.
//
// Resolution order:
//  1. ReviewerMapping override (explicit pin)
//  2. Dynamic lookup via Registry for a reviewer-role agent whose
//     reviews_domain matches the originating agent's domain
//  3. Tool-hint → domain → registry lookup
//  4. "test-reviewer" fallback
func (p *ReviewPolicy) SelectReviewer(step *task.TaskStep) string {
	// 1. Explicit override wins.
	if step.AgentID != "" {
		if reviewer, ok := p.ReviewerMapping[step.AgentID]; ok && reviewer != "" {
			return reviewer
		}
	}

	// 2. Determine target domain from agent ID, then tool hint.
	domain := ""
	if step.AgentID != "" {
		domain = agentDomainMap[step.AgentID]
	}
	if domain == "" && step.ToolHint != "" {
		domain = toolHintDomainMap[step.ToolHint]
	}

	// 3. Dynamic lookup in the registry (if wired).
	if p.Registry != nil && domain != "" {
		if reviewer := p.Registry.findReviewerByDomain(domain); reviewer != "" {
			return reviewer
		}
	}

	// 4. Fallback to the generic test reviewer.
	return "test-reviewer"
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
		RequireValidation:    []string{string(IntentCode), KeywordRefactor, string(IntentDebug), string(IntentGit), KeywordFix, KeywordCommit},
		SkipValidation:       []string{string(IntentChat), string(IntentReport), string(IntentRecall), string(IntentSearch), string(IntentAnalyze), string(IntentPlatform)},
		MaxValidationLoops:   3,
		SkipValidationAgents: []string{config.AgentIDChat, config.AgentIDAnalyst},
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
