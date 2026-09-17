package http

import (
	"encoding/json"
	"testing"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/comm/wsclass"
)

// TestDecodeTurnTerminalWSClassMarker pins that the typed decode -> marker
// chain (not any topic prefix) yields WSProgress: decode a marshaled
// TurnTerminalEvent via the table's decoder and assert its WSClass marker
// directly.
func TestDecodeTurnTerminalWSClassMarker(t *testing.T) {
	enc, err := json.Marshal(agent.TurnTerminalEvent{
		ConversationID: "c1",
		TurnID:         "t1",
		Status:         "completed",
	})
	if err != nil {
		t.Fatal(err)
	}
	typed, err := decodeTurnTerminalWSClass(enc)
	if err != nil {
		t.Fatal(err)
	}
	if typed.WSClass() != wsclass.WSProgress {
		t.Errorf("decoded marker WSClass = %v, want WSProgress", typed.WSClass())
	}
	// And the topic key in the table is the declared typed topic name.
	if _, ok := typedPayloadDecoders[agent.TopicTurnTerminal.Name]; !ok {
		t.Errorf("typedPayloadDecoders missing entry for %q", agent.TopicTurnTerminal.Name)
	}
}
