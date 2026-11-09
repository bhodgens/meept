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

// TestBuildChatRequestAdapterPath verifies that buildChatRequest stamps
// the field mlx_lm.server applies ("adapters") when WithAdapter is set,
// and omits it when not set. "adapter_path" is not that field.
func TestBuildChatRequestAdapterPath(t *testing.T) {
	cfg := &ModelConfig{
		ModelID:     "test-model",
		Temperature: 0.7,
		MaxTokens:   100,
	}
	messages := []ChatMessage{
		{Role: RoleUser, Content: "hello"},
	}

	t.Run("payload includes adapters when set", func(t *testing.T) {
		_, payload, err := buildChatRequestForTest(t, messages, cfg, []ChatOption{
			WithAdapter("/adapters/code-lora"),
		})
		if err != nil {
			t.Fatalf("buildChatRequest failed: %v", err)
		}
		got, ok := payload[runtimeLoRAField]
		if !ok {
			t.Fatalf("expected %q key in payload, not found", runtimeLoRAField)
		}
		if got != "/adapters/code-lora" {
			t.Errorf("%s = %v, want %q", runtimeLoRAField, got, "/adapters/code-lora")
		}
		if _, ok := payload["adapter_path"]; ok {
			t.Error("adapter_path is ignored by the runtime that applies weights")
		}
	})

	t.Run("payload omits adapters when not set", func(t *testing.T) {
		_, payload, err := buildChatRequestForTest(t, messages, cfg, nil)
		if err != nil {
			t.Fatalf("buildChatRequest failed: %v", err)
		}
		if _, ok := payload[runtimeLoRAField]; ok {
			t.Errorf("did not expect %q key in payload when WithAdapter not used", runtimeLoRAField)
		}
	})

	t.Run("payload omits adapters when empty string", func(t *testing.T) {
		_, payload, err := buildChatRequestForTest(t, messages, cfg, []ChatOption{
			WithAdapter(""),
		})
		if err != nil {
			t.Fatalf("buildChatRequest failed: %v", err)
		}
		if _, ok := payload[runtimeLoRAField]; ok {
			t.Errorf("did not expect %q key in payload when path is empty", runtimeLoRAField)
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
