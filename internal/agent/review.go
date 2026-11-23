package agent

import (
	"path"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/modelrating"
	"github.com/caimlas/meept/internal/task"
)

// codeFileToken matches a filename token with a SOURCE-CODE extension. A step
// naming one of these is never auto-approved, even when it also names a doc.
// Doc/config extensions (.md, .txt, .json, .yaml) are intentionally absent:
// those are the low-risk shapes in AutoApprovePatterns.
var codeFileToken = regexp.MustCompile(`(?i)\.(go|py|js|jsx|mjs|cjs|ts|tsx|cc|cpp|hpp|rs|java|kt|kts|rb|php|swift|sh|bash|zsh|sql|vue|svelte|dart|lua|zig|ex|exs|erl|hs|scala|clj|groovy|r|pl)$`)

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

	// Verdicts carries the rubric grades the review pass produced
	// (model-rating-metrics-plan C4). Additive + omitempty so every
	// non-emitting producer (pair_manager, inline-review lane) compiles
	// and serializes unchanged. The evidence gate and the ReviewManager
	// reviewer lane are the only v1 emitting producers.
	Verdicts []modelrating.Verdict `json:"verdicts,omitempty"`
}

// ReviewPolicy determines which steps require review and how.
type ReviewPolicy struct {
	// Tool hints that ALWAYS require review
	RequireReview []string

	// Tool hints that NEVER require review (trusted operations)
	SkipReview []string

	// Registry, when non-nil, is consulted by SelectReviewer to find
	// reviewer-role agents dynamically by reviews_domain.

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
// Reviewer routing is dynamic via ReviewPolicy.Registry and the
// reviews_domain field on reviewer-role agents.
func DefaultReviewPolicy() *ReviewPolicy {
	return &ReviewPolicy{
		RequireReview:     []string{string(IntentCode), KeywordRefactor, string(IntentDebug), string(IntentGit), KeywordFix},
		SkipReview:        []string{string(IntentChat), string(IntentReport), string(IntentRecall), string(IntentSearch), string(IntentAnalyze)},
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

// ReviewPolicyFromConfig builds the runtime review policy from [agent.review].
// Empty fields fall back to DefaultReviewPolicy values, so an installed home
// with no review block keeps the same posture as before this seam existed.
// A configured pattern list REPLACES the default list (operator owns it).
func ReviewPolicyFromConfig(c config.ReviewConfig) *ReviewPolicy {
	p := DefaultReviewPolicy()
	// A zero-value bool cannot express "unset". Keep the enabled default;
	// an operator who wants review off sets [agent] review.enabled=false,
	// which DefaultConfig already supplies as true on a fresh install.
	if c.RequireReview != nil || c.SkipReview != nil || c.AutoApprovePatterns != nil ||
		c.MaxRevisionCycles != 0 {
		p.Enabled = c.Enabled
	}
	if len(c.RequireReview) > 0 {
		p.RequireReview = append([]string(nil), c.RequireReview...)
	}
	if len(c.SkipReview) > 0 {
		p.SkipReview = append([]string(nil), c.SkipReview...)
	}
	if c.MaxRevisionCycles > 0 {
		p.MaxRevisionCycles = c.MaxRevisionCycles
	}
	if c.AutoApprovePatterns != nil {
		p.AutoApprovePatterns = append([]string(nil), c.AutoApprovePatterns...)
	}
	return p
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
	config.AgentIDCoder:      "code",
	config.AgentIDDebugger:   "debug",
	config.AgentIDPlanner:    "plan",
	config.AgentIDAnalyst:    "analysis",
	config.AgentIDCommitter:  "code",
	config.AgentIDResearcher: "analysis",
	config.AgentIDChat:       "test",
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
// Dynamic lookup via Registry for a reviewer-role agent
//
//	   reviews_domain matches the originating agent's domain
//	3. Tool-hint → domain → registry lookup
//	4. "test-reviewer" fallback
func (p *ReviewPolicy) SelectReviewer(step *task.TaskStep) string {
	// Determine target domain from agent ID, then tool hint.
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

// ShouldAutoApprove checks if a step is a low-risk, documentation-only change.
//
// Tightened by the dev-setup-safety audit (F-02): the old matcher searched the
// WHOLE description for a pattern substring plus "update"/"fix", so a step
// like "fix auth.py and update the handler" matched "*.md" only as a literal
// substring and could auto-approve a code change. The new rule requires an
// EXACT filename token: a whitespace-delimited word whose path.Base matches
// the pattern via path.Match. No token, no approval. A code-shaped token
// (any token with a source-code extension) vetoes the whole step.
func (p *ReviewPolicy) ShouldAutoApprove(step *task.TaskStep) bool {
	if step == nil {
		return false
	}

	// Keep original case: configured patterns such as "LICENSE" are
	// case-sensitive. Action-verb and code-extension checks below are
	// case-insensitive on their own.
	tokens := strings.FieldsFunc(step.Description, func(r rune) bool {
		return r == ' ' || r == '	' || r == '\n' || r == ',' || r == ';' || r == '"' || r == '\'' || r == '`'
	})

	// Require an explicit low-risk action verb as a whole word. "prefix-fix"
	// must not count as "fix". Absence fails closed.
	hasAction := false
	for _, token := range tokens {
		switch strings.ToLower(token) {
		case "update", "fix", "updated", "fixed":
			hasAction = true
		}
	}
	if !hasAction {
		return false
	}

	// A file-shaped step needs real successful file evidence before the
	// caller may take any shortcut (the review evidence gate). Here we
	// additionally refuse to auto-approve a step whose tokens name source
	// code, even when one token is a doc file.
	for _, token := range tokens {
		if codeFileToken.MatchString(token) {
			return false
		}
	}

	for _, token := range tokens {
		base := path.Base(token)
		// A glob written in prose ("use *.md style") is not a filename.
		// Only literal basenames may match AutoApprovePatterns.
		if strings.ContainsAny(base, "*?[]\\") {
			continue
		}
		for _, pattern := range p.AutoApprovePatterns {
			if ok, err := path.Match(pattern, base); err == nil && ok {
				return true
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
