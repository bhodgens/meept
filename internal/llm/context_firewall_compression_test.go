package llm

import (
	"context"
	"strings"
	"testing"
)

// TestContextFirewallMultiStageCompression verifies that the ContextFirewall
// correctly delegates to the embedded ContextCompressor at each stage when
// ProactiveCompression is enabled.
func TestContextFirewallMultiStageCompression(t *testing.T) {
	// Model with a 1000-token context limit; hard limit at 0.80 so the
	// ValidateContextSize gate still passes at 800 tokens.
	model := &ModelConfig{ContextLimit: 1000}

	cfg := ContextFirewallConfig{
		Enabled:                true,
		ProactiveCompression:   true,
		DropContextOnHardLimit: false,
		WrapUpThreshold:        0.50,
		HardLimit:              0.80,
	}
	inner := &stubChatter{resp: &Response{Content: "ok"}}
	firewall := NewContextFirewall(inner, model, cfg, nil, nil, &HeuristicTokenizer{})

	t.Run("compressor_not_nil", func(t *testing.T) {
		if firewall.compressor == nil {
			t.Fatal("expected compressor to be initialised when ProactiveCompression=true")
		}
	})

	t.Run("compress_returns_none_when_below_threshold", func(t *testing.T) {
		msgs := []ChatMessage{
			{Role: RoleSystem, Content: "system prompt"},
			{Role: RoleUser, Content: makeFiller(10)}, // 10 tokens
		}
		result, err := firewall.Compress(context.Background(), msgs)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.Compressed {
			t.Error("should not compress when utilization is low")
		}
		if result.Stage != CompressionStageNone {
			t.Errorf("expected stage None, got %s", result.Stage)
		}
	})

	t.Run("compress_returns_warning_at_stage1", func(t *testing.T) {
		// 550 tokens out of 1000 = 0.55 utilization, which is in the warning zone [0.50, 0.60).
		msgs := makeCompressorMessages(5, 100) // 1 system (~2 tokens) + 5 user x 100 tokens = ~502 tokens
		// Add extra to get closer to 550
		msgs = append(msgs, ChatMessage{Role: RoleUser, Content: strings.Repeat("y", 150)})

		result, err := firewall.Compress(context.Background(), msgs)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.Stage != CompressionStageWarning {
			t.Errorf("expected stage Warning, got %s (util ~%.2f)", result.Stage,
				float64(result.TokensBefore)/float64(model.ContextLimit))
		}
		if result.Compressed {
			t.Error("warning stage should not alter messages")
		}
		if len(result.Messages) != len(msgs) {
			t.Error("warning stage should not change message count")
		}
	})

	t.Run("compress_returns_summarize_at_stage2", func(t *testing.T) {
		// 650 tokens out of 1000 = 0.65 utilization, in the summarize zone [0.60, 0.70).
		msgs := makeCompressorMessages(6, 100) // ~602 tokens
		msgs = append(msgs, ChatMessage{Role: RoleUser, Content: strings.Repeat("y", 150)})

		result, err := firewall.Compress(context.Background(), msgs)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.Stage != CompressionStageSummarize {
			t.Errorf("expected stage Summarize, got %s (util ~%.2f)", result.Stage,
				float64(result.TokensBefore)/float64(model.ContextLimit))
		}
		if !result.Compressed {
			t.Error("summarize stage should set Compressed=true")
		}
		// After LLM summarization: original system + summary system message + last 4 non-system
		systemCount := 0
		for _, m := range result.Messages {
			if m.Role == RoleSystem {
				systemCount++
			}
		}
		if systemCount != 2 {
			t.Errorf("expected 2 system messages (original + summary), got %d", systemCount)
		}
		if len(result.Messages) > 6 {
			t.Errorf("expected at most 6 messages (1 system + 1 summary + 4 kept), got %d", len(result.Messages))
		}
		if result.DroppedCount == 0 {
			t.Error("expected DroppedCount > 0 at summarize stage")
		}
	})

	t.Run("compress_returns_aggressive_at_stage3", func(t *testing.T) {
		// 750 tokens out of 1000 = 0.75 utilization, in aggressive zone [0.70, 0.80).
		msgs := makeCompressorMessages(7, 100) // ~702 tokens
		msgs = append(msgs, ChatMessage{Role: RoleUser, Content: strings.Repeat("y", 150)})

		result, err := firewall.Compress(context.Background(), msgs)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.Stage != CompressionStageAggressive {
			t.Errorf("expected stage Aggressive, got %s (util ~%.2f)", result.Stage,
				float64(result.TokensBefore)/float64(model.ContextLimit))
		}
		if !result.Compressed {
			t.Error("aggressive stage should set Compressed=true")
		}
		// After aggressive compression: system + last 4 non-system messages
		if len(result.Messages) > 5 {
			t.Errorf("expected at most 5 messages (1 system + 4 kept), got %d", len(result.Messages))
		}
		if result.TokensAfter >= result.TokensBefore {
			t.Errorf("TokensAfter (%d) should be less than TokensBefore (%d)",
				result.TokensAfter, result.TokensBefore)
		}
	})

	t.Run("compress_returns_hardlimit_at_stage4", func(t *testing.T) {
		// 850 tokens out of 1000 = 0.85 utilization, at hard limit stage.
		msgs := makeCompressorMessages(8, 100) // ~802 tokens
		msgs = append(msgs, ChatMessage{Role: RoleUser, Content: strings.Repeat("y", 150)})

		result, err := firewall.Compress(context.Background(), msgs)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.Stage != CompressionStageHardLimit {
			t.Errorf("expected stage HardLimit, got %s (util ~%.2f)", result.Stage,
				float64(result.TokensBefore)/float64(model.ContextLimit))
		}
		if !result.Compressed {
			t.Error("hard limit stage should set Compressed=true")
		}
		if len(result.Messages) > 3 {
			t.Errorf("expected at most 3 messages (1 system + 2 kept), got %d", len(result.Messages))
		}
	})

	t.Run("chat_passes_through_compressor", func(t *testing.T) {
		// Verify that Chat() invokes the compressor inside processMessages.
		msgs := []ChatMessage{
			{Role: RoleSystem, Content: "system prompt"},
			{Role: RoleUser, Content: "hello"},
		}
		resp, err := firewall.Chat(context.Background(), msgs)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.Content != "ok" {
			t.Errorf("unexpected response: %s", resp.Content)
		}
	})

	t.Run("compressor_stats_accumulate", func(t *testing.T) {
		// Fresh firewall for isolated stats.
		fw := NewContextFirewall(inner, model, cfg, nil, nil, &HeuristicTokenizer{})

		// Run compress at summarize level multiple times.
		msgs := makeCompressorMessages(6, 100)
		msgs = append(msgs, ChatMessage{Role: RoleUser, Content: strings.Repeat("y", 150)})

		fw.Compress(context.Background(), msgs)
		fw.Compress(context.Background(), msgs)

		stats := fw.compressor.Stats()
		if stats.SummarizeEvents != 2 {
			t.Errorf("expected 2 SummarizeEvents, got %d", stats.SummarizeEvents)
		}
		if stats.TotalTokensSaved == 0 {
			t.Error("expected TotalTokensSaved > 0 after repeated compressions")
		}
	})
}

// TestContextFirewallCompressionDisabled verifies that the firewall works
// normally when ProactiveCompression is false.
func TestContextFirewallCompressionDisabled(t *testing.T) {
	model := &ModelConfig{ContextLimit: 1000}
	cfg := ContextFirewallConfig{
		Enabled:              true,
		ProactiveCompression: false,
	}
	inner := &stubChatter{resp: &Response{Content: "ok"}}
	firewall := NewContextFirewall(inner, model, cfg, nil, nil, &HeuristicTokenizer{})

	t.Run("compressor_is_nil", func(t *testing.T) {
		if firewall.compressor != nil {
			t.Error("compressor should be nil when ProactiveCompression=false")
		}
	})

	t.Run("compress_returns_none", func(t *testing.T) {
		msgs := makeCompressorMessages(10, 100)
		result, err := firewall.Compress(context.Background(), msgs)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.Compressed {
			t.Error("should not compress when proactive compression is disabled")
		}
		if result.Stage != CompressionStageNone {
			t.Errorf("expected stage None, got %s", result.Stage)
		}
		if len(result.Messages) != len(msgs) {
			t.Error("messages should be unchanged")
		}
	})
}

// TestContextFirewallCompressionWithModelContextLimitOverride verifies that
// ModelContextLimit in the firewall config is forwarded to the compressor.
func TestContextFirewallCompressionWithModelContextLimitOverride(t *testing.T) {
	model := &ModelConfig{ContextLimit: 10000} // large model limit
	cfg := ContextFirewallConfig{
		Enabled:              true,
		ProactiveCompression: true,
		ModelContextLimit:    1000, // override to small limit
	}
	inner := &stubChatter{resp: &Response{Content: "ok"}}
	firewall := NewContextFirewall(inner, model, cfg, nil, nil, &HeuristicTokenizer{})

	if firewall.compressor == nil {
		t.Fatal("expected compressor to be initialised")
	}
	// The compressor should use the overridden limit, not the model's.
	if firewall.compressor.config.ModelContextLimit != 1000 {
		t.Errorf("expected ModelContextLimit=1000, got %d",
			firewall.compressor.config.ModelContextLimit)
	}
}
