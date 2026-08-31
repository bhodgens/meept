package llm

import (
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

// TestAliasHealth_StickyPins verifies the StickyPins map accepts pins and
// that releasePinsForIndex drops exactly the pins on the given index.
func TestAliasHealth_StickyPins(t *testing.T) {
	health := &AliasHealth{
		CurrentIndex: 0,
		StickyPins:   make(map[string]int),
	}
	health.StickyPins["session-abc"] = 1
	health.StickyPins["session-other"] = 2
	assert.Equal(t, 1, health.StickyPins["session-abc"])

	health.releasePinsForIndex(1)
	assert.NotContains(t, health.StickyPins, "session-abc")
	assert.Contains(t, health.StickyPins, "session-other")
}

// TestAliasHealth_StickyPinsNilByDefault verifies StickyPins is nil until a
// sticky resolve creates it.
func TestAliasHealth_StickyPinsNilByDefault(t *testing.T) {
	health := &AliasHealth{CurrentIndex: 0}
	assert.Nil(t, health.StickyPins)
	// Release on a nil map must be a no-op, not a panic.
	health.releasePinsForIndex(0)
	assert.Nil(t, health.StickyPins)
}
