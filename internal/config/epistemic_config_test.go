package config

import "testing"

func TestEpistemicConfigDefaults(t *testing.T) {
	cfg := MemoryConfig{}
	// Ambient extraction must default to false.
	if cfg.Epistemic.AmbientExtraction.Enabled {
		t.Error("ambient extraction must default to false")
	}
	// AutoTrustWeight default is 0 (helpers substitute DefaultAutoClaimTrustWeight).
	if cfg.Epistemic.AutoTrustWeight != 0 {
		t.Errorf("AutoTrustWeight zero-value expected, got %v", cfg.Epistemic.AutoTrustWeight)
	}
}

func TestEpistemicConfigRoundTrip(t *testing.T) {
	cfg := EpistemicConfig{
		AmbientExtraction: AmbientExtractionConfig{
			Enabled:             true,
			ConfidenceThreshold: 0.75,
			MaxPerTurn:          3,
			ExcludeIntents:      []string{"chat"},
			ExcludeCategories:   []string{"joke"},
			ContextWindow:       5,
		},
		AutoTrustWeight:       0.5,
		DetectionThreshold:    0.7,
		ReviewPromptFrequency: "weekly",
		MaxPendingReviews:     20,
	}
	if !cfg.AmbientExtraction.Enabled {
		t.Error("Enabled did not round-trip")
	}
	if cfg.AutoTrustWeight != 0.5 {
		t.Errorf("AutoTrustWeight got %v, want 0.5", cfg.AutoTrustWeight)
	}
}
