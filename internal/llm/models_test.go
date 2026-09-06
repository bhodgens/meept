package llm

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTokenUsage_CacheFields(t *testing.T) {
	u := TokenUsage{
		PromptTokens:     1000,
		CompletionTokens: 200,
		TotalTokens:      1200,
		CachedTokens:     800,
	}
	if u.CachedTokens != 800 {
		t.Errorf("CachedTokens = %d, want 800", u.CachedTokens)
	}
}

// TestTokenUsage_NewFieldsJSON verifies the tokscale-ingest ReasoningTokens
// and CacheCreationTokens fields round-trip through JSON and serialize with
// the contracted keys. Zero values omit (omitempty) — semantics: 0 means
// "provider did not report", not "zero occurred".
func TestTokenUsage_NewFieldsJSON(t *testing.T) {
	u := TokenUsage{
		PromptTokens:        10,
		CompletionTokens:    5,
		TotalTokens:         15,
		ReasoningTokens:     3,
		CacheCreationTokens: 7,
	}
	b, err := json.Marshal(u)
	if err != nil {
		t.Fatal(err)
	}
	var back TokenUsage
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	if back.ReasoningTokens != 3 {
		t.Fatalf("ReasoningTokens = %d, want 3", back.ReasoningTokens)
	}
	if back.CacheCreationTokens != 7 {
		t.Fatalf("CacheCreationTokens = %d, want 7", back.CacheCreationTokens)
	}
	// Zero fields omit from JSON (omitempty) — semantic: not reported.
	if !strings.Contains(string(b), `"reasoning_tokens":3`) ||
		!strings.Contains(string(b), `"cache_creation_tokens":7`) {
		t.Fatalf("expected new keys in %s", b)
	}
}

func TestChatResponseUsage_PromptTokensDetails(t *testing.T) {
	resp := ChatResponse{}
	resp.Usage.PromptTokens = 1000
	resp.Usage.CompletionTokens = 200
	resp.Usage.TotalTokens = 1200
	resp.Usage.PromptTokensDetails.CachedTokens = 800

	if resp.Usage.PromptTokensDetails.CachedTokens != 800 {
		t.Errorf("PromptTokensDetails.CachedTokens = %d, want 800", resp.Usage.PromptTokensDetails.CachedTokens)
	}
}

func TestChatMessage_IsToolError(t *testing.T) {
	t.Run("defaults to false", func(t *testing.T) {
		msg := ChatMessage{Role: RoleTool, Content: "ok"}
		if msg.IsToolError {
			t.Error("expected IsToolError to default to false")
		}
	})

	t.Run("can be set true", func(t *testing.T) {
		msg := ChatMessage{Role: RoleTool, Content: "fail", IsToolError: true}
		if !msg.IsToolError {
			t.Error("expected IsToolError to be true")
		}
	})

	t.Run("not serialized", func(t *testing.T) {
		// IsToolError has json:"-" so it must not appear in serialized output.
		msg := ChatMessage{Role: RoleTool, Content: "fail", IsToolError: true}
		dict := msg.ToOpenAIDict()
		if _, ok := dict["is_tool_error"]; ok {
			t.Error("IsToolError must not be serialized in OpenAI dict")
		}
	})
}

// TestAliasHealth_ReleasePinsForModel verifies releasePinsForModel drops
// exactly the pins whose model matches the given identity, and drops stale
// out-of-range pins.
func TestAliasHealth_ReleasePinsForModel(t *testing.T) {
	cfg := createTestConfig()
	alias := &AliasEntry{
		Models: []*ModelConfig{
			ResolveModelRef("zai/glm-4.7", cfg),
			ResolveModelRef("ollama/llama3.2", cfg),
		},
	}
	health := &AliasHealth{
		StickyPins: map[string]int{
			"session-on-glm":   0,
			"session-on-llama": 1,
			"session-stale":    7, // beyond the list
		},
	}

	health.releasePinsForModel(alias, "zai", "glm-4.7")
	assert.NotContains(t, health.StickyPins, "session-on-glm")
	assert.Contains(t, health.StickyPins, "session-on-llama")
	assert.NotContains(t, health.StickyPins, "session-stale", "stale pins are dropped")

	// Empty identity is a no-op.
	health.releasePinsForModel(alias, "", "")
	assert.Contains(t, health.StickyPins, "session-on-llama")
}
