package shadow

import "testing"

func TestAdaptersConfig_Defaults(t *testing.T) {
	cfg := DefaultConfig()
	if !cfg.Adapters.HotSwapEnabled {
		t.Errorf("HotSwapEnabled default should be true, got false")
	}
	if cfg.Adapters.EvalThreshold != 0.7 {
		t.Errorf("EvalThreshold default should be 0.7, got %v", cfg.Adapters.EvalThreshold)
	}
}
