package daemon

import (
	"testing"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/llm"
)

func TestApplyGBNFConstrainedFromConfig(t *testing.T) {
	prev := llm.GBNFConstrainedEnabled()
	t.Cleanup(func() { llm.SetGBNFConstrained(prev) })

	tests := []struct {
		name string
		cfg  config.AgentToolsConfig
		want bool
	}{
		{
			name: "enabled",
			cfg:  config.AgentToolsConfig{GBNFConstrained: true},
			want: true,
		},
		{
			name: "disabled",
			cfg:  config.AgentToolsConfig{GBNFConstrained: false},
			want: false,
		},
		{
			name: "zero-value default-off",
			cfg:  config.AgentToolsConfig{},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			applyGBNFConstrainedFromConfig(tt.cfg)
			if got := llm.GBNFConstrainedEnabled(); got != tt.want {
				t.Errorf("GBNFConstrainedEnabled() = %v, want %v", got, tt.want)
			}
		})
	}
}
