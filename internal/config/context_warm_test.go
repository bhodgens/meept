package config

// Phase2-context-warm leaf 02 (C6): voice.context_warm opt-in defaults —
// the flag is false out of the box (byte-identical posture,
// docs/agents/opt-in-defaults.md) and the token floor ships at 60.

import "testing"

func TestDefaultConfigContextWarm(t *testing.T) {
	v := DefaultConfig().Voice
	if v.ContextWarm {
		t.Error("voice.context_warm default = true, want false (opt-in)")
	}
	if v.ContextWarmMinTokens != 60 {
		t.Errorf("voice.context_warm_min_tokens default = %d, want 60", v.ContextWarmMinTokens)
	}
}
