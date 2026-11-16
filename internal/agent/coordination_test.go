package agent

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/comm/wsclass"
)

// Contract tests for the inter-agent coordination event contract (plan
// docs/plans/20261115-inter-agent-coordination, leaf 00T). These pin the
// wire strings, JSON keys, WS class, and topic name that leaf 01-01
// implements. RED until 01-01 lands.

func TestTurnState_Marshal(t *testing.T) {
	tests := []struct {
		state  TurnState
		expect string
	}{
		{TurnStateRunning, "running"},
		{TurnStateIdle, "idle"},
		{TurnStateRequiresAction, "requires_action"},
	}

	for _, tc := range tests {
		t.Run(tc.expect, func(t *testing.T) {
			b, err := json.Marshal(tc.state)
			if err != nil {
				t.Fatalf("json.Marshal(TurnState) error: %v", err)
			}
			if got := string(b); got != `"`+tc.expect+`"` {
				t.Fatalf("TurnState marshal = %s, want %q", got, tc.expect)
			}
		})
	}
}

func TestStopReason_Marshal(t *testing.T) {
	tests := []struct {
		reason StopReason
		expect string
	}{
		{StopReasonEndTurn, "end_turn"},
		{StopReasonCancelled, "cancelled"},
		{StopReasonMaxTokens, "max_tokens"},
		{StopReasonRefusal, "refusal"},
		{StopReasonMaxIterations, "max_iterations"},
		{StopReasonError, "error"},
	}

	for _, tc := range tests {
		t.Run(tc.expect, func(t *testing.T) {
			b, err := json.Marshal(tc.reason)
			if err != nil {
				t.Fatalf("json.Marshal(StopReason) error: %v", err)
			}
			if got := string(b); got != `"`+tc.expect+`"` {
				t.Fatalf("StopReason marshal = %s, want %q", got, tc.expect)
			}
		})
	}
}

func TestTurnStateEvent_WSClass(t *testing.T) {
	ev := TurnStateEvent{
		AgentID:    "a",
		SessionID:  "s",
		ChildID:    "c",
		State:      TurnStateRequiresAction,
		StopReason: StopReasonRefusal,
		Phase:      "reviewer_turn",
		Timestamp:  time.Now(),
	}

	b, err := json.Marshal(ev)
	if err != nil {
		t.Fatalf("json.Marshal(TurnStateEvent) error: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("json.Unmarshal event error: %v", err)
	}

	wantKeys := map[string]string{
		"agent_id":    "a",
		"session_id":  "s",
		"child_id":    "c",
		"state":       "requires_action",
		"stop_reason": "refusal",
		"phase":       "reviewer_turn",
	}
	for k, want := range wantKeys {
		got, ok := m[k]
		if !ok {
			t.Errorf("event JSON missing key %q (full: %s)", k, b)
			continue
		}
		if got != want {
			t.Errorf("event JSON %s = %v, want %q", k, got, want)
		}
	}
	if _, ok := m["timestamp"]; !ok {
		t.Errorf("event JSON missing key %q (full: %s)", "timestamp", b)
	}

	if got := (TurnStateEvent{}).WSClass(); got != wsclass.WSProgress {
		t.Fatalf("TurnStateEvent.WSClass() = %v, want wsclass.WSProgress", got)
	}
}

func TestTopicTurnState_Name(t *testing.T) {
	if TopicTurnState.Name != "agent.turn.state" {
		t.Fatalf("TopicTurnState.Name = %q, want %q", TopicTurnState.Name, "agent.turn.state")
	}
}
