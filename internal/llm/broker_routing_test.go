package llm

import (
	"testing"
)

func TestIsAnthropicRoute(t *testing.T) {
	tests := []struct {
		name     string
		cfg      *ModelConfig
		expected bool
	}{
		{"nil cfg", nil, false},
		{
			"direct anthropic",
			&ModelConfig{ProviderID: ProviderIDAnthropic, BaseURL: "https://api.anthropic.com"},
			true,
		},
		{
			"url contains anthropic",
			&ModelConfig{ProviderID: "custom", BaseURL: "https://my-anthropic-proxy.com/v1"},
			true,
		},
		{
			"bedrock claude",
			&ModelConfig{ProviderID: ProviderIDBedrock, BaseURL: "https://bedrock-runtime.us-east-1.amazonaws.com", ModelID: "anthropic.claude-sonnet-4-6"},
			true,
		},
		{
			"bedrock non-claude",
			&ModelConfig{ProviderID: ProviderIDBedrock, BaseURL: "https://bedlock-runtime.us-east-1.amazonaws.com", ModelID: "meta.llama3-70b"},
			false,
		},
		{
			"openrouter claude",
			&ModelConfig{ProviderID: "openrouter", BaseURL: "https://openrouter.ai/api/v1", ModelID: "anthropic/claude-sonnet-4-6"},
			true,
		},
		{
			"openrouter non-claude",
			&ModelConfig{ProviderID: "openrouter", BaseURL: "https://openrouter.ai/api/v1", ModelID: "openai/gpt-4o"},
			false,
		},
		{
			"openai provider",
			&ModelConfig{ProviderID: ProviderIDOpenAI, BaseURL: "https://api.openai.com/v1"},
			false,
		},
		{
			"zai provider",
			&ModelConfig{ProviderID: ProviderIDZAI, BaseURL: "https://open.bigmodel.cn/api/paas/v4"},
			false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isAnthropicRoute(tt.cfg); got != tt.expected {
				t.Errorf("isAnthropicRoute() = %v, want %v", got, tt.expected)
			}
		})
	}
}

// TestAnthropicRequestURL verifies URL construction across provider routes:
// direct Anthropic, OpenRouter-claude, Bedrock-claude (streaming + non-streaming),
// and custom providers. Covers spec §5 Phase 2-A Bedrock URL fix.
func TestAnthropicRequestURL(t *testing.T) {
	tests := []struct {
		name      string
		cfg       *ModelConfig
		streaming bool
		want      string
	}{
		{
			name:      "direct anthropic non-streaming",
			cfg:       &ModelConfig{ProviderID: ProviderIDAnthropic, BaseURL: "https://api.anthropic.com", ModelID: "claude-opus-4-7"},
			streaming: false,
			want:      "https://api.anthropic.com/v1/messages",
		},
		{
			name:      "direct anthropic streaming",
			cfg:       &ModelConfig{ProviderID: ProviderIDAnthropic, BaseURL: "https://api.anthropic.com", ModelID: "claude-opus-4-7"},
			streaming: true,
			want:      "https://api.anthropic.com/v1/messages",
		},
		{
			name:      "openrouter anthropic claude (non-streaming)",
			cfg:       &ModelConfig{ProviderID: "openrouter", BaseURL: "https://openrouter.ai/api/v1", ModelID: "anthropic/claude-sonnet-4-6"},
			streaming: false,
			want:      "https://openrouter.ai/api/v1/v1/messages",
		},
		{
			name:      "bedrock claude non-streaming",
			cfg:       &ModelConfig{ProviderID: ProviderIDBedrock, BaseURL: "https://bedrock-runtime.us-east-1.amazonaws.com", ModelID: "anthropic.claude-sonnet-4-6-v2:0"},
			streaming: false,
			want:      "https://bedrock-runtime.us-east-1.amazonaws.com/model/anthropic.claude-sonnet-4-6-v2:0/invoke",
		},
		{
			name:      "bedrock claude streaming",
			cfg:       &ModelConfig{ProviderID: ProviderIDBedrock, BaseURL: "https://bedrock-runtime.us-east-1.amazonaws.com", ModelID: "anthropic.claude-sonnet-4-6-v2:0"},
			streaming: true,
			want:      "https://bedrock-runtime.us-east-1.amazonaws.com/model/anthropic.claude-sonnet-4-6-v2:0/invoke_with_response_stream",
		},
		{
			name:      "custom provider (not bedrock) non-streaming",
			cfg:       &ModelConfig{ProviderID: "custom-proxy", BaseURL: "https://my-proxy.example.com", ModelID: "claude-fork"},
			streaming: false,
			want:      "https://my-proxy.example.com/v1/messages",
		},
		{
			name:      "trailing slash stripped from base url",
			cfg:       &ModelConfig{ProviderID: ProviderIDAnthropic, BaseURL: "https://api.anthropic.com/", ModelID: "claude-opus-4-7"},
			streaming: false,
			want:      "https://api.anthropic.com/v1/messages",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := NewAnthropicClient(tt.cfg)
			got := c.anthropicRequestURL(tt.streaming)
			if got != tt.want {
				t.Errorf("anthropicRequestURL(streaming=%v) = %q, want %q", tt.streaming, got, tt.want)
			}
		})
	}
}

