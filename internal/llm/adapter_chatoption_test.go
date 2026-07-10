package llm

import (
	"testing"
)

// TestWithAdapterOption verifies that WithAdapter populates the adapterPath
// field on chatOptions.
func TestWithAdapterOption(t *testing.T) {
	t.Run("sets path when non-empty", func(t *testing.T) {
		opts := &chatOptions{}
		WithAdapter("/models/lora/code-adapter")(opts)
		if opts.adapterPath != "/models/lora/code-adapter" {
			t.Errorf("adapterPath = %q, want %q", opts.adapterPath, "/models/lora/code-adapter")
		}
	})

	t.Run("empty path leaves field empty", func(t *testing.T) {
		opts := &chatOptions{}
		WithAdapter("")(opts)
		if opts.adapterPath != "" {
			t.Errorf("adapterPath = %q, want empty", opts.adapterPath)
		}
	})
}

// TestBuildChatRequestAdapterPath verifies that buildChatRequest includes
// "adapter_path" in the payload when WithAdapter is set, and omits it when
// not set (backward compatibility).
func TestBuildChatRequestAdapterPath(t *testing.T) {
	cfg := &ModelConfig{
		ModelID:     "test-model",
		Temperature: 0.7,
		MaxTokens:   100,
	}
	messages := []ChatMessage{
		{Role: RoleUser, Content: "hello"},
	}

	t.Run("payload includes adapter_path when set", func(t *testing.T) {
		_, payload, err := buildChatRequestForTest(t, messages, cfg, []ChatOption{
			WithAdapter("/adapters/code-lora"),
		})
		if err != nil {
			t.Fatalf("buildChatRequest failed: %v", err)
		}
		got, ok := payload["adapter_path"]
		if !ok {
			t.Fatal("expected adapter_path key in payload, not found")
		}
		if got != "/adapters/code-lora" {
			t.Errorf("adapter_path = %v, want %q", got, "/adapters/code-lora")
		}
	})

	t.Run("payload omits adapter_path when not set", func(t *testing.T) {
		_, payload, err := buildChatRequestForTest(t, messages, cfg, nil)
		if err != nil {
			t.Fatalf("buildChatRequest failed: %v", err)
		}
		if _, ok := payload["adapter_path"]; ok {
			t.Error("did not expect adapter_path key in payload when WithAdapter not used")
		}
	})

	t.Run("payload omits adapter_path when empty string", func(t *testing.T) {
		_, payload, err := buildChatRequestForTest(t, messages, cfg, []ChatOption{
			WithAdapter(""),
		})
		if err != nil {
			t.Fatalf("buildChatRequest failed: %v", err)
		}
		if _, ok := payload["adapter_path"]; ok {
			t.Error("did not expect adapter_path key in payload when path is empty")
		}
	})
}

// buildChatRequestForTest wraps the unexported buildChatRequest method for
// table-driven tests. It uses a minimal Client whose configMu and other
// fields are not needed by buildChatRequest itself.
func buildChatRequestForTest(t *testing.T, messages []ChatMessage, cfg *ModelConfig, opts []ChatOption) (*chatOptions, map[string]any, error) {
	t.Helper()
	c := NewClient(cfg)
	return c.buildChatRequest(messages, cfg, opts, false)
}
