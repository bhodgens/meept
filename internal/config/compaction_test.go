package config

import (
	"testing"
)

func TestCompactionConfig_DefaultValues(t *testing.T) {
	// Verify that DefaultConfig() provides sensible compaction defaults
	cfg := DefaultConfig()

	if cfg.Compaction.Enabled {
		t.Error("Compaction should be disabled by default")
	}
	if cfg.Compaction.ReserveTokens != 16384 {
		t.Errorf("ReserveTokens: got %d, want 16384", cfg.Compaction.ReserveTokens)
	}
	if cfg.Compaction.KeepRecentTokens != 20000 {
		t.Errorf("KeepRecentTokens: got %d, want 20000", cfg.Compaction.KeepRecentTokens)
	}
	if cfg.Compaction.MaxResponseTokens != 13107 {
		t.Errorf("MaxResponseTokens: got %d, want 13107", cfg.Compaction.MaxResponseTokens)
	}
	if cfg.Compaction.SummaryFormat != "structured" {
		t.Errorf("SummaryFormat: got %q, want %q", cfg.Compaction.SummaryFormat, "structured")
	}
	if cfg.Compaction.TriggerRatio != 0.60 {
		t.Errorf("TriggerRatio: got %f, want 0.60", cfg.Compaction.TriggerRatio)
	}
	if !cfg.Compaction.IterativeUpdates {
		t.Error("IterativeUpdates should be true by default")
	}
	if !cfg.Compaction.TrackFileOps {
		t.Error("TrackFileOps should be true by default")
	}
	if cfg.Compaction.TimeoutSeconds != 30 {
		t.Errorf("TimeoutSeconds: got %d, want 30", cfg.Compaction.TimeoutSeconds)
	}
	if cfg.Compaction.Model != "" {
		t.Errorf("Model should be empty by default, got %q", cfg.Compaction.Model)
	}
}

func TestCompactionConfig_ZeroValues(t *testing.T) {
	// Verify zero-value config (as would appear if config file omits compaction section)
	var cfg CompactionConfig

	if cfg.Enabled {
		t.Error("zero-value Enabled should be false")
	}
	if cfg.Model != "" {
		t.Errorf("zero-value Model should be empty, got %q", cfg.Model)
	}
	if cfg.ReserveTokens != 0 {
		t.Errorf("zero-value ReserveTokens should be 0, got %d", cfg.ReserveTokens)
	}
	if cfg.SummaryFormat != "" {
		t.Errorf("zero-value SummaryFormat should be empty, got %q", cfg.SummaryFormat)
	}
	if cfg.TriggerRatio != 0 {
		t.Errorf("zero-value TriggerRatio should be 0, got %f", cfg.TriggerRatio)
	}
	if cfg.TimeoutSeconds != 0 {
		t.Errorf("zero-value TimeoutSeconds should be 0, got %d", cfg.TimeoutSeconds)
	}
}

func TestCompactionConfig_OverrideValues(t *testing.T) {
	// Verify that explicit config values are stored correctly
	cfg := CompactionConfig{
		Enabled:           true,
		Model:             "zai/glm-4.5-air",
		ReserveTokens:     8192,
		KeepRecentTokens:  10000,
		MaxResponseTokens: 4096,
		SummaryFormat:     "narrative",
		TriggerRatio:      0.75,
		IterativeUpdates:  false,
		TrackFileOps:      false,
		TimeoutSeconds:    60,
	}

	if !cfg.Enabled {
		t.Error("Enabled should be true")
	}
	if cfg.Model != "zai/glm-4.5-air" {
		t.Errorf("Model: got %q, want %q", cfg.Model, "zai/glm-4.5-air")
	}
	if cfg.SummaryFormat != "narrative" {
		t.Errorf("SummaryFormat: got %q, want %q", cfg.SummaryFormat, "narrative")
	}
	if cfg.TriggerRatio != 0.75 {
		t.Errorf("TriggerRatio: got %f, want 0.75", cfg.TriggerRatio)
	}
	if cfg.IterativeUpdates {
		t.Error("IterativeUpdates should be false when explicitly set")
	}
	if cfg.TrackFileOps {
		t.Error("TrackFileOps should be false when explicitly set")
	}
	if cfg.TimeoutSeconds != 60 {
		t.Errorf("TimeoutSeconds: got %d, want 60", cfg.TimeoutSeconds)
	}
}
