package agent

import (
	"context"
	"errors"
	"testing"

	"github.com/caimlas/meept/internal/memory"
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
			errContains: "empty response",
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
			name:        "empty intent returns error",
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
				if !containsString(err.Error(), tt.errContains) {
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

func TestDispatcher_FallbackChain(t *testing.T) {
	t.Run("fallback to keyword when LLM returns low confidence", func(t *testing.T) {
		d := &Dispatcher{
			logger:            slog.Default(),
			keywordClassifier: &KeywordClassifier{},
			llmClassifier: &LLMClassifier{
				logger: stdLogger{},
			},
		}

		d.llmClassifier.Classify = func(ctx context.Context, input string, ctxMemory []memory.MemoryResult) (*Intent, error) {
			return &Intent{
				Type:       "search",
				Confidence: 0.4,
				AgentType:  "analyst",
			}, nil
		}

		ctx := context.Background()
		result, err := d.classifyIntent(ctx, "find something", nil)

		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		if result.AgentType != "analyst" {
			t.Errorf("Expected analyst agent (keyword fallback), got %q", result.AgentType)
		}
	})

	t.Run("uses LLM result when above threshold", func(t *testing.T) {
		d := &Dispatcher{
			logger:            slog.Default(),
			keywordClassifier: &KeywordClassifier{},
			llmClassifier: &LLMClassifier{
				logger: stdLogger{},
			},
		}

		d.llmClassifier.Classify = func(ctx context.Context, input string, ctxMemory []memory.MemoryResult) (*Intent, error) {
			return &Intent{
				Type:       "git",
				Confidence: 0.95,
				AgentType:  "committer",
			}, nil
		}

		ctx := context.Background()
		result, err := d.classifyIntent(ctx, "push to main", nil)

		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		if result.AgentType != "committer" {
			t.Errorf("Expected committer agent, got %q", result.AgentType)
		}
		if result.Confidence != 0.95 {
			t.Errorf("Expected confidence 0.95, got %v", result.Confidence)
		}
	})

	t.Run("fallback to chat when both classifiers fail", func(t *testing.T) {
		d := &Dispatcher{
			logger:            slog.Default(),
			keywordClassifier: &KeywordClassifier{},
			llmClassifier: &LLMClassifier{
				logger: stdLogger{},
			},
		}

		d.llmClassifier.Classify = func(ctx context.Context, input string, ctxMemory []memory.MemoryResult) (*Intent, error) {
			return nil, errors.New("LLM failed")
		}

		d.keywordClassifier.Classify = func(ctx context.Context, input string, ctxMemory []memory.MemoryResult) (*Intent, error) {
			return nil, errors.New("keyword failed")
		}

		ctx := context.Background()
		result, err := d.classifyIntent(ctx, "some random text", nil)

		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		if result.AgentType != "chat" {
			t.Errorf("Expected chat fallback, got %q", result.AgentType)
		}
		if result.Confidence != 0.3 {
			t.Errorf("Expected fallback confidence 0.3, got %v", result.Confidence)
		}
	})

	t.Run("keyword classifier only when no LLM classifier", func(t *testing.T) {
		d := &Dispatcher{
			logger:            slog.Default(),
			keywordClassifier: &KeywordClassifier{},
			llmClassifier:     nil,
		}

		ctx := context.Background()
		result, err := d.classifyIntent(ctx, "fix bug in login", nil)

		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		if result.Type != "debug" {
			t.Errorf("Expected debug intent from keyword, got %q", result.Type)
		}
	})
}

type mockClassifier struct {
	intent *Intent
	err    error
}

func (m *mockClassifier) Classify(ctx context.Context, input string, ctxMemory []memory.MemoryResult) (*Intent, error) {
	return m.intent, m.err
}

func containsString(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstring(s, substr))
}

func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
