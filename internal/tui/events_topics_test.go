package tui

import (
	"slices"
	"testing"
)

// TestDefaultEventStreamConfig_SubscribesTurnTerminal pins the TUI event
// stream's subscription to the turn.terminal topic (leaf 01-turn-terminal-
// event Task 5). Without the subscription the TUI never sees a turn's
// terminal outcome and the timeout pending-indicator stays dead.
func TestDefaultEventStreamConfig_SubscribesTurnTerminal(t *testing.T) {
	cfg := DefaultEventStreamConfig()
	if !slices.Contains(cfg.Topics, "turn.terminal") {
		t.Errorf("DefaultEventStreamConfig topics %v missing \"turn.terminal\"", cfg.Topics)
	}
}

// TestDefaultEventStreamConfig_SubscribesACPPermissions pins the TUI event
// stream's subscription to the ACP human-permission relay topics
// (acp-human-permission-relay leaf 04, Contract D). The daemon publishes
// acp.permission.requested/.resolved (3-segment topics); without a
// matching wildcard pattern in the subscribe list the TUI never sees a
// relay frame and no permission modal can open. matchWildcard requires
// EQUAL SEGMENT COUNTS (internal/bus/bus.go), so "acp.*" would not match —
// the pattern must be the 3-segment "acp.*.*". It stays pairwise
// non-overlapping with every other pattern (TestTopicPatternsAreNonOverlapping
// invariant).
func TestDefaultEventStreamConfig_SubscribesACPPermissions(t *testing.T) {
	cfg := DefaultEventStreamConfig()
	if !slices.Contains(cfg.Topics, "acp.*.*") {
		t.Errorf("DefaultEventStreamConfig topics %v missing \"acp.*.*\"", cfg.Topics)
	}
}
