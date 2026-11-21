package activity

import (
	"context"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/bus"
)

// TestTurnTerminalAppendsSpecialistRun pins C5: one execution_runs row with
// tier "specialist" per completed specialist chat turn, written from the
// turn.terminal subscriber. RED today because WireExecutionProjector
// subscribes only to bot.result and employee.result. This test does not
// add that subscription.
func TestTurnTerminalAppendsSpecialistRun(t *testing.T) {
	s := openTestExecutionStore(t)
	msgBus := bus.New(nil, nil)
	t.Cleanup(func() { msgBus.Close() })

	proj := WireExecutionProjector(msgBus, s, nil)
	if proj == nil {
		t.Fatal("WireExecutionProjector returned nil for a live bus and store")
	}
	t.Cleanup(func() { proj.Stop(msgBus) })

	bus.PublishT(msgBus, agent.TopicTurnTerminal, "chat", agent.TurnTerminalEvent{
		ConversationID: "conv-coder",
		SessionID:      "sess-coder",
		TurnID:         "turn-coder",
		AgentID:        "coder",
		HandlerCase:    "specialist",
		Status:         "completed",
		Reply:          "patched the gate",
		DurationMS:     40,
	})

	deadline := time.Now().Add(2 * time.Second)
	var specialist []ExecutionRun
	for {
		runs, _, err := s.List(context.Background(), ExecutionQuery{AgentID: "coder"})
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		specialist = specialist[:0]
		for _, run := range runs {
			if run.Tier == "specialist" {
				specialist = append(specialist, run)
			}
		}
		if len(specialist) == 1 || time.Now().After(deadline) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if len(specialist) != 1 {
		t.Fatalf("specialist execution_runs for agent %q = %d, want 1 (projector has no turn.terminal subscription)", "coder", len(specialist))
	}
}
