package config

import (
	"testing"
)

// T5 (turn-journal leaf 01): daemon.turn_journal_enabled is an opt-in
// flag, default FALSE (R0 finding D3 / docs/agents/opt-in-defaults.md:
// flipping a default is a product decision).
func TestTurnJournalFlag(t *testing.T) {
	// A fresh default config has the flag false.
	cfg := DefaultConfig()
	if cfg.Daemon.TurnJournalEnabled {
		t.Fatal("TurnJournalEnabled default = true, want false")
	}

	// A JSON5 config carrying turn_journal_enabled: true parses true
	// (snake_case key under the daemon block, template spelling).
	if err := UnmarshalJSON5([]byte(`{"daemon": {"turn_journal_enabled": true}}`), cfg); err != nil {
		t.Fatalf("unmarshal true case: %v", err)
	}
	if !cfg.Daemon.TurnJournalEnabled {
		t.Fatal("turn_journal_enabled: true did not parse to true")
	}

	// And false parses back to false.
	if err := UnmarshalJSON5([]byte(`{"daemon": {"turn_journal_enabled": false}}`), cfg); err != nil {
		t.Fatalf("unmarshal false case: %v", err)
	}
	if cfg.Daemon.TurnJournalEnabled {
		t.Fatal("turn_journal_enabled: false parsed to true")
	}
}
