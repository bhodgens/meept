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

	req, err := c.buildRequest(c.config, messages, &chatOptions{maxTokens: 1024}, false)
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

	// Last block (dynamic/session) SHOULD have cache_control: the trailing
	// Session breakpoint (tree 03 leaf L03) makes the growing session tail
	// cacheable across turns (2 of Anthropic's 4 breakpoints total).
	last := req.SystemBlocks[len(req.SystemBlocks)-1]
	if last.CacheControl == nil {
		t.Error("session block missing cache_control")
	} else if last.CacheControl.Type != "ephemeral" {
		t.Errorf("session block cache_control type = %q, want 'ephemeral'", last.CacheControl.Type)
	}

	// Boundary marker must not appear in any block text
	for i, b := range req.SystemBlocks {
		if strings.Contains(b.Text, PromptCacheBoundary) {
			t.Errorf("block %d contains boundary marker", i)
		}
	}
}

// TestCacheBlocksFromStructuredBuilder verifies the structured-block cache
// path (tree 03 leaf L03): a system prompt assembled by BuildSystemPromptBlocks
// whose STATIC section contains blank lines internally must produce exactly one
// Static cache_control block carrying the FULL static text (never re-split on
// blank lines) and one Session block, and BOTH blocks must carry
// cache_control (2 of Anthropic's 4 breakpoints).
func TestCacheBlocksFromStructuredBuilder(t *testing.T) {
	// Static section deliberately contains a blank line: the old
	// strings.Split(systemPrompt, "\n\n") classification re-partitioned it.
	staticText := "# Constitution\nBe helpful.\n\n# Capabilities\nCoding and debugging."
	sessionText := "Memory: user prefers Go.\n\nTask: fix bug #42."

	blocks := BuildSystemPromptBlocks([]string{
		staticText,
		PromptCacheBoundary,
		sessionText,
	})
	if len(blocks) != 2 {
		t.Fatalf("BuildSystemPromptBlocks produced %d blocks, want 2", len(blocks))
	}
	if blocks[0].CacheScope != CacheScopeStatic || blocks[0].Text != staticText {
		t.Fatalf("static block wrong: scope=%v text=%q", blocks[0].CacheScope, blocks[0].Text)
	}
	if blocks[1].CacheScope != CacheScopeSession || blocks[1].Text != sessionText {
		t.Fatalf("session block wrong: scope=%v text=%q", blocks[1].CacheScope, blocks[1].Text)
	}

	cfg := &ModelConfig{
		ProviderID:  "anthropic",
		ModelID:     "claude-test",
		BaseURL:     "https://api.anthropic.com",
		APIKey:      testAPIKey,
		MaxTokens:   1024,
		PromptCache: &PromptCacheConfig{Enabled: new(true)},
	}
	c := NewAnthropicClient(cfg)

	systemPrompt := staticText + "\n\n" + PromptCacheBoundary + "\n\n" + sessionText
	messages := []ChatMessage{
		{Role: RoleSystem, Content: systemPrompt},
		{Role: RoleUser, Content: "hello"},
	}

	req, err := c.buildRequest(c.config, messages, &chatOptions{
		maxTokens:    1024,
		systemBlocks: blocks,
	}, false)
	if err != nil {
		t.Fatalf("buildRequest failed: %v", err)
	}

	if len(req.SystemBlocks) != 2 {
		t.Fatalf("expected exactly 2 system blocks, got %d: %+v", len(req.SystemBlocks), req.SystemBlocks)
	}

	staticBlock := req.SystemBlocks[0]
	if staticBlock.CacheControl == nil {
		t.Error("static block missing cache_control")
	} else if staticBlock.CacheControl.Type != "ephemeral" {
		t.Errorf("static block cache_control type = %q, want 'ephemeral'", staticBlock.CacheControl.Type)
	}
	if staticBlock.Text != staticText {
		t.Errorf("static block was re-split: text = %q, want full static text %q", staticBlock.Text, staticText)
	}

	sessionBlock := req.SystemBlocks[1]
	if sessionBlock.CacheControl == nil {
		t.Error("session block missing cache_control (trailing Session breakpoint)")
	} else if sessionBlock.CacheControl.Type != "ephemeral" {
		t.Errorf("session block cache_control type = %q, want 'ephemeral'", sessionBlock.CacheControl.Type)
	}
	if sessionBlock.Text != sessionText {
		t.Errorf("session block text = %q, want %q", sessionBlock.Text, sessionText)
	}

	// Wire shape: the system array must marshal with cache_control on both
	// blocks and must never leak the boundary marker.
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	if strings.Contains(string(data), PromptCacheBoundary) {
		t.Error("boundary marker found in serialized API request")
	}
	var wire struct {
		System []struct {
			Text         string `json:"text"`
			CacheControl *struct {
				Type string `json:"type"`
			} `json:"cache_control"`
		} `json:"system"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if len(wire.System) != 2 {
		t.Fatalf("wire system array has %d blocks, want 2", len(wire.System))
	}
	for i, b := range wire.System {
		if b.CacheControl == nil {
			t.Errorf("wire block %d (%q) missing cache_control", i, b.Text)
		}
	}
}

// TestCacheBlocksFallbackStringPath verifies the fallback for callers that
// never build structured blocks: a flat boundary-carrying system prompt whose
// static half contains blank lines is split ONLY on the boundary sentinel,
// never on blank lines, so the static block keeps its internal blank lines and
// both blocks carry cache_control.
func TestCacheBlocksFallbackStringPath(t *testing.T) {
	staticText := "# Constitution\nBe helpful.\n\n# Capabilities\nCoding and debugging."
	sessionText := "Memory: user prefers Go.\n\nTask: fix bug #42."

	cfg := &ModelConfig{
		ProviderID:  "anthropic",
		ModelID:     "claude-test",
		BaseURL:     "https://api.anthropic.com",
		APIKey:      testAPIKey,
		MaxTokens:   1024,
		PromptCache: &PromptCacheConfig{Enabled: new(true)},
	}
	c := NewAnthropicClient(cfg)

	systemPrompt := staticText + "\n\n" + PromptCacheBoundary + "\n\n" + sessionText
	messages := []ChatMessage{
		{Role: RoleSystem, Content: systemPrompt},
		{Role: RoleUser, Content: "hello"},
	}

	req, err := c.buildRequest(c.config, messages, &chatOptions{maxTokens: 1024}, false)
	if err != nil {
		t.Fatalf("buildRequest failed: %v", err)
	}

	if len(req.SystemBlocks) != 2 {
		t.Fatalf("expected exactly 2 system blocks, got %d: %+v", len(req.SystemBlocks), req.SystemBlocks)
	}
	if req.SystemBlocks[0].Text != staticText {
		t.Errorf("static block re-split on blank lines: got %q, want %q", req.SystemBlocks[0].Text, staticText)
	}
	if req.SystemBlocks[0].CacheControl == nil || req.SystemBlocks[1].CacheControl == nil {
		t.Error("expected cache_control on both static and session blocks")
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

	req, err := c.buildRequest(c.config, messages, &chatOptions{maxTokens: 1024}, false)
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

			req, err := c.buildRequest(c.config, messages, &chatOptions{maxTokens: 1024}, false)
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
