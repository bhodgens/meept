package llm

import (
	"encoding/json"
	"testing"
)

func TestApplyOpenAICompatReasoning(t *testing.T) {
	tests := []struct {
		name         string
		providerID   string
		modelID      string
		capabilities map[string]bool
		rc           *ReasoningConfig
		assertBody   func(t *testing.T, body map[string]any)
	}{
		{
			name:         "openai high",
			providerID:   ProviderIDOpenAI,
			modelID:      "gpt-5.4",
			capabilities: map[string]bool{CapReasoning: true},
			rc:           &ReasoningConfig{Effort: "high"},
			assertBody: func(t *testing.T, body map[string]any) {
				if body["reasoning_effort"] != "high" {
					t.Errorf("reasoning_effort = %v, want high", body["reasoning_effort"])
				}
			},
		},
		{
			name:         "openai none sends nothing",
			providerID:   ProviderIDOpenAI,
			modelID:      "o4-mini",
			capabilities: map[string]bool{CapReasoning: true},
			rc:           &ReasoningConfig{Effort: "none"},
			assertBody: func(t *testing.T, body map[string]any) {
				if _, ok := body["reasoning_effort"]; ok {
					t.Error("reasoning_effort should not be set for none")
				}
			},
		},
		{
			name:         "xai high passthrough",
			providerID:   "xai",
			modelID:      "grok-4",
			capabilities: map[string]bool{CapReasoning: true},
			rc:           &ReasoningConfig{Effort: "high"},
			assertBody: func(t *testing.T, body map[string]any) {
				if body["reasoning_effort"] != "high" {
					t.Errorf("reasoning_effort = %v, want high", body["reasoning_effort"])
				}
			},
		},
		{
			name:         "xai xhigh clamped to high",
			providerID:   "xai",
			modelID:      "grok-4",
			capabilities: map[string]bool{CapReasoning: true},
			rc:           &ReasoningConfig{Effort: "xhigh"},
			assertBody: func(t *testing.T, body map[string]any) {
				if body["reasoning_effort"] != "high" {
					t.Errorf("reasoning_effort = %v, want high (clamped from xhigh)", body["reasoning_effort"])
				}
			},
		},
		{
			name:         "xai medium clamped to low",
			providerID:   "xai",
			modelID:      "grok-4",
			capabilities: map[string]bool{CapReasoning: true},
			rc:           &ReasoningConfig{Effort: "medium"},
			assertBody: func(t *testing.T, body map[string]any) {
				if body["reasoning_effort"] != "low" {
					t.Errorf("reasoning_effort = %v, want low (clamped from medium)", body["reasoning_effort"])
				}
			},
		},
		{
			name:         "google gemini reasoning_effort via extra_body",
			providerID:   ProviderIDGoogle,
			modelID:      "gemini-2.5-pro",
			capabilities: map[string]bool{CapReasoning: true},
			rc:           &ReasoningConfig{Effort: "medium"},
			assertBody: func(t *testing.T, body map[string]any) {
				extra, ok := body["extra_body"].(map[string]any)
				if !ok {
					t.Fatal("extra_body not set")
				}
				if extra["reasoning_effort"] != "medium" {
					t.Errorf("extra_body.reasoning_effort = %v, want medium", extra["reasoning_effort"])
				}
			},
		},
		{
			name:         "zai/glm thinking block",
			providerID:   ProviderIDZAI,
			modelID:      "glm-5.2",
			capabilities: map[string]bool{CapThinking: true},
			rc:           &ReasoningConfig{Effort: "high"},
			assertBody: func(t *testing.T, body map[string]any) {
				thinking, ok := body["thinking"].(map[string]any)
				if !ok {
					t.Fatal("thinking block not set")
				}
				if thinking["type"] != "enabled" {
					t.Errorf("thinking.type = %v, want enabled", thinking["type"])
				}
				if thinking["budget_tokens"] == nil {
					t.Error("thinking.budget_tokens should be set")
				}
			},
		},
		{
			name:         "qwen enable_thinking",
			providerID:   "qwen",
			modelID:      "qwen3-32b",
			capabilities: map[string]bool{CapThinking: true},
			rc:           &ReasoningConfig{Effort: "low"},
			assertBody: func(t *testing.T, body map[string]any) {
				if body["enable_thinking"] != true {
					t.Errorf("enable_thinking = %v, want true", body["enable_thinking"])
				}
				if body["thinking_budget"] == nil {
					t.Error("thinking_budget should be set")
				}
			},
		},
		{
			name:         "deepseek no request field",
			providerID:   ProviderIDDeepSeek,
			modelID:      "deepseek-chat",
			capabilities: map[string]bool{CapThinking: true},
			rc:           &ReasoningConfig{Effort: "high"},
			assertBody: func(t *testing.T, body map[string]any) {
				if _, ok := body["reasoning_effort"]; ok {
					t.Error("reasoning_effort should not be set for deepseek")
				}
			},
		},
		{
			name:         "no capability no send",
			providerID:   ProviderIDOpenAI,
			modelID:      "gpt-4o",
			capabilities: nil,
			rc:           &ReasoningConfig{Effort: "high"},
			assertBody: func(t *testing.T, body map[string]any) {
				if _, ok := body["reasoning_effort"]; ok {
					t.Error("reasoning_effort should not be set without capability")
				}
			},
		},
		{
			name:         "force bypasses capability",
			providerID:   ProviderIDOpenAI,
			modelID:      "gpt-4o",
			capabilities: nil,
			rc:           &ReasoningConfig{Effort: "high", Force: true},
			assertBody: func(t *testing.T, body map[string]any) {
				if body["reasoning_effort"] != "high" {
					t.Errorf("reasoning_effort = %v, want high (forced)", body["reasoning_effort"])
				}
			},
		},
		{
			name:         "zero config no-op",
			providerID:   ProviderIDOpenAI,
			modelID:      "gpt-5.4",
			capabilities: map[string]bool{CapReasoning: true},
			rc:           &ReasoningConfig{},
			assertBody: func(t *testing.T, body map[string]any) {
				if len(body) != 0 {
					t.Errorf("expected empty body, got %v", body)
				}
			},
		},
		// --- P2-B: vendor coverage gaps (spec §2 table) ---
		{
			// OpenRouter Claude routes through the Anthropic upstream, so
			// both the OpenRouter meta-field and the Anthropic-native
			// thinking block must be sent.
			name:         "openrouter claude dual-send reasoning + thinking",
			providerID:   "openrouter",
			modelID:      "anthropic/claude-sonnet-4-6",
			capabilities: map[string]bool{CapThinking: true},
			rc:           &ReasoningConfig{Effort: "high"},
			assertBody: func(t *testing.T, body map[string]any) {
				reasoning, ok := body["reasoning"].(map[string]any)
				if !ok {
					t.Fatalf("reasoning meta-field not set; body=%v", body)
				}
				if reasoning["effort"] != "high" {
					t.Errorf("reasoning.effort = %v, want high", reasoning["effort"])
				}
				thinking, ok := body["thinking"].(map[string]any)
				if !ok {
					t.Fatalf("thinking block not set; body=%v", body)
				}
				if thinking["type"] != "enabled" {
					t.Errorf("thinking.type = %v, want enabled", thinking["type"])
				}
				if thinking["budget_tokens"] == nil {
					t.Error("thinking.budget_tokens should be set for openrouter claude")
				}
			},
		},
		{
			// OpenRouter routing to a non-Anthropic upstream (e.g. OpenAI)
			// only gets the meta-field; no Anthropic thinking block.
			name:         "openrouter non-claude meta-field only",
			providerID:   "openrouter",
			modelID:      "openai/gpt-4o",
			capabilities: map[string]bool{CapReasoning: true},
			rc:           &ReasoningConfig{Effort: "high"},
			assertBody: func(t *testing.T, body map[string]any) {
				reasoning, ok := body["reasoning"].(map[string]any)
				if !ok {
					t.Fatalf("reasoning meta-field not set; body=%v", body)
				}
				if reasoning["effort"] != "high" {
					t.Errorf("reasoning.effort = %v, want high", reasoning["effort"])
				}
				if _, ok := body["thinking"]; ok {
					t.Error("thinking block should NOT be set for non-anthropic openrouter models")
				}
			},
		},
		{
			// Kimi/Moonshot uses the same Anthropic-style thinking block
			// as GLM (applyZAIStyleThinking).
			name:         "moonshot kimi thinking block",
			providerID:   "moonshot",
			modelID:      "kimi-k2.6",
			capabilities: map[string]bool{CapThinking: true},
			rc:           &ReasoningConfig{Effort: "medium"},
			assertBody: func(t *testing.T, body map[string]any) {
				thinking, ok := body["thinking"].(map[string]any)
				if !ok {
					t.Fatalf("thinking block not set; body=%v", body)
				}
				if thinking["type"] != "enabled" {
					t.Errorf("thinking.type = %v, want enabled", thinking["type"])
				}
				if thinking["budget_tokens"] == nil {
					t.Error("thinking.budget_tokens should be set for moonshot/kimi")
				}
			},
		},
		{
			// The "grok" providerID is distinct from "xai" but uses the
			// same clampXAIEffort clamping logic (xhigh→high).
			name:         "grok provider xhigh clamped to high",
			providerID:   "grok",
			modelID:      "grok-4",
			capabilities: map[string]bool{CapReasoning: true},
			rc:           &ReasoningConfig{Effort: "xhigh"},
			assertBody: func(t *testing.T, body map[string]any) {
				if body["reasoning_effort"] != "high" {
					t.Errorf("reasoning_effort = %v, want high (clamped from xhigh for grok)", body["reasoning_effort"])
				}
			},
		},
		{
			// DeepSeek does reasoning by default and exposes
			// `reasoning_content` in responses; no request-side field is
			// sent regardless of tier (including the "none" tier).
			name:         "deepseek none tier sends nothing",
			providerID:   ProviderIDDeepSeek,
			modelID:      "deepseek-chat",
			capabilities: map[string]bool{CapThinking: true},
			rc:           &ReasoningConfig{Effort: "none"},
			assertBody: func(t *testing.T, body map[string]any) {
				if _, ok := body["reasoning_effort"]; ok {
					t.Error("reasoning_effort should not be set for deepseek none tier")
				}
			},
		},
		{
			// OpenRouter "none" tier suppresses the reasoning meta-field,
			// but the Anthropic thinking block is still emitted with
			// type=disabled (applyZAIStyleThinking line 99-102).
			name:         "openrouter claude none tier disables thinking",
			providerID:   "openrouter",
			modelID:      "anthropic/claude-sonnet-4-6",
			capabilities: map[string]bool{CapThinking: true},
			rc:           &ReasoningConfig{Effort: "none"},
			assertBody: func(t *testing.T, body map[string]any) {
				if _, ok := body["reasoning"]; ok {
					t.Error("reasoning meta-field should NOT be set for none tier")
				}
				thinking, ok := body["thinking"].(map[string]any)
				if !ok {
					t.Fatalf("thinking block should be set with type=disabled for openrouter none; body=%v", body)
				}
				if thinking["type"] != "disabled" {
					t.Errorf("thinking.type = %v, want disabled", thinking["type"])
				}
			},
		},
		{
			// Qwen "none" tier sets enable_thinking=false via
			// ResolveEnabled; thinking_budget is not asserted (ResolveBudget
			// returns nil for "none" but the spec allows either).
			name:         "qwen none tier disables thinking",
			providerID:   "qwen",
			modelID:      "qwen3-32b",
			capabilities: map[string]bool{CapThinking: true},
			rc:           &ReasoningConfig{Effort: "none"},
			assertBody: func(t *testing.T, body map[string]any) {
				if body["enable_thinking"] != false {
					t.Errorf("enable_thinking = %v, want false", body["enable_thinking"])
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &ModelConfig{
				ProviderID:   tt.providerID,
				ModelID:      tt.modelID,
				Capabilities: tt.capabilities,
			}
			body := map[string]any{}
			applyOpenAICompatReasoning(body, cfg, tt.rc, nil)
			tt.assertBody(t, body)
		})
	}
}

func TestClampXAIEffort(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"", ""},
		{"none", "none"},
		{"low", "low"},
		{"medium", "low"},
		{"high", "high"},
		{"xhigh", "high"},
		{"max", "high"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := clampXAIEffort(tt.input); got != tt.want {
				t.Errorf("clampXAIEffort(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestShouldSendReasoning(t *testing.T) {
	t.Run("nil rc", func(t *testing.T) {
		cfg := &ModelConfig{Capabilities: map[string]bool{CapReasoning: true}}
		if shouldSendReasoning(cfg, nil) {
			t.Error("expected false for nil rc")
		}
	})
	t.Run("zero rc", func(t *testing.T) {
		cfg := &ModelConfig{Capabilities: map[string]bool{CapReasoning: true}}
		if shouldSendReasoning(cfg, &ReasoningConfig{}) {
			t.Error("expected false for zero rc")
		}
	})
	t.Run("cap reasoning", func(t *testing.T) {
		cfg := &ModelConfig{Capabilities: map[string]bool{CapReasoning: true}}
		if !shouldSendReasoning(cfg, &ReasoningConfig{Effort: "high"}) {
			t.Error("expected true for CapReasoning")
		}
	})
	t.Run("cap thinking", func(t *testing.T) {
		cfg := &ModelConfig{Capabilities: map[string]bool{CapThinking: true}}
		if !shouldSendReasoning(cfg, &ReasoningConfig{Effort: "high"}) {
			t.Error("expected true for CapThinking")
		}
	})
	t.Run("extended_thinking", func(t *testing.T) {
		cfg := &ModelConfig{Capabilities: map[string]bool{"extended_thinking": true}}
		if !shouldSendReasoning(cfg, &ReasoningConfig{Effort: "high"}) {
			t.Error("expected true for extended_thinking")
		}
	})
	t.Run("no capability no force", func(t *testing.T) {
		cfg := &ModelConfig{Capabilities: nil}
		if shouldSendReasoning(cfg, &ReasoningConfig{Effort: "high"}) {
			t.Error("expected false without capability or force")
		}
	})
	t.Run("force bypasses capability", func(t *testing.T) {
		cfg := &ModelConfig{Capabilities: nil}
		if !shouldSendReasoning(cfg, &ReasoningConfig{Effort: "high", Force: true}) {
			t.Error("expected true with force")
		}
	})
}

// TestParseResponseReasoningContent verifies that reasoning_content emitted
// by OpenAI-compat providers (DeepSeek, Qwen, GLM) is surfaced on
// Response.Reasoning via parseResponse (spec §6.3).
func TestParseResponseReasoningContent(t *testing.T) {
	raw := `{
		"id": "chatcmpl-1",
		"object": "chat.completion",
		"created": 1234567890,
		"model": "deepseek-chat",
		"choices": [{
			"index": 0,
			"message": {
				"role": "assistant",
				"content": "answer",
				"reasoning_content": "thinking..."
			},
			"finish_reason": "stop"
		}],
		"usage": {
			"prompt_tokens": 10,
			"completion_tokens": 5,
			"total_tokens": 15
		}
	}`
	var chatResp ChatResponse
	if err := json.Unmarshal([]byte(raw), &chatResp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	client := NewClient(&ModelConfig{
		BaseURL: "http://localhost",
		ModelID: "deepseek-chat",
	})
	resp, err := client.parseResponse(&chatResp)
	if err != nil {
		t.Fatalf("parseResponse: %v", err)
	}
	if resp.Reasoning != "thinking..." {
		t.Errorf("Response.Reasoning = %q, want %q", resp.Reasoning, "thinking...")
	}
	if resp.Content != "answer" {
		t.Errorf("Response.Content = %q, want %q", resp.Content, "answer")
	}
}
