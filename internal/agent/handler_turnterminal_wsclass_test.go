package agent

import (
	"testing"

	"github.com/caimlas/meept/internal/comm/wsclass"
)

// TestTurnTerminalEventImplementsWSClassified pins the WSClassified
// marker: turn lifecycle events render as agent_progress — never
// chat_message (blank-bubble invariant, AGENTS.md WS classification).
func TestTurnTerminalEventImplementsWSClassified(t *testing.T) {
	var _ wsclass.WSClassified = TurnTerminalEvent{}
	if got := (TurnTerminalEvent{}).WSClass(); got != wsclass.WSProgress {
		t.Errorf("TurnTerminalEvent.WSClass() = %v, want WSProgress", got)
	}
}
