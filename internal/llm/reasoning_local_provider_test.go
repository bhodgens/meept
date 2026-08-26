package llm

import "testing"

// Leaf 01 Task 2: local llama.cpp/vLLM-style providers must receive
// chat_template_kwargs.enable_thinking when a reasoning config is present.
func TestApplyOpenAICompatReasoning_LocalProvider(t *testing.T) {
	f := false

	t.Run("enabled false sends chat_template_kwargs", func(t *testing.T) {
		cfg := &ModelConfig{
			ProviderID:   "local",
			ModelID:      "lfm2.5-8b-a1b",
			Capabilities: map[string]bool{CapThinking: true},
		}
		rc := &ReasoningConfig{Enabled: &f}
		body := map[string]any{}
		applyOpenAICompatReasoning(body, cfg, rc, nil)
		ktw, ok := body["chat_template_kwargs"].(map[string]any)
		if !ok {
			t.Fatalf("chat_template_kwargs missing; body=%v", body)
		}
		if ktw["enable_thinking"] != false {
			t.Errorf("enable_thinking = %v, want false", ktw["enable_thinking"])
		}
	})

	t.Run("nil reasoning config leaves payload unchanged", func(t *testing.T) {
		cfg := &ModelConfig{
			ProviderID:   "local",
			ModelID:      "lfm2.5-8b-a1b",
			Capabilities: map[string]bool{CapThinking: true},
		}
		body := map[string]any{"model": "x"}
		applyOpenAICompatReasoning(body, cfg, nil, nil)
		for _, key := range []string{"chat_template_kwargs", "enable_thinking"} {
			if _, ok := body[key]; ok {
				t.Errorf("unexpected key %q for nil config", key)
			}
		}
		if body["model"] != "x" {
			t.Error("existing payload should be untouched")
		}
	})

	t.Run("zai provider gets no chat_template_kwargs", func(t *testing.T) {
		cfg := &ModelConfig{
			ProviderID:   ProviderIDZAI,
			ModelID:      "glm-4",
			Capabilities: map[string]bool{CapThinking: true},
		}
		rc := &ReasoningConfig{Enabled: &f}
		body := map[string]any{}
		applyOpenAICompatReasoning(body, cfg, rc, nil)
		if _, ok := body["chat_template_kwargs"]; ok {
			t.Error("zai must not receive chat_template_kwargs")
		}
	})
}
