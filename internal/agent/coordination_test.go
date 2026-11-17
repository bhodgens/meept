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
		state  AgentTurnState
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
				t.Fatalf("json.Marshal(AgentTurnState) error: %v", err)
			}
			if got := string(b); got != `"`+tc.expect+`"` {
				t.Fatalf("AgentTurnState marshal = %s, want %q", got, tc.expect)
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

// TestTurnStateEvent_BothShapesDecode pins the Q2 ratification (2026-11-16):
// the pair-phase shape (phase set) and the plain delegate/ACP shape (phase
// absent) both decode into TurnStateEvent without error. Phase is optional;
// no consumer may require it.
func TestTurnStateEvent_BothShapesDecode(t *testing.T) {
	shapes := map[string]string{
		"pair phase shape (phase present)": `{"agent_id":"pair-1","session_id":"s","state":"running","phase":"reviewer_turn","timestamp":"2026-11-16T00:00:00Z"}`,
		"plain shape (phase absent)":       `{"agent_id":"a","session_id":"s","state":"idle","stop_reason":"end_turn","timestamp":"2026-11-16T00:00:00Z"}`,
		"pair completed shape":             `{"agent_id":"pair-1","state":"idle","stop_reason":"end_turn","phase":"completed","timestamp":"2026-11-16T00:00:00Z"}`,
		"pair failed shape":                `{"agent_id":"pair-1","state":"idle","stop_reason":"error","phase":"failed","timestamp":"2026-11-16T00:00:00Z"}`,
	}
	for name, raw := range shapes {
		t.Run(name, func(t *testing.T) {
			var ev TurnStateEvent
			if err := json.Unmarshal([]byte(raw), &ev); err != nil {
				t.Fatalf("unmarshal error: %v", err)
			}
			if ev.AgentID == "" || ev.State == "" {
				t.Fatalf("decoded event missing required fields: %+v", ev)
			}
		})
	}
	// The phase-absent shape must re-marshal without a phase key.
	var plain TurnStateEvent
	if err := json.Unmarshal([]byte(shapes["plain shape (phase absent)"]), &plain); err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(plain)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	if _, ok := m["phase"]; ok {
		t.Fatalf("phase key present in re-marshalled plain shape: %s", b)
	}
}
