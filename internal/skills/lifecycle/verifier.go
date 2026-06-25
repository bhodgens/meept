package lifecycle

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/util/markdown"
)

// defaultMinScore is the default minimum overall average score required for
// acceptance. The Verifier rejects any proposal whose overall average falls
// below this threshold.
const defaultMinScore = 0.75

// dimensionFloor is the per-dimension floor. If any single dimension falls
// below this value the proposal is rejected regardless of the overall average.
const dimensionFloor = 0.5

// Verifier is the skill-change quality gate. Before the evolver (Phase 2)
// applies any proposed skill modification, it calls Verify to ensure the
// change meets quality standards across four dimensions.
//
// When an LLM client is available, Verify sends a single prompt containing the
// current and candidate skill content plus the evidence summary, then parses
// the four-dimension JSON response. When no LLM client is configured, Verify
// falls back to a neutral heuristic that scores 0.5 on every dimension — this
// leans toward rejection under the default 0.75 threshold, ensuring that
// unverified changes never go live silently.
type Verifier struct {
	llmClient *llm.Client
	logger    *slog.Logger
	minScore  float64
}

// VerifierOption configures a Verifier at construction time.
type VerifierOption func(*Verifier)

// WithMinScore sets the minimum overall average score required for acceptance.
// Proposals whose overall average falls below this value are rejected. The
// default is 0.75. The per-dimension floor (0.5) is always enforced
// regardless of this setting.
func WithMinScore(score float64) VerifierOption {
	return func(v *Verifier) {
		v.minScore = score
	}
}

// NewVerifier constructs a Verifier backed by the given LLM client. If
// llmClient is nil, Verify uses a heuristic fallback (all dims = 0.5). The
// logger must be non-nil; if it is, a default logger is used.
func NewVerifier(llmClient *llm.Client, logger *slog.Logger, opts ...VerifierOption) *Verifier {
	if logger == nil {
		logger = slog.Default()
	}
	v := &Verifier{
		llmClient: llmClient,
		logger:    logger,
		minScore:  defaultMinScore,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(v)
		}
	}
	return v
}

const verifierSystemPrompt = `You are a skill quality reviewer for an AI agent platform. You evaluate proposed changes to skill files (markdown documents that guide agent behavior). Score the proposal on four dimensions, each in [0, 1]:

1. grounded_in_evidence — Is the change backed by concrete usage data or observed problems? (0 = no evidence, 1 = strongly evidence-backed)
2. preserves_existing_value — Does it retain the useful parts of the current skill? (0 = discards value, 1 = preserves and enhances)
3. specificity_and_reusability — Is the result specific enough to be useful but general enough to apply across contexts? (0 = too vague or too narrow, 1 = well-balanced)
4. safe_to_publish — Is it free of harmful, unstable, or dangerous instructions? (0 = unsafe, 1 = clearly safe)

Respond in JSON format:
{
  "grounded_in_evidence": 0.8,
  "preserves_existing_value": 0.9,
  "specificity_and_reusability": 0.7,
  "safe_to_publish": 0.85,
  "reasons": ["brief explanation per dimension"]
}`

// Verify evaluates a proposed skill change against the four-dimension rubric.
// Returns a VerificationResult containing the accept/reject decision, the
// dimension scores, the overall average, and human-readable reasons.
//
// When the LLM client is nil or the call fails, Verify falls back to a neutral
// heuristic (all dimensions = 0.5). Under the default minScore of 0.75, the
// heuristic path always rejects — ensuring unverified changes never go live.
func (v *Verifier) Verify(ctx context.Context, req VerifyRequest) (*VerificationResult, error) {
	if v.llmClient == nil {
		v.logger.Debug("verifier: LLM client unavailable, using heuristic fallback",
			"action", req.Action, "skill", req.SkillName)
		return v.verifyHeuristic(req), nil
	}

	prompt := v.buildPrompt(req)

	messages := []llm.ChatMessage{
		{Role: llm.RoleSystem, Content: verifierSystemPrompt},
		{Role: llm.RoleUser, Content: prompt},
	}

	resp, err := v.llmClient.Chat(ctx, messages)
	if err != nil {
		v.logger.Warn("verifier: LLM call failed, using heuristic fallback",
			"error", err, "action", req.Action, "skill", req.SkillName)
		return v.verifyHeuristic(req), nil
	}

	dims, reasons, err := parseVerifierResponse(resp.Content)
	if err != nil {
		v.logger.Warn("verifier: failed to parse LLM response, using heuristic fallback",
			"error", err, "action", req.Action, "skill", req.SkillName)
		return v.verifyHeuristic(req), nil
	}

	return decide(dims, reasons, v.minScore), nil
}

// buildPrompt constructs the user prompt sent to the LLM for verification.
func (v *Verifier) buildPrompt(req VerifyRequest) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Proposed action: %s\n", req.Action)
	if req.SkillName != "" {
		fmt.Fprintf(&sb, "Skill name: %s\n", req.SkillName)
	}
	sb.WriteString("\n--- Evidence Summary ---\n")
	if req.EvidenceSummary != "" {
		sb.WriteString(req.EvidenceSummary)
	} else {
		sb.WriteString("(no evidence provided)")
	}
	sb.WriteString("\n\n--- Current Skill Content ---\n")
	if req.CurrentContent != "" {
		sb.WriteString(req.CurrentContent)
	} else {
		sb.WriteString("(none — new skill)")
	}
	sb.WriteString("\n\n--- Candidate Skill Content ---\n")
	if req.CandidateContent != "" {
		sb.WriteString(req.CandidateContent)
	} else {
		sb.WriteString("(empty)")
	}
	sb.WriteString("\n\nScore the proposal on the four dimensions. Return JSON.")
	return sb.String()
}

// verifyHeuristic is the fallback path used when the LLM client is nil or the
// LLM call fails. It assigns 0.5 to every dimension — a neutral score that
// leans toward rejection under the default 0.75 threshold.
func (v *Verifier) verifyHeuristic(req VerifyRequest) *VerificationResult {
	dims := Dimensions{
		GroundedInEvidence:        0.5,
		PreservesExistingValue:    0.5,
		SpecificityAndReusability: 0.5,
		SafeToPublish:             0.5,
	}
	reasons := []string{
		"heuristic fallback: LLM unavailable, using neutral 0.5 scores",
	}
	return decide(dims, reasons, v.minScore)
}

// parseVerifierResponse extracts the four dimension scores and optional
// reasons from the LLM's JSON response. Follows the same pattern as
// internal/selfimprove/learning.go:parseJudgmentResponse.
func parseVerifierResponse(content string) (Dimensions, []string, error) {
	jsonData := markdown.ExtractJSON(content)
	if jsonData == nil {
		return Dimensions{}, nil, fmt.Errorf("no valid JSON found in verifier response")
	}

	var parsed struct {
		GroundedInEvidence        float64  `json:"grounded_in_evidence"`
		PreservesExistingValue    float64  `json:"preserves_existing_value"`
		SpecificityAndReusability float64  `json:"specificity_and_reusability"`
		SafeToPublish             float64  `json:"safe_to_publish"`
		Reasons                   []string `json:"reasons"`
	}

	if err := json.Unmarshal(jsonData, &parsed); err != nil {
		return Dimensions{}, nil, fmt.Errorf("unmarshal verifier response: %w", err)
	}

	dims := Dimensions{
		GroundedInEvidence:        clamp01(parsed.GroundedInEvidence),
		PreservesExistingValue:    clamp01(parsed.PreservesExistingValue),
		SpecificityAndReusability: clamp01(parsed.SpecificityAndReusability),
		SafeToPublish:             clamp01(parsed.SafeToPublish),
	}
	return dims, parsed.Reasons, nil
}

// decide is the pure decision function that translates dimension scores into
// an accept/reject verdict. It is extracted from the Verify method so that
// tests can exercise the threshold logic without an LLM.
//
// Rejection rules (any one triggers rejection):
//   - any dimension < dimensionFloor (0.5)
//   - overall average < minScore
//
// Acceptance requires all dimensions >= dimensionFloor AND overall average >=
// minScore.
func decide(dims Dimensions, reasons []string, minScore float64) *VerificationResult {
	score := (dims.GroundedInEvidence +
		dims.PreservesExistingValue +
		dims.SpecificityAndReusability +
		dims.SafeToPublish) / 4.0

	// Build reasons if none provided.
	if reasons == nil {
		reasons = []string{}
	}

	// Check per-dimension floor.
	rejected := false
	dimNames := []struct {
		name  string
		value float64
	}{
		{"grounded_in_evidence", dims.GroundedInEvidence},
		{"preserves_existing_value", dims.PreservesExistingValue},
		{"specificity_and_reusability", dims.SpecificityAndReusability},
		{"safe_to_publish", dims.SafeToPublish},
	}
	for _, d := range dimNames {
		if d.value < dimensionFloor {
			reasons = append(reasons, fmt.Sprintf(
				"%s score %.2f is below floor %.2f", d.name, d.value, dimensionFloor))
			rejected = true
		}
	}

	// Check overall average.
	if score < minScore {
		reasons = append(reasons, fmt.Sprintf(
			"overall score %.2f is below minimum %.2f", score, minScore))
		rejected = true
	}

	action := ActionAccept
	if rejected {
		action = ActionReject
	}

	return &VerificationResult{
		Action:     action,
		Score:      score,
		Reasons:    reasons,
		Dimensions: dims,
	}
}

// clamp01 constrains a float to the [0, 1] range. LLMs occasionally return
// values slightly outside the expected range.
func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}
