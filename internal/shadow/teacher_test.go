package shadow

import (
	"context"
	"encoding/json"
	"log/slog"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/caimlas/meept/internal/llm"
)

// teacherUsageResponse is an OpenAI-compatible chat completion body carrying
// explicit token usage, so tests can pin exact cost expectations.
type teacherUsageResponse struct {
	Choices []struct {
		Message struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

func TestTeacherClient_CostUsesRealTokenCounts(t *testing.T) {
	// Prices: $3/M input, $15/M output. The stub returns 5 prompt and 7
	// completion tokens, so the real cost is (5*3 + 7*15)/1e6 = 1.2e-4 USD.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := teacherUsageResponse{}
		resp.Choices = make([]struct {
			Message struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"message"`
		}, 1)
		resp.Choices[0].Message.Role = "assistant"
		resp.Choices[0].Message.Content = "hello"
		resp.Usage.PromptTokens = 5
		resp.Usage.CompletionTokens = 7
		resp.Usage.TotalTokens = 12
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			t.Errorf("encode response: %v", err)
		}
	}))
	defer srv.Close()

	cfg := &llm.ModelConfig{
		BaseURL:              srv.URL,
		ModelID:              "test-model",
		APIKey:               "test-key",
		CostPerMillionInput:  3.0,
		CostPerMillionOutput: 15.0,
	}

	teacherCfg := TeacherConfig{
		Model:             "test-model",
		Temperature:       0.0,
		MaxTokens:         256,
		TimeoutSeconds:    30,
		MaxDailyQueries:   0, // unlimited
		MaxDailyCost:      0, // unlimited
		RequestsPerMinute: 6000,
	}

	client := llm.NewClient(cfg)
	tc := NewTeacherClient(client, nil, &teacherCfg, WithTeacherLogger(slog.Default()))

	messages := []llm.ChatMessage{{Role: llm.RoleUser, Content: "hi"}}
	_, _, err := tc.GetResponse(context.Background(), messages)
	if err != nil {
		t.Fatalf("GetResponse failed: %v", err)
	}

	queries, cost := tc.GetUsageStats()
	if queries != 1 {
		t.Errorf("expected 1 recorded query, got %d", queries)
	}

	wantCost := (5*3.0 + 7*15.0) / 1_000_000.0
	if math.Abs(cost-wantCost) > 1e-12 {
		t.Errorf("cost = %v, want %v (token-based)", cost, wantCost)
	}
}

func TestTeacherClient_CalculateCost(t *testing.T) {
	tc := &TeacherClient{}
	cfg := &llm.ModelConfig{
		CostPerMillionInput:  3.0,
		CostPerMillionOutput: 15.0,
	}

	tests := []struct {
		name     string
		cfg      *llm.ModelConfig
		usage    llm.TokenUsage
		expected float64
	}{
		{
			name:     "real_token_counts",
			cfg:      cfg,
			usage:    llm.TokenUsage{PromptTokens: 1000, CompletionTokens: 500},
			expected: 1000*3.0/1_000_000 + 500*15.0/1_000_000,
		},
		{
			name:     "zero_tokens_free",
			cfg:      cfg,
			usage:    llm.TokenUsage{},
			expected: 0,
		},
		{
			name:     "nil_config_free",
			cfg:      nil,
			usage:    llm.TokenUsage{PromptTokens: 1000, CompletionTokens: 1000},
			expected: 0,
		},
		{
			name:     "zero_prices_free",
			cfg:      &llm.ModelConfig{},
			usage:    llm.TokenUsage{PromptTokens: 1000, CompletionTokens: 1000},
			expected: 0,
		},
		{
			name:     "large_counts_scale_per_million",
			cfg:      cfg,
			usage:    llm.TokenUsage{PromptTokens: 1_000_000, CompletionTokens: 1_000_000},
			expected: 3.0 + 15.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tc.calculateCost(tt.cfg, tt.usage)
			if math.Abs(got-tt.expected) > 1e-12 {
				t.Errorf("calculateCost() = %v, want %v", got, tt.expected)
			}
		})
	}
}
