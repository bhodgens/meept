package agent

import (
	"testing"

	"github.com/caimlas/meept/internal/llm"
)

// TestEffectiveClassificationCap covers the behavior table from
// .hermes/plans/classifier-reliability/02-token-caps.md.
//
// Note: llm.ModelConfig exposes the model's declared max_output tokens as
// the MaxTokens field (see internal/llm/providers.go), so cfg.MaxTokens is
// what the helper derives from.
func TestEffectiveClassificationCap(t *testing.T) {
	tests := []struct {
		name string
		cfg  *llm.ModelConfig
		want int
	}{
		{name: "nil config yields floor", cfg: nil, want: classificationFloor},
		{name: "zero max_output yields floor", cfg: &llm.ModelConfig{}, want: classificationFloor},
		{name: "max_output below floor clamps to floor", cfg: &llm.ModelConfig{MaxTokens: 512}, want: 1024},
		{name: "max_output within range used verbatim", cfg: &llm.ModelConfig{MaxTokens: 2048}, want: 2048},
		{name: "max_output above ceiling clamps to ceiling", cfg: &llm.ModelConfig{MaxTokens: 99999}, want: 2048},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := effectiveClassificationCap(tt.cfg)
			if got != tt.want {
				t.Errorf("effectiveClassificationCap(%v) = %d, want %d", tt.cfg, got, tt.want)
			}
		})
	}
}
