package tui

import "testing"

// TestDefaultEventStreamConfig_SubscribesTurnTerminal pins the TUI event
// stream's subscription to the turn.terminal topic (leaf 01-turn-terminal-
// event Task 5). Without the subscription the TUI never sees a turn's
// terminal outcome and the timeout pending-indicator stays dead.
func TestDefaultEventStreamConfig_SubscribesTurnTerminal(t *testing.T) {
	cfg := DefaultEventStreamConfig()
	found := false
	for _, topic := range cfg.Topics {
		if topic == "turn.terminal" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("DefaultEventStreamConfig topics %v missing \"turn.terminal\"", cfg.Topics)
	}
}
