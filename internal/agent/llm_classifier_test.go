package agent

import (
	"context"
	"log/slog"
	"strings"
	"testing"
)

func TestGetThresholdForIntent(t *testing.T) {
	tests := []struct {
		name       string
		intentType string
		want       float64
	}{
		{"git threshold", "git", 0.85},
		{"schedule threshold", "schedule", 0.80},
		{"code threshold", "code", 0.75},
		{"debug threshold", "debug", 0.75},
		{"review threshold", "review", 0.75},
		{"plan threshold", "plan", 0.70},
		{"platform threshold", "platform", 0.70},
		{"report threshold", "report", 0.70},
		{"recall threshold", "recall", 0.70},
		{"analyze threshold", "analyze", 0.60},
		{"search threshold", "search", 0.60},
		{"chat threshold", "chat", 0.50},
		{"unknown intent defaults to 0.5", "unknown_intent", 0.5},
		{"empty intent defaults to 0.5", "", 0.5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GetThresholdForIntent(tt.intentType)
			if got != tt.want {
				t.Errorf("GetThresholdForIntent(%q) = %v, want %v", tt.intentType, got, tt.want)
			}
		})
	}
}

func TestShouldUseLLMResult(t *testing.T) {
	tests := []struct {
		name   string
		intent *Intent
		want   bool
	}{
		{
			name:   "nil intent returns false",
			intent: nil,
			want:   false,
		},
		{
			name:   "git intent at 0.9 confidence passes 0.85 threshold",
			intent: &Intent{Type: "git", Confidence: 0.9},
			want:   true,
		},
		{
			name:   "git intent at 0.8 confidence fails 0.85 threshold",
			intent: &Intent{Type: "git", Confidence: 0.8},
			want:   false,
		},
		{
			name:   "chat intent at 0.6 confidence passes 0.50 threshold",
			intent: &Intent{Type: "chat", Confidence: 0.6},
			want:   true,
		},
		{
			name:   "chat intent at 0.4 confidence fails 0.50 threshold",
			intent: &Intent{Type: "chat", Confidence: 0.4},
			want:   false,
		},
		{
			name:   "code intent at 0.75 confidence exactly at threshold",
			intent: &Intent{Type: "code", Confidence: 0.75},
			want:   true,
		},
		{
			name:   "analyze intent at 0.55 confidence passes 0.60 threshold",
			intent: &Intent{Type: "analyze", Confidence: 0.55},
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ShouldUseLLMResult(tt.intent)
			if got != tt.want {
				t.Errorf("ShouldUseLLMResult() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLLMClassifier_NoClient(t *testing.T) {
	c := NewLLMClassifier(LLMClassifierConfig{
		Client: nil,
		Model:  "",
		Logger: nil,
	})

	ctx := context.Background()
	intent, err := c.Classify(ctx, "test input", nil)

	if err == nil {
		t.Error("Expected error when client is nil, got nil")
	}
	if intent != nil {
		t.Error("Expected nil intent when client is nil")
	}
}

func TestLLMClassifier_ParseInvalidIntent(t *testing.T) {
	c := &LLMClassifier{}

	tests := []struct {
		name        string
		content     string
		input       string
		wantErr     bool
		errContains string
	}{
		{
			name:        "empty response returns error",
			content:     "",
			input:       "test",
			wantErr:     true,
			errContains: "no intent",
		},
		{
			name:        "invalid JSON returns error",
			content:     "{ invalid json }",
			input:       "test",
			wantErr:     true,
			errContains: "parse",
		},
		{
			name:        "invalid intent returns error",
			content:     `{"intent": "invalid_intent", "confidence": 0.9}`,
			input:       "test",
			wantErr:     true,
			errContains: "invalid intent",
		},
		{
			name:        "empty intent in JSON returns error",
			content:     `{"intent": "", "confidence": 0.9}`,
			input:       "test",
			wantErr:     true,
			errContains: "no intent",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			intent, err := c.parseResponse(tt.content, tt.input)

			if tt.wantErr {
				if err == nil {
					t.Errorf("Expected error containing %q, got nil", tt.errContains)
					return
				}
				if !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("Error %q does not contain %q", err.Error(), tt.errContains)
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
				if intent == nil {
					t.Error("Expected intent, got nil")
				}
			}
		})
	}
}

func TestLLMClassifier_ValidIntentParsing(t *testing.T) {
	c := &LLMClassifier{}

	tests := []struct {
		name    string
		content string
		input   string
		want    *Intent
	}{
		{
			name:    "git intent parsed correctly",
			content: `{"intent": "git", "confidence": 0.9, "reasoning": "user mentioned push"}`,
			input:   "push to main",
			want: &Intent{
				Type:             "git",
				Confidence:       0.9,
				AgentType:        "committer",
				RequiresPlanning: false,
			},
		},
		{
			name:    "code intent parsed correctly",
			content: `{"intent":"code","confidence":0.85}`,
			input:   "write a function",
			want: &Intent{
				Type:             "code",
				Confidence:       0.85,
				AgentType:        "coder",
				RequiresPlanning: false,
			},
		},
		{
			name:    "plan intent with planning flag",
			content: `{"intent":"plan","confidence":0.8}`,
			input:   "design a system",
			want: &Intent{
				Type:             "plan",
				Confidence:       0.8,
				AgentType:        "planner",
				RequiresPlanning: true,
			},
		},
		{
			name:    "chat intent",
			content: `{"intent":"chat","confidence":0.7}`,
			input:   "hello there",
			want: &Intent{
				Type:             "chat",
				Confidence:       0.7,
				AgentType:        "chat",
				RequiresPlanning: false,
			},
		},
		{
			name:    "confidence clamped to 1.0",
			content: `{"intent":"chat","confidence":1.5}`,
			input:   "hi",
			want: &Intent{
				Type:             "chat",
				Confidence:       1.0,
				AgentType:        "chat",
				RequiresPlanning: false,
			},
		},
		{
			name:    "confidence clamped to 0.0",
			content: `{"intent":"chat","confidence":-0.5}`,
			input:   "hi",
			want: &Intent{
				Type:             "chat",
				Confidence:       0.0,
				AgentType:        "chat",
				RequiresPlanning: false,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			intent, err := c.parseResponse(tt.content, tt.input)

			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}
			if intent == nil {
				t.Fatal("Expected intent, got nil")
			}

			if intent.Type != tt.want.Type {
				t.Errorf("Type = %q, want %q", intent.Type, tt.want.Type)
			}
			if intent.Confidence != tt.want.Confidence {
				t.Errorf("Confidence = %v, want %v", intent.Confidence, tt.want.Confidence)
			}
			if intent.AgentType != tt.want.AgentType {
				t.Errorf("AgentType = %q, want %q", intent.AgentType, tt.want.AgentType)
			}
			if intent.RequiresPlanning != tt.want.RequiresPlanning {
				t.Errorf("RequiresPlanning = %v, want %v", intent.RequiresPlanning, tt.want.RequiresPlanning)
			}
		})
	}
}

func TestExtractJSONFromLLM(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "extracts JSON from markdown code block",
			input: "```json\n{\"intent\": \"chat\", \"confidence\": 0.9}\n```",
			want:  `{"intent": "chat", "confidence": 0.9}`,
		},
		{
			name:  "extracts JSON from plain text",
			input: "Here is the result: {\"intent\": \"code\"} for you.",
			want:  `{"intent": "code"}`,
		},
		{
			name:  "returns empty for no JSON",
			input: "No JSON here",
			want:  "",
		},
		{
			name:  "handles nested braces",
			input: "Result: {\"data\": {\"nested\": true}}",
			want:  `{"data": {"nested": true}}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractJSONFromLLM(tt.input)
			if got != tt.want {
				t.Errorf("extractJSONFromLLM(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestIsValidIntent(t *testing.T) {
	tests := []struct {
		name   string
		intent string
		want   bool
	}{
		{"git is valid", "git", true},
		{"code is valid", "code", true},
		{"debug is valid", "debug", true},
		{"chat is valid", "chat", true},
		{"plan is valid", "plan", true},
		{"invalid intent", "invalid", false},
		{"empty intent", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isValidIntent(tt.intent)
			if got != tt.want {
				t.Errorf("isValidIntent(%q) = %v, want %v", tt.intent, got, tt.want)
			}
		})
	}
}

// TestDispatcher_FallbackChain exercises the classifyIntent fallback ladder:
// when no capability matcher and no LLM classifier are wired, a matching input
// is resolved by the keyword classifier; a non-matching input falls through to
// the final Chat fallback with confidence 0.3.
//
// Note: the Dispatcher stores llmClassifier as a concrete *LLMClassifier, so
// the full LLM path cannot be mocked without a refactor. The keyword → chat
// slice of the chain is what we can reliably cover here.
func TestDispatcher_FallbackChain(t *testing.T) {
	d := &Dispatcher{
		logger:            slog.Default(),
		keywordClassifier: &KeywordClassifier{},
	}

	tests := []struct {
		name           string
		input          string
		wantType       string
		wantAgent      string
		wantConfidence float64
		wantExactConf  bool
	}{
		{
			name:      "keyword classifier matches git",
			input:     "please commit these changes",
			wantType:  "git",
			wantAgent: "committer",
		},
		{
			name:      "keyword classifier matches debug",
			input:     "there is a bug, please debug it",
			wantType:  "debug",
			wantAgent: "debugger",
		},
		{
			name:           "no classifier matches falls through to chat",
			input:          "xyzzy fnord quux",
			wantType:       "chat",
			wantAgent:      "chat",
			wantConfidence: 0.3,
			wantExactConf:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			intent, err := d.classifyIntent(context.Background(), tt.input, nil)
			if err != nil {
				t.Fatalf("classifyIntent returned error: %v", err)
			}
			if intent == nil {
				t.Fatalf("classifyIntent returned nil intent")
			}
			if intent.Type != tt.wantType {
				t.Errorf("Type = %q, want %q", intent.Type, tt.wantType)
			}
			if intent.AgentType != tt.wantAgent {
				t.Errorf("AgentType = %q, want %q", intent.AgentType, tt.wantAgent)
			}
			if tt.wantExactConf && intent.Confidence != tt.wantConfidence {
				t.Errorf("Confidence = %v, want exactly %v", intent.Confidence, tt.wantConfidence)
			}
		})
	}
}
