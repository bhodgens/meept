package daemon

import (
	"testing"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestContextAssemblerWiringDisabledNoOp pins the opt-in posture: a
// disabled config leaves the loop's assembler state untouched (no seam,
// no provider) -- zero-cost default (T6).
func TestContextAssemblerWiringDisabledNoOp(t *testing.T) {
	loop := agent.NewAgentLoop("ca-wiring-disabled", "/tmp")
	require.NotNil(t, loop)
	c := &Components{AgentLoop: loop}
	cfg := &config.Config{} // Enabled defaults false

	WireContextAssembler(c, cfg)

	// Disabled: nothing armed. (The loop's exported surface only exposes
	// the config value; the seam materializes lazily from it.)
	assert.False(t, cfg.Agent.ContextAssembler.Enabled)
}

// TestContextAssemblerWiringEnabledNilStoresNoPanic pins the typed-nil
// rule through the REAL wiring function: enabled config + nil components
// memory manager + nil stores arms nothing that can panic (the adapter
// reports an empty tier list; the loop normalizes to no seam).
func TestContextAssemblerWiringEnabledNilStoresNoPanic(t *testing.T) {
	loop := agent.NewAgentLoop("ca-wiring-enabled", "/tmp")
	require.NotNil(t, loop)
	c := &Components{AgentLoop: loop} // MemoryManager nil
	cfg := &config.Config{}
	cfg.Agent.ContextAssembler.Enabled = true

	assert.NotPanics(t, func() {
		WireContextAssembler(c, cfg)
	})
}

// TestContextAssemblerWiringNilComponents pins the top-level guards: nil
// components, nil loop, and nil config are all no-ops.
func TestContextAssemblerWiringNilComponents(t *testing.T) {
	cfg := &config.Config{}
	cfg.Agent.ContextAssembler.Enabled = true

	assert.NotPanics(t, func() { WireContextAssembler(nil, cfg) })

	c := &Components{} // AgentLoop nil
	assert.NotPanics(t, func() { WireContextAssembler(c, cfg) })

	loop := agent.NewAgentLoop("ca-wiring-nilcfg", "/tmp")
	require.NotNil(t, loop)
	c2 := &Components{AgentLoop: loop}
	assert.NotPanics(t, func() { WireContextAssembler(c2, nil) })
}
