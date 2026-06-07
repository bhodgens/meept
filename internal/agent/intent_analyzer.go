package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/caimlas/meept/internal/llm"
)

const defaultAmbiguityThreshold = 0.6

// TrueIntentAnalysis represents a deep analysis of the user's actual intent.
type TrueIntentAnalysis struct {
	Goal               string   `json:"goal"`
	Ambiguity          float64  `json:"ambiguity"`
	Scope              string   `json:"scope"`
	Category           string   `json:"category"`
	SuggestedQuestions []string `json:"suggested_questions"`
	Confidence         float64  `json:"confidence"`
}

// IsAmbiguous returns true if the ambiguity score meets or exceeds the threshold.
func (a *TrueIntentAnalysis) IsAmbiguous(threshold float64) bool {
	return a.Ambiguity >= threshold
}

// IntentAnalyzer wraps an LLM client to perform deep intent analysis.
type IntentAnalyzer struct {
	client             *llm.Client
	ambiguityThreshold float64
	logger             *slog.Logger
}

// NewIntentAnalyzer creates a new IntentAnalyzer with the given LLM client and logger.
func NewIntentAnalyzer(client *llm.Client, logger *slog.Logger) *IntentAnalyzer {
	if logger == nil {
		logger = slog.Default()
	}
	return &IntentAnalyzer{
		client:             client,
		ambiguityThreshold: defaultAmbiguityThreshold,
		logger:             logger,
	}
}

// WithAmbiguityThreshold sets a custom ambiguity threshold.
func (ia *IntentAnalyzer) WithAmbiguityThreshold(threshold float64) *IntentAnalyzer {
	ia.ambiguityThreshold = threshold
	return ia
}

// AnalyzeTrueIntent performs a lightweight LLM-based analysis of the user's true intent.
func (ia *IntentAnalyzer) AnalyzeTrueIntent(ctx context.Context, input string) (*TrueIntentAnalysis, error) {
	if ia.client == nil {
		return nil, fmt.Errorf("intent analyzer: no client configured")
	}

	systemPrompt := `You are an intent analysis assistant. Analyze the user's input and return ONLY valid JSON with these exact fields:
- goal (string): What the user actually wants
- ambiguity (number 0.0-1.0): How ambiguous the request is (1.0 = very ambiguous)
- scope (string): One of "narrow", "medium", "broad"
- category (string): One of "research", "implementation", "investigation", "fix", "clarification", "other"
- suggested_questions (array of strings): If ambiguity >= 0.6, list clarifying questions to ask the user; otherwise empty array
- confidence (number 0.0-1.0): Your confidence in this analysis

Rules:
- scope must be exactly "narrow", "medium", or "broad"
- category must be exactly one of the allowed values
- Keep the response concise.`

	messages := []llm.ChatMessage{
		{Role: llm.RoleSystem, Content: systemPrompt},
		{Role: llm.RoleUser, Content: input},
	}

	resp, err := ia.client.Chat(ctx, messages,
		llm.WithMaxTokens(300),
		llm.WithTemperature(0.2),
	)
	if err != nil {
		ia.logger.Warn("intent analysis failed", "error", err)
		return nil, fmt.Errorf("intent analysis failed: %w", err)
	}

	if resp == nil || resp.Content == "" {
		return nil, fmt.Errorf("intent analysis: empty response from LLM")
	}

	return ia.parseAnalysis(resp.Content)
}

func (ia *IntentAnalyzer) parseAnalysis(content string) (*TrueIntentAnalysis, error) {
	jsonStr := extractJSONFromLLM(content)
	if jsonStr == "" {
		return nil, fmt.Errorf("intent analysis: no JSON found in response")
	}

	var analysis TrueIntentAnalysis
	if err := json.Unmarshal([]byte(jsonStr), &analysis); err != nil {
		return nil, fmt.Errorf("intent analysis: failed to parse JSON: %w", err)
	}

	// Normalize and validate
	analysis.Scope = strings.ToLower(strings.TrimSpace(analysis.Scope))
	analysis.Category = strings.ToLower(strings.TrimSpace(analysis.Category))

	validScopes := map[string]bool{"narrow": true, "medium": true, "broad": true}
	if !validScopes[analysis.Scope] {
		analysis.Scope = "medium"
	}

	validCategories := map[string]bool{
		"research": true, "implementation": true, "investigation": true,
		"fix": true, "clarification": true, "other": true,
	}
	if !validCategories[analysis.Category] {
		analysis.Category = "other"
	}

	analysis.Ambiguity = clampFloat(analysis.Ambiguity, 0.0, 1.0)
	analysis.Confidence = clampFloat(analysis.Confidence, 0.0, 1.0)

	return &analysis, nil
}

func clampFloat(v, min, max float64) float64 {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}
