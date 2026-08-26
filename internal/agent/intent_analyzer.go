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
	SuggestedMode      string   `json:"suggested_mode,omitempty"`
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

	// tokenCap is the per-call output token budget, derived from the
	// model's declared max_output (see effectiveClassificationCap).
	tokenCap int

	// resolver enables alias-based failover (leaf 03 of
	// classifier-reliability). When non-nil (with aliasName set), a failed
	// Chat attempt records an alias failure, rotates to the next candidate,
	// reconfigures the client, and retries once.
	resolver  *llm.Resolver // nil = no failover (legacy behavior)
	aliasName string        // e.g. "classifier"; required when resolver != nil
}

// NewIntentAnalyzer creates a new IntentAnalyzer with the given LLM client and logger.
func NewIntentAnalyzer(client *llm.Client, logger *slog.Logger) *IntentAnalyzer {
	return newIntentAnalyzer(client, 0, nil, logger)
}

// IntentAnalyzerConfig carries optional construction parameters for
// newIntentAnalyzer. All fields are additive; zero values keep prior defaults.
type IntentAnalyzerConfig struct {
	// ModelConfig is the resolved model configuration for the analyzer's
	// endpoint. When non-nil, the per-call token cap is derived from its
	// declared max_output (see effectiveClassificationCap).
	ModelConfig *llm.ModelConfig
	// Resolver enables alias failover (leaf 03 of classifier-reliability).
	// When non-nil, a failed Chat attempt records an alias failure, rotates
	// to the next candidate via ResolveForAlias(aliasName), swaps the client
	// config, and retries once. Nil = no failover (unchanged behavior).
	Resolver *llm.Resolver
	// AliasName is the resolver alias used for failover (e.g. "classifier").
	// Required when Resolver != nil; ignored otherwise.
	AliasName string
}

// newIntentAnalyzer is the shared constructor; tokenCapOverride of 0 means
// derive from modelCfg.
func newIntentAnalyzer(client *llm.Client, _ int, modelCfg *llm.ModelConfig, logger *slog.Logger) *IntentAnalyzer {
	return newIntentAnalyzerWithConfig(IntentAnalyzerConfig{ModelConfig: modelCfg}, client, logger)
}

// newIntentAnalyzerWithConfig is the fully-parameterized constructor used by
// the dispatcher to wire resolver-based alias failover.
func newIntentAnalyzerWithConfig(cfg IntentAnalyzerConfig, client *llm.Client, logger *slog.Logger) *IntentAnalyzer {
	if logger == nil {
		logger = slog.Default()
	}
	return &IntentAnalyzer{
		client:             client,
		ambiguityThreshold: defaultAmbiguityThreshold,
		logger:             logger,
		tokenCap:           effectiveClassificationCap(cfg.ModelConfig),
		resolver:           cfg.Resolver,
		aliasName:          cfg.AliasName,
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
- suggested_mode (string): One of "direct", "plan", "spec_plan", "spec_pair"
  - "direct" for trivial/lookup questions
  - "plan" for single-component work
  - "spec_plan" for multi-file or multi-phase work
  - "spec_pair" for compound requests

Rules:
- scope must be exactly "narrow", "medium", or "broad"
- category must be exactly one of the allowed values
- suggested_mode must be exactly one of the allowed values
- Keep the response concise.`

	messages := []llm.ChatMessage{
		{Role: llm.RoleSystem, Content: systemPrompt},
		{Role: llm.RoleUser, Content: input},
	}

	resp, err := ia.chatWithFailover(ctx, messages,
		llm.WithMaxTokens(ia.tokenCap),
		llm.WithTemperature(0.2),
		noThinkingOpt(),
	)
	if err != nil {
		ia.logger.Warn("intent analysis failed", "error", err)
		return nil, fmt.Errorf("intent analysis failed: %w", err)
	}

	return ia.parseAnalysis(resp.Content)
}

// chatWithFailover performs at most two Chat attempts against the underlying
// client: the initial attempt plus, when resolver-based alias failover is
// configured and the first attempt fails (including empty responses), one
// rotation to the next alias candidate. On success it records AliasSuccess so
// resolver health resets. Max 2 total attempts — no loops.
func (ia *IntentAnalyzer) chatWithFailover(ctx context.Context, messages []llm.ChatMessage, opts ...llm.ChatOption) (*llm.Response, error) {
	attempt := func() (*llm.Response, error) {
		resp, err := ia.client.Chat(ctx, messages, opts...)
		if err != nil {
			return nil, err
		}
		if resp == nil || resp.Content == "" {
			return nil, fmt.Errorf("intent analysis: %w", llm.ErrEmptyResponse)
		}
		return resp, nil
	}

	resp, err := attempt()
	if err == nil {
		if ia.resolver != nil && ia.aliasName != "" {
			ia.resolver.RecordAliasSuccess(ia.aliasName)
		}
		return resp, nil
	}
	if ia.resolver == nil || ia.aliasName == "" {
		return nil, err
	}

	// Fail over: record the failure, advance to the next candidate, swap the
	// client config, and retry once.
	ia.resolver.RecordAliasFailure(ia.aliasName, err)
	nextCfg, rerr := ia.resolver.ResolveForAlias(ia.aliasName)
	if rerr != nil || nextCfg == nil {
		return nil, fmt.Errorf("intent analysis: %w (no alternate candidate: %v)", err, rerr)
	}
	ia.logger.Warn("Intent analyzer rotating to next alias candidate",
		"alias", ia.aliasName,
		"model", nextCfg.ProviderID+"/"+nextCfg.ModelID,
		"error", err,
	)
	ia.client.Reconfigure(nextCfg)

	resp, err = attempt()
	if err == nil {
		ia.resolver.RecordAliasSuccess(ia.aliasName)
	}
	return resp, err
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

	// Validate suggested_mode (Thread D complexity routing). Invalid/empty
	// values are zeroed — the rule-based fallback in suggestMode handles it.
	analysis.SuggestedMode = validateMode(strings.ToLower(strings.TrimSpace(analysis.SuggestedMode)))

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
