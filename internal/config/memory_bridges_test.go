package config

import "testing"

// TestMemoryBridgesDefaults: the default config ships memory.bridges with
// enabled=false, kb=2, bridge_df_max=10, verify=true (branch 09 contract 6).
func TestMemoryBridgesDefaults(t *testing.T) {
	cfg := DefaultConfig()
	b := cfg.Memory.Bridges
	if b.Enabled {
		t.Error("memory.bridges.enabled default is true, want false (opt-in)")
	}
	if b.KB != 2 {
		t.Errorf("memory.bridges.kb = %d, want 2", b.KB)
	}
	if b.BridgeDFMax != 10 {
		t.Errorf("memory.bridges.bridge_df_max = %d, want 10", b.BridgeDFMax)
	}
	if !b.Verify {
		t.Error("memory.bridges.verify default is false, want true")
	}
}

// TestMemoryBridgesZeroValueDisabled: a zero-value MemoryBridgesConfig must
// read as disabled (fail-closed for configs predating the key).
func TestMemoryBridgesZeroValueDisabled(t *testing.T) {
	var b struct{ Bridges MemoryBridgesConfig }
	_ = b
	var zero MemoryBridgesConfig
	if zero.Enabled {
		t.Error("zero-value MemoryBridgesConfig reports enabled")
	}
}
