package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/memory"
)

const (
	// defaultClassifierTimeout is used when LLMClassifierConfig.Timeout is zero.
	defaultClassifierTimeout = 5 * time.Second
)

var intentThresholds = map[string]float64{
	"git":      0.85,
	"schedule": 0.80,
	"code":     0.75,
	"debug":    0.75,
	"review":   0.75,
	"plan":     0.70,
	"platform": 0.70,
	"report":   0.70,
	"recall":   0.70,
	"analyze":  0.60,
	"search":   0.60,
	"chat":     0.50,
}

var agentMapping = map[string]string{
	"git":      "committer",
	"schedule": "scheduler",
	"code":     "coder",
	"debug":    "debugger",
	"review":   "coder",
	"plan":     "planner",
	"platform": "chat",
	"report":   "chat",
	"recall":   "chat",
	"analyze":  "analyst",
	"search":   "analyst",
	"chat":     "chat",
}

type LLMClassifier struct {
	client  *llm.Client
	model   string
	timeout time.Duration
	logger  Logger
}

type Logger interface {
	Debug(msg string, args ...any)
	Warn(msg string, args ...any)
	Error(msg string, args ...any)
	Info(msg string, args ...any)
}

type stdLogger struct{}

func (stdLogger) Debug(msg string, args ...any) {}
func (stdLogger) Warn(msg string, args ...any)  {}
func (stdLogger) Error(msg string, args ...any) {}
func (stdLogger) Info(msg string, args ...any)  {}

type LLMClassifierConfig struct {
	Client  *llm.Client
	Model   string
	Timeout time.Duration // When zero, defaultClassifierTimeout is used.
	Logger  Logger
}

func NewLLMClassifier(cfg LLMClassifierConfig) *LLMClassifier {
	logger := cfg.Logger
	if logger == nil {
		logger = stdLogger{}
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = defaultClassifierTimeout
	}
	return &LLMClassifier{
		client:  cfg.Client,
		model:   cfg.Model,
		timeout: timeout,
		logger:  logger,
	}
}

type classificationResponse struct {
	Intent     string  `json:"intent"`
	Confidence float64 `json:"confidence"`
	Reasoning  string  `json:"reasoning,omitempty"`
}

func (c *LLMClassifier) Classify(ctx context.Context, input string, memCtx *MemoryContext) (*Intent, error) {
	if c.client == nil {
		return nil, fmt.Errorf("LLM classifier: no client configured")
	}

	classificationPrompt := c.buildClassificationPrompt(input)
	messages := []llm.ChatMessage{
		{Role: llm.RoleSystem, Content: "You are an intent classifier for an AI agent system. Classify user inputs into one of these intents: git, schedule, code, debug, review, plan, platform, report, recall, analyze, search, chat. Return ONLY valid JSON with fields: intent (lowercase), confidence (0.0-1.0), and optional reasoning."},
		{Role: llm.RoleUser, Content: classificationPrompt},
	}

	timeoutCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	resp, err := c.client.Chat(timeoutCtx, messages,
		llm.WithMaxTokens(200),
		llm.WithTemperature(0.1),
	)
	if err != nil {
		c.logger.Debug("LLM classification failed", "error", err)
		return nil, err
	}

	if resp == nil || resp.Content == "" {
		return nil, fmt.Errorf("LLM classification: empty response")
	}

	return c.parseResponse(resp.Content, input)
}

// ClassifyMulti detects multiple intents in a single input.
func (c *LLMClassifier) ClassifyMulti(ctx context.Context, input string, ctxMemory []memory.MemoryResult) []*Intent {
	if c.client == nil {
		return nil
	}

	// Use a prompt that asks LLM to detect ALL intents
	prompt := fmt.Sprintf(`Analyze this user request and identify ALL distinct intents.

A request may contain multiple independent tasks joined by "and", "also", "then", "but", "while", etc.

For EACH detected intent, output:
- intent: one of [git, schedule, code, debug, review, plan, platform, report, recall, analyze, search, chat]
- confidence: 0.0-1.0
- summary: brief description

User input: %s

Return ONLY valid JSON array: [{"intent": "debug", "confidence": 0.8, "summary": "..."}]

If only one intent is present, return a single-element array.
If no intents detected, return empty array [].`, input)

	messages := []llm.ChatMessage{
		{Role: llm.RoleSystem, Content: "You are a multi-intent detector for an AI agent system. Identify ALL distinct intents in user requests."},
		{Role: llm.RoleUser, Content: prompt},
	}

	timeoutCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	resp, err := c.client.Chat(timeoutCtx, messages,
		llm.WithMaxTokens(500),
		llm.WithTemperature(0.1),
	)
	if err != nil {
		c.logger.Debug("LLM multi-intent classification failed", "error", err)
		return nil
	}

	if resp == nil || resp.Content == "" {
		return nil
	}

	// Parse the JSON array response
	jsonStr := extractJSONFromLLM(resp.Content)
	if jsonStr == "" || jsonStr[0] != '[' {
		return nil
	}

	var multiResp []struct {
		Intent     string  `json:"intent"`
		Confidence float64 `json:"confidence"`
		Summary    string  `json:"summary"`
	}
	if err := json.Unmarshal([]byte(jsonStr), &multiResp); err != nil {
		c.logger.Debug("Failed to parse multi-intent response", "error", err)
		return nil
	}

	var intents []*Intent
	for _, r := range multiResp {
		intent := strings.ToLower(strings.TrimSpace(r.Intent))
		if !isValidIntent(intent) {
			continue
		}
		agentType := agentMapping[intent]
		if agentType == "" {
			agentType = "chat"
		}
		requiresPlanning := intent == "plan"
		intents = append(intents, &Intent{
			Type:             intent,
			Confidence:       clampConfidence(r.Confidence),
			AgentType:        agentType,
			RequiresPlanning: requiresPlanning,
			Summary:          extractSummary(input),
		})
	}

	return intents
}

func (c *LLMClassifier) buildClassificationPrompt(input string) string {
	var sb strings.Builder
	sb.WriteString("Classify this user input:\n\n")
	sb.WriteString("Input: ")
	sb.WriteString(input)
	sb.WriteString("\n\n")
	sb.WriteString("Available intents:\n")
	intents := []string{"git", "schedule", "code", "debug", "review", "plan", "platform", "report", "recall", "analyze", "search", "chat"}
	for _, intent := range intents {
		sb.WriteString("- ")
		sb.WriteString(intent)
		sb.WriteString(": ")
		sb.WriteString(c.getIntentDescription(intent))
		sb.WriteString("\n")
	}
	sb.WriteString("\nReturn JSON with intent, confidence, and reasoning.")
	return sb.String()
}

func (c *LLMClassifier) getIntentDescription(intent string) string {
	descriptions := map[string]string{
		"git":      "Git operations (commit, push, pull, merge, branch)",
		"schedule": "Scheduling, reminders, timers, future tasks",
		"code":     "Code writing, implementation, refactoring",
		"debug":    "Bug fixing, debugging, error handling",
		"review":   "Code review, PR review",
		"plan":     "Planning, architecture, design",
		"platform": "Questions about agent capabilities, tools",
		"report":   "Status reports, summaries of work done",
		"recall":   "Memory recall, remembering past conversations",
		"analyze":  "Research, analysis, explanations",
		"search":   "Web search, finding information",
		"chat":     "General conversation, greetings, help",
	}
	if desc, ok := descriptions[intent]; ok {
		return desc
	}
	return "Unknown intent"
}

func (c *LLMClassifier) parseResponse(content string, originalInput string) (*Intent, error) {
	var resp classificationResponse

	cleanContent := strings.TrimSpace(content)

	if resp.Intent == "" {
		jsonStr := extractJSONFromLLM(cleanContent)
		if jsonStr != "" {
			if err := json.Unmarshal([]byte(jsonStr), &resp); err != nil {
				return nil, fmt.Errorf("failed to parse classification response: %w", err)
			}
		}
	}

	resp.Intent = strings.ToLower(strings.TrimSpace(resp.Intent))
	if resp.Intent == "" {
		return nil, fmt.Errorf("LLM classification: no intent returned")
	}

	if !isValidIntent(resp.Intent) {
		if c.logger != nil {
			c.logger.Debug("Invalid intent from LLM", "intent", resp.Intent)
		}
		return nil, fmt.Errorf("invalid intent: %s", resp.Intent)
	}

	agentType := agentMapping[resp.Intent]
	if agentType == "" {
		agentType = "chat"
	}

	requiresPlanning := resp.Intent == "plan"

	return &Intent{
		Type:             resp.Intent,
		Confidence:       clampConfidence(resp.Confidence),
		AgentType:        agentType,
		RequiresPlanning: requiresPlanning,
		Summary:          extractSummary(originalInput),
	}, nil
}

func extractJSONFromLLM(s string) string {
	start := strings.Index(s, "{")
	if start == -1 {
		start = strings.Index(s, "[")
		if start == -1 {
			return ""
		}
	}
	end := strings.LastIndex(s, "}")
	if end == -1 {
		end = strings.LastIndex(s, "]")
	}
	if end < start {
		return ""
	}
	return s[start : end+1]
}

func isValidIntent(intent string) bool {
	validIntents := map[string]bool{
		"git":      true,
		"schedule": true,
		"code":     true,
		"debug":    true,
		"review":   true,
		"plan":     true,
		"platform": true,
		"report":   true,
		"recall":   true,
		"analyze":  true,
		"search":   true,
		"chat":     true,
	}
	return validIntents[intent]
}

func clampConfidence(conf float64) float64 {
	if conf < 0 {
		return 0
	}
	if conf > 1 {
		return 1
	}
	return conf
}

func GetThresholdForIntent(intentType string) float64 {
	if threshold, ok := intentThresholds[intentType]; ok {
		return threshold
	}
	return 0.5
}

func ShouldUseLLMResult(intent *Intent) bool {
	if intent == nil {
		return false
	}
	threshold := GetThresholdForIntent(intent.Type)
	return intent.Confidence >= threshold
}
