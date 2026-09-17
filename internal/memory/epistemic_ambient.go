package memory

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
)

// AmbientCandidate is a single claim/decision/prediction extracted from
// conversation by the ambient extractor.
type AmbientCandidate struct {
	Type       string   // "claim", "decision", "prediction"
	Text       string   // the extracted assertion
	Source     string   // origin tag, typically "conversation"
	Confidence float64  // 0.0-1.0
	Premises   []string // supporting premises
	Category   string   // classifier-detected category
}

// AmbientClassifierLLM is the interface the ambient extractor uses to run
// its extraction prompt. Defined locally to avoid an import cycle on
// internal/llm.
type AmbientClassifierLLM interface {
	// ExtractCandidates calls the LLM with the configured prompt and returns
	// the raw JSON body.
	ExtractCandidates(ctx context.Context, prompt string) ([]byte, error)
}

// AmbientExtractorConfig holds construction parameters for AmbientExtractor.
type AmbientExtractorConfig struct {
	Manager    *Manager
	Classifier AmbientClassifierLLM
	Logger     *slog.Logger
}

// AmbientExtractor runs the ambient-extraction LLM prompt over a conversation
// window and persists the resulting claim candidates as auto claims.
type AmbientExtractor struct {
	manager    *Manager
	classifier AmbientClassifierLLM
	logger     *slog.Logger
}

// NewAmbientExtractor constructs an extractor from the given configuration.
func NewAmbientExtractor(cfg AmbientExtractorConfig) *AmbientExtractor {
	ex := &AmbientExtractor{
		manager:    cfg.Manager,
		classifier: cfg.Classifier,
		logger:     cfg.Logger,
	}
	if ex.logger == nil {
		ex.logger = slog.Default()
	}
	return ex
}

// ambientExtractionPromptTemplate is the LLM prompt template per spec
// section "Path B: Ambient extraction / LLM prompt template".
// SYNC REQUIREMENT: the eval harness mirrors this template verbatim as
// ambientPrompt in tools/memory-eval/grade.go — any wording change here MUST
// be applied identically there (guarded by
// TestAmbientExtractionPromptMatchesEvalHarness).
const ambientExtractionPromptTemplate = `You are an epistemic extractor. Read the following conversation segment and
extract assertions of belief (claims), forward-looking commitments (decisions),
and forecasts (predictions). For each candidate:

- Extract only assertions the USER commits to. An assistant's restatement,
  summary, or confirmation of what the user said is NOT a new candidate —
  skip it.
- Never split one assertion into fragments; each candidate must be a complete,
  self-contained assertion.
- Only extract statements the speaker is committing to, not hypotheticals,
  questions, sarcasm, jokes, or quotations of others' views.
- Skip pleasantries, agreements without content, and meta-conversation.

Return JSON array. Each element:
{
  "type": "claim" | "decision" | "prediction",
  "text": "<the assertion>",
  "source": "conversation",
  "confidence": 0.0-1.0,
  "premises": [],
  "category": "<one of: architecture, business, technical, prediction, opinion, methodology>"
}

If no candidates, return [].

Conversation:
%s`

// Extract runs the ambient extraction prompt over the given messages and
// returns filtered candidates. Returns nil, nil when the classifier is nil
// (graceful zero-value behaviour).
func (ex *AmbientExtractor) Extract(ctx context.Context, messages []string) ([]AmbientCandidate, error) {
	if ex == nil || ex.classifier == nil {
		return nil, nil
	}
	if len(messages) == 0 {
		return nil, nil
	}
	prompt := fmt.Sprintf(ambientExtractionPromptTemplate, strings.Join(messages, "\n"))
	raw, err := ex.classifier.ExtractCandidates(ctx, prompt)
	if err != nil {
		return nil, fmt.Errorf("ambient classifier call: %w", err)
	}
	candidates, err := ParseAmbientCandidates(raw)
	if err != nil {
		return nil, fmt.Errorf("parse ambient candidates: %w", err)
	}
	return candidates, nil
}

// WriteCandidates persists each candidate as an auto-claim and returns the
// resulting memory IDs.
func (ex *AmbientExtractor) WriteCandidates(ctx context.Context, candidates []AmbientCandidate) ([]string, error) {
	if ex == nil || ex.manager == nil {
		return nil, errors.New("ambient extractor not configured")
	}
	ids := make([]string, 0, len(candidates))
	for _, c := range candidates {
		id, err := ex.manager.StoreClaim(ctx, Claim{
			Text:       c.Text,
			Premises:   c.Premises,
			Source:     c.Source,
			Confidence: c.Confidence,
			Status:     ClaimStatusAuto,
		})
		if err != nil {
			ex.logger.Warn("ambient write candidate failed", "error", err)
			continue
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// ParseAmbientCandidates parses the raw JSON body returned by the LLM into
// AmbientCandidate values.
//
// Tolerated shapes (defense in depth — the grammar-constrained wire path in
// internal/llm forces the bare-array shape, but unconstrained endpoints do
// not, see tools/memory-eval findings):
//
//  1. bare JSON array (the contract shape),
//  2. an object wrapping the array under "candidates", "results", or "items",
//  3. markdown code fences around either of the above,
//  4. arbitrary prose surrounding a top-level JSON array (the first
//     bracket-balanced [...] block that unmarshals wins).
//
// Anything else is an error.
func ParseAmbientCandidates(raw []byte) ([]AmbientCandidate, error) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" {
		return nil, errors.New("empty ambient classifier response")
	}
	trimmed = stripCodeFences(trimmed)
	// Shape 1: bare JSON array. Byte-identical to the original behavior.
	var out []AmbientCandidate
	if err := json.Unmarshal([]byte(trimmed), &out); err == nil {
		return out, nil
	}
	// Shape 2: object wrapping the array under a known key.
	var wrapper struct {
		Candidates []AmbientCandidate `json:"candidates"`
		Results    []AmbientCandidate `json:"results"`
		Items      []AmbientCandidate `json:"items"`
	}
	if err := json.Unmarshal([]byte(trimmed), &wrapper); err == nil {
		switch {
		case wrapper.Candidates != nil:
			return wrapper.Candidates, nil
		case wrapper.Results != nil:
			return wrapper.Results, nil
		case wrapper.Items != nil:
			return wrapper.Items, nil
		}
	}
	// Shape 3: prose around a JSON array (bracket-balanced scan from
	// consolidation.go — string/escape aware).
	if block := extractJSONArray(trimmed); block != "" {
		var out []AmbientCandidate
		if err := json.Unmarshal([]byte(block), &out); err == nil {
			return out, nil
		}
	}
	return nil, errors.New("unmarshal candidates: no JSON array in ambient classifier response")
}
