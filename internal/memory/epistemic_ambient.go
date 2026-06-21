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
const ambientExtractionPromptTemplate = `You are an epistemic extractor. Read the following conversation segment and
extract assertions of belief (claims), forward-looking commitments (decisions),
and forecasts (predictions). For each candidate:

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
func ParseAmbientCandidates(raw []byte) ([]AmbientCandidate, error) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" {
		return nil, errors.New("empty ambient classifier response")
	}
	trimmed = stripCodeFences(trimmed)
	var out []AmbientCandidate
	if err := json.Unmarshal([]byte(trimmed), &out); err != nil {
		return nil, fmt.Errorf("unmarshal candidates: %w", err)
	}
	return out, nil
}
