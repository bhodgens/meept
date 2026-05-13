package llm

import (
	"testing"
)

func TestCompactorFromConfig_Defaults(t *testing.T) {
	// Verify that default CompactorConfig values produce a valid compactor
	compactorCfg := DefaultCompactorConfig()

	compactor := NewContextCompactor(compactorCfg, nil, nil, nil)
	if compactor == nil {
		t.Fatal("NewContextCompactor returned nil")
	}
	if compactor.config.ReserveTokens != 16384 {
		t.Errorf("ReserveTokens: got %d, want 16384", compactor.config.ReserveTokens)
	}
	if compactor.config.SummaryFormat != "structured" {
		t.Errorf("SummaryFormat: got %q, want %q", compactor.config.SummaryFormat, "structured")
	}
}

func TestCompactorConfig_ZeroValues(t *testing.T) {
	// Verify zero-value config fields
	var cfg CompactorConfig

	if cfg.ReserveTokens != 0 {
		t.Errorf("zero-value ReserveTokens should be 0, got %d", cfg.ReserveTokens)
	}
	if cfg.SummaryFormat != "" {
		t.Errorf("zero-value SummaryFormat should be empty, got %q", cfg.SummaryFormat)
	}
	if cfg.TimeoutSeconds != 0 {
		t.Errorf("zero-value TimeoutSeconds should be 0, got %d", cfg.TimeoutSeconds)
	}
}

func TestCompactorConfig_OverrideValues(t *testing.T) {
	// Verify that custom config values are respected
	cfg := CompactorConfig{
		ReserveTokens:     8192,
		KeepRecentTokens:  10000,
		MaxResponseTokens: 4096,
		SummaryFormat:     "narrative",
		TrackFileOps:      false,
		TimeoutSeconds:    60,
	}

	compactor := NewContextCompactor(cfg, nil, nil, nil)
	if compactor == nil {
		t.Fatal("NewContextCompactor returned nil")
	}
	if compactor.config.SummaryFormat != "narrative" {
		t.Errorf("SummaryFormat: got %q, want %q", compactor.config.SummaryFormat, "narrative")
	}
	if compactor.config.TrackFileOps {
		t.Error("TrackFileOps: got true, want false")
	}
	if compactor.config.TimeoutSeconds != 60 {
		t.Errorf("TimeoutSeconds: got %d, want 60", compactor.config.TimeoutSeconds)
	}
}

func TestCompactorNilSummarizer_DoesNotCompact(t *testing.T) {
	// Compaction with nil summarizer should return messages unchanged
	cfg := DefaultCompactorConfig()
	compactor := NewContextCompactor(cfg, nil, nil, nil)

	msgs := []ChatMessage{
		{Role: RoleSystem, Content: "system prompt"},
		{Role: RoleUser, Content: "hello"},
		{Role: RoleAssistant, Content: "hi there"},
	}

	result := compactor.Compact(t.Context(), msgs)
	if result.Compacted {
		t.Error("should not compact with nil summarizer")
	}
	if len(result.Messages) != len(msgs) {
		t.Errorf("message count: got %d, want %d", len(result.Messages), len(msgs))
	}
}

func TestModelBroker_ChatterForModel_Empty(t *testing.T) {
	// Test that ChatterForModel returns nil for unknown models on an empty broker
	broker := NewModelBroker(BrokerConfig{
		ProvidersConfig: &ProvidersConfig{},
		Logger:          nil,
	})
	if ch := broker.ChatterForModel("nonexistent/model"); ch != nil {
		t.Error("expected nil for unknown model")
	}
}

func TestTypeAssertion_NonBrokerChatter(t *testing.T) {
	// Verify the type assertion pattern used in loop.go:
	// A regular Client should not be a ModelBroker
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("unexpected panic: %v", r)
		}
	}()

	var chatter Chatter = NewClient(&ModelConfig{
		BaseURL: "http://localhost",
		APIKey:  "test",
		ModelID: "test",
	}, WithLogger(nil))
	if _, ok := chatter.(*ModelBroker); ok {
		t.Error("Client should not type-assert to *ModelBroker")
	}
}
