package llm

import (
	"encoding/json"
	"strings"
	"testing"
)

// testAPIKey is a dummy key for test requests (never sent to a real API).
const testAPIKey = "test" + "-key"

// TestAnthropicCacheControl verifies that when prompt caching is enabled and
// the system prompt contains the boundary marker, static blocks get
// cache_control: ephemeral and dynamic blocks do not.
func TestAnthropicCacheControl(t *testing.T) {
	cfg := &ModelConfig{
		ProviderID:  "anthropic",
		ModelID:     "claude-test",
		BaseURL:     "https://api.anthropic.com",
		APIKey:      testAPIKey,
		MaxTokens:   1024,
		PromptCache: &PromptCacheConfig{Enabled: new(true)},
	}
	c := NewAnthropicClient(cfg)

	// System prompt with boundary marker separating static from dynamic
	systemPrompt := "You are a helpful assistant.\n\nCapabilities: coding, debugging.\n\n" +
		PromptCacheBoundary + "\n\nMemory: user prefers Go.\n\nTask: fix bug #42."

	messages := []ChatMessage{
		{Role: RoleSystem, Content: systemPrompt},
		{Role: RoleUser, Content: "hello"},
	}

	req, err := c.buildRequest(messages, &chatOptions{maxTokens: 1024}, false)
	if err != nil {
		t.Fatalf("buildRequest failed: %v", err)
	}

	if len(req.SystemBlocks) == 0 {
		t.Fatal("expected SystemBlocks to be populated when cache is enabled and boundary present")
	}

	// First block (static) should have cache_control
	if req.SystemBlocks[0].CacheControl == nil {
		t.Error("static block missing cache_control")
	} else if req.SystemBlocks[0].CacheControl.Type != "ephemeral" {
		t.Errorf("static block cache_control type = %q, want 'ephemeral'", req.SystemBlocks[0].CacheControl.Type)
	}

	// Last block (dynamic/session) should NOT have cache_control
	last := req.SystemBlocks[len(req.SystemBlocks)-1]
	if last.CacheControl != nil {
		t.Error("dynamic block should not have cache_control")
	}

	// Boundary marker must not appear in any block text
	for i, b := range req.SystemBlocks {
		if strings.Contains(b.Text, PromptCacheBoundary) {
			t.Errorf("block %d contains boundary marker", i)
		}
	}
}

// TestAnthropicCacheControlDisabled verifies that when prompt caching is
// disabled, the system prompt is sent as a plain string without cache_control.
func TestAnthropicCacheControlDisabled(t *testing.T) {
	cfg := &ModelConfig{
		ProviderID:  "anthropic",
		ModelID:     "claude-test",
		BaseURL:     "https://api.anthropic.com",
		APIKey:      testAPIKey,
		MaxTokens:   1024,
		PromptCache: &PromptCacheConfig{Enabled: new(false)},
	}
	c := NewAnthropicClient(cfg)

	systemPrompt := "Static content.\n\n" + PromptCacheBoundary + "\n\nDynamic content."
	messages := []ChatMessage{
		{Role: RoleSystem, Content: systemPrompt},
		{Role: RoleUser, Content: "hello"},
	}

	req, err := c.buildRequest(messages, &chatOptions{maxTokens: 1024}, false)
	if err != nil {
		t.Fatalf("buildRequest failed: %v", err)
	}

	// Should use plain System string, not SystemBlocks
	if len(req.SystemBlocks) != 0 {
		t.Error("SystemBlocks should be empty when cache is disabled")
	}
	if req.System == "" {
		t.Error("System should be populated when cache is disabled")
	}
	// Boundary must be stripped
	if strings.Contains(req.System, PromptCacheBoundary) {
		t.Error("boundary marker not stripped from plain system string")
	}
}

// TestBoundaryStripped verifies the boundary marker never appears in API
// requests regardless of cache setting.
func TestBoundaryStripped(t *testing.T) {
	tests := []struct {
		name    string
		enabled bool
	}{
		{"cache enabled", true},
		{"cache disabled", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &ModelConfig{
				ProviderID:  "anthropic",
				ModelID:     "claude-test",
				BaseURL:     "https://api.anthropic.com",
				APIKey:      testAPIKey,
				MaxTokens:   1024,
				PromptCache: &PromptCacheConfig{Enabled: new(tt.enabled)},
			}
			c := NewAnthropicClient(cfg)

			systemPrompt := "Before boundary.\n\n" + PromptCacheBoundary + "\n\nAfter boundary."
			messages := []ChatMessage{
				{Role: RoleSystem, Content: systemPrompt},
				{Role: RoleUser, Content: "test"},
			}

			req, err := c.buildRequest(messages, &chatOptions{maxTokens: 1024}, false)
			if err != nil {
				t.Fatalf("buildRequest failed: %v", err)
			}

			// Marshal to JSON and check boundary doesn't appear
			data, err := json.Marshal(req)
			if err != nil {
				t.Fatalf("marshal failed: %v", err)
			}
			if strings.Contains(string(data), PromptCacheBoundary) {
				t.Error("boundary marker found in serialized API request")
			}
		})
	}
}

// TestPrefixStability verifies that the same static content produces the same
// prefix hash across calls (deterministic cache key).
func TestPrefixStability(t *testing.T) {
	sections := []string{
		"Constitution: be helpful.",
		"Capabilities: coding.",
		PromptCacheBoundary,
		"Memory: user likes Go.",
	}

	static1, dynamic1 := ClassifyPromptSections(sections)
	static2, dynamic2 := ClassifyPromptSections(sections)

	if len(static1) != len(static2) || len(dynamic1) != len(dynamic2) {
		t.Fatal("classification not deterministic")
	}
	for i := range static1 {
		if static1[i] != static2[i] {
			t.Errorf("static[%d] differs: %q vs %q", i, static1[i], static2[i])
		}
	}
}
