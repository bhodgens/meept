// Package lifecycle provides skill lifecycle management: usage tracking,
// atomic skill file writes, verification, and versioned bundles.
//
// This package is the foundation for the closed-loop skill evolution system.
// Phase 1 provides usage tracking (SQLite-backed) and a Writer for atomic
// skill file operations.
package lifecycle

import (
	"errors"
	"time"
)

// ErrDuplicateContent is returned by Writer.WriteSkill when the new content's
// SHA-256 matches an already-indexed skill's content. Identical content means
// the write would be a no-op from a content perspective, so we skip it.
var ErrDuplicateContent = errors.New("duplicate skill content")

// Outcome represents the outcome of a skill injection (whether the skill
// helped, hurt, or was neutral for the conversation turn).
type Outcome int

const (
	// OutcomePositive means the skill contributed positively to the turn
	// (e.g. judgment quality >= 0.7 and ShouldLearn is true).
	OutcomePositive Outcome = iota
	// OutcomeNegative means the skill did not help (ShouldLearn is false).
	OutcomeNegative
	// OutcomeNeutral means the skill was used but the outcome is ambiguous
	// (e.g. quality is between thresholds).
	OutcomeNeutral
)

// String returns the string representation of an Outcome.
func (o Outcome) String() string {
	switch o {
	case OutcomePositive:
		return "positive"
	case OutcomeNegative:
		return "negative"
	case OutcomeNeutral:
		return "neutral"
	default:
		return "unknown"
	}
}

// ParseOutcome converts a string to an Outcome. Returns OutcomeNeutral for
// unrecognized strings (defensive — never fails).
func ParseOutcome(s string) Outcome {
	switch s {
	case "positive":
		return OutcomePositive
	case "negative":
		return OutcomeNegative
	default:
		return OutcomeNeutral
	}
}

// UsageStats holds aggregate usage statistics for a single skill.
type UsageStats struct {
	SkillName      string    `json:"skill_name"`
	InjectCount    int       `json:"inject_count"`
	PositiveCount  int       `json:"positive_count"`
	NegativeCount  int       `json:"negative_count"`
	NeutralCount   int       `json:"neutral_count"`
	LastInjectedAt time.Time `json:"last_injected_at"`
	LastUsedAt     time.Time `json:"last_used_at"`
	// Effectiveness = PositiveCount / InjectCount (0 when InjectCount == 0).
	Effectiveness float64 `json:"effectiveness"`
}

// SkillEvent represents a single usage event in the event log.
type SkillEvent struct {
	ID         string    `json:"id"`
	SkillName  string    `json:"skill_name"`
	EventType  string    `json:"event_type"` // "inject" or "outcome"
	Outcome    string    `json:"outcome,omitempty"` // populated for outcome events
	SessionID  string    `json:"session_id,omitempty"`
	Timestamp  time.Time `json:"timestamp"`
}

// VersionEntry represents a single versioned snapshot of a skill, recorded in
// the version bundle manifest (bundle.json).
type VersionEntry struct {
	Version    int       `json:"version"`
	ContentSHA string    `json:"content_sha"`
	Timestamp  time.Time `json:"timestamp"`
	Action     string    `json:"action"`
	TreeSHA256 string    `json:"tree_sha256"`
}

// VerifyAction is the accept/reject verdict produced by the Verifier.
type VerifyAction string

const (
	// ActionAccept means the proposed skill change passed the quality gate.
	ActionAccept VerifyAction = "accept"
	// ActionReject means the proposed skill change failed the quality gate.
	ActionReject VerifyAction = "reject"
)

// Dimensions holds the four rubric scores (each in [0,1]) used by the Verifier
// to decide whether a proposed skill change is safe to apply.
//
//   - GroundedInEvidence     — is the change backed by observed usage data?
//   - PreservesExistingValue — does it keep the useful parts of the current skill?
//   - SpecificityAndReusability — is the result general enough to apply again?
//   - SafeToPublish          — is it free of harmful or unstable instructions?
type Dimensions struct {
	GroundedInEvidence        float64 `json:"grounded_in_evidence"`
	PreservesExistingValue    float64 `json:"preserves_existing_value"`
	SpecificityAndReusability float64 `json:"specificity_and_reusability"`
	SafeToPublish             float64 `json:"safe_to_publish"`
}

// VerifyRequest carries the inputs needed to evaluate a proposed skill change.
// The Verifier compares the candidate content against the current content and
// the supplied evidence (typically from Phase 1's UsageStats).
type VerifyRequest struct {
	// Action describes the kind of change being proposed (e.g. "improve_skill",
	// "create_skill", "archive_skill").
	Action string `json:"action"`
	// SkillName is the target skill being modified (empty for new skills).
	SkillName string `json:"skill_name"`
	// CandidateContent is the proposed new content for the skill file.
	CandidateContent string `json:"candidate_content"`
	// CurrentContent is the existing content of the skill file (empty for new).
	CurrentContent string `json:"current_content"`
	// EvidenceSummary is a human-readable summary of usage stats and learned
	// patterns that motivated this proposal.
	EvidenceSummary string `json:"evidence_summary"`
}

// VerificationResult is the output of Verifier.Verify. It contains the
// accept/reject decision, the four dimension scores, the overall average, and
// human-readable reasons explaining the verdict.
type VerificationResult struct {
	Action     VerifyAction `json:"action"`
	Score      float64      `json:"score"` // mean of the four dimensions
	Reasons    []string     `json:"reasons"`
	Dimensions Dimensions   `json:"dimensions"`
}

// EvolutionProposalAction labels what the evolver wants to do with a skill.
type EvolutionProposalAction string

const (
	// ProposalRefine means improve an existing skill's content.
	ProposalRefine EvolutionProposalAction = "improve"
	// ProposalCreate means promote a learned pattern into a new skill.
	ProposalCreate EvolutionProposalAction = "create"
	// ProposalArchive means prune (archive) a low-performing skill.
	ProposalArchive EvolutionProposalAction = "archive"
)

// EvolutionProposal represents a single proposed skill change produced by one
// of the three evolver passes (refine, promote, prune). Every proposal passes
// through the Verifier before it is applied or turned into a plan.
type EvolutionProposal struct {
	Action            EvolutionProposalAction `json:"action"`
	SkillName         string                  `json:"skill_name"`
	Rationale         string                  `json:"rationale"`
	CandidateContent  string                  `json:"candidate_content,omitempty"`
	VerifierResult    *VerificationResult     `json:"verifier_result,omitempty"`
}

// EvolutionReport is the aggregate result of a single Evolver.RunCycle pass.
// It contains counts for each disposition plus the full list of proposals
// (including rejected ones, so callers can audit the decision trail).
type EvolutionReport struct {
	Refined  int                 `json:"refined"`   // Pass A: skills improved
	Promoted int                 `json:"promoted"`  // Pass B: patterns promoted
	Pruned   int                 `json:"pruned"`    // Pass C: skills archived
	Skipped  int                 `json:"skipped"`   // proposals not applied (LLM said skip)
	Rejected int                 `json:"rejected"`  // verifier rejected
	Planned  int                 `json:"planned"`   // AutoApply=false → plan created
	Details  []EvolutionProposal `json:"details"`
}
