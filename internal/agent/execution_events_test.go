package agent

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/comm/wsclass"
)

// Wire-contract gate tests for the shared bot/employee execution result
// payload (plan docs/plans/20261117-bot-employee-execution-observability,
// leaf 01-wire-contract/00T). RED until C1 lands the frozen
// ExecutionResultEvent and C2 declares the two typed topics. Do NOT weaken
// these assertions: they pin the frozen wire shape, the two topic names,
// and the ABSENCE of any `kind` discriminator / ExecutionKind type.

// TestExecutionResultEvent_JSONShape pins the exact JSON wire shape of the
// shared ExecutionResultEvent: the ten frozen keys, their omitempty
// modifiers, the WSProgress marker, and the absence of a `kind` key. The
// SAME shape is used for both the bot.result and employee.result topics.
func TestExecutionResultEvent_JSONShape(t *testing.T) {
	t.Run("full shape pins every frozen tag", func(t *testing.T) {
		ev := ExecutionResultEvent{
			AgentID:    "worker-1",
			Status:     "failed",
			Output:     "partial output",
			TokensUsed: 42,
			Error:      "boom",
			DurationMS: 1234,
			StopReason: StopReasonEndTurn,
			TriggerRef: "intake:repo#7",
			SessionID:  "sess-abc",
			Timestamp:  time.Date(2026, 11, 17, 12, 34, 56, 0, time.UTC),
		}

		b, err := json.Marshal(ev)
		if err != nil {
			t.Fatalf("json.Marshal(ExecutionResultEvent) error: %v", err)
		}
		var m map[string]any
		if err := json.Unmarshal(b, &m); err != nil {
			t.Fatalf("json.Unmarshal event error: %v", err)
		}

		// Every field is non-zero here, so all ten frozen keys must be
		// present with exactly these values. timestamp is checked by key
		// alone (its RFC3339 formatting is time.Time's concern, not the tag).
		want := map[string]any{
			"agent_id":    "worker-1",
			"status":      "failed",
			"output":      "partial output",
			"tokens_used": float64(42),
			"error":       "boom",
			"duration_ms": float64(1234),
			"stop_reason": "end_turn",
			"trigger_ref": "intake:repo#7",
			"session_id":  "sess-abc",
		}
		if len(m) != len(want)+1 { // +1 for timestamp
			t.Fatalf("event JSON has %d keys (got %v), want %d (+timestamp): %s", len(m), m, len(want), b)
		}
		for k, v := range want {
			got, ok := m[k]
			if !ok {
				t.Errorf("event JSON missing key %q (full: %s)", k, b)
				continue
			}
			if got != v {
				t.Errorf("event JSON %s = %v, want %v", k, got, v)
			}
		}
		if _, ok := m["timestamp"]; !ok {
			t.Errorf("event JSON missing key %q (full: %s)", "timestamp", b)
		}
		// There is NO kind discriminator: the wire shape is shared, never
		// tagged by an execution-kind field.
		if _, ok := m["kind"]; ok {
			t.Errorf("event JSON must NOT contain a %q discriminator (full: %s)", "kind", b)
		}
	})

	t.Run("minimal shape honors omitempty and rejects kind", func(t *testing.T) {
		// Worked shape from the leaf: only Status + StopReason are set.
		ev := ExecutionResultEvent{
			Status:     "completed",
			StopReason: StopReasonEndTurn,
		}
		b, err := json.Marshal(ev)
		if err != nil {
			t.Fatalf("json.Marshal(ExecutionResultEvent) error: %v", err)
		}
		var m map[string]any
		if err := json.Unmarshal(b, &m); err != nil {
			t.Fatalf("json.Unmarshal event error: %v", err)
		}

		// Non-omitempty fields are ALWAYS present: agent_id, status,
		// tokens_used, duration_ms, timestamp. stop_reason is present here
		// because it is set (it is omitempty when empty).
		for _, k := range []string{"agent_id", "status", "tokens_used", "duration_ms", "stop_reason", "timestamp"} {
			if _, ok := m[k]; !ok {
				t.Errorf("minimal event JSON missing key %q (full: %s)", k, b)
			}
		}
		// Omitted (omitempty + zero value): output, error, trigger_ref,
		// session_id. These MUST NOT appear when empty.
		for _, k := range []string{"output", "error", "trigger_ref", "session_id"} {
			if _, ok := m[k]; ok {
				t.Errorf("minimal event JSON unexpectedly contains %q (full: %s)", k, b)
			}
		}
		// No kind discriminator in EITHER shape.
		if _, ok := m["kind"]; ok {
			t.Errorf("event JSON must NOT contain a %q discriminator (full: %s)", "kind", b)
		}
	})

	// The WS marker is part of the wire contract: the shared payload
	// classifies as agent_progress for both result topics.
	if got := (ExecutionResultEvent{}).WSClass(); got != wsclass.WSProgress {
		t.Errorf("ExecutionResultEvent.WSClass() = %v, want wsclass.WSProgress", got)
	}
}

// TestResultTopics_Names pins the two frozen topic names. Both topics carry
// the SAME ExecutionResultEvent payload; there is no kind/ExecutionKind
// discriminator between them — the topic name is the only distinction.
func TestResultTopics_Names(t *testing.T) {
	if got := TopicBotResult.Name; got != "bot.result" {
		t.Errorf("TopicBotResult.Name = %q, want %q", got, "bot.result")
	}
	if got := TopicEmployeeResult.Name; got != "employee.result" {
		t.Errorf("TopicEmployeeResult.Name = %q, want %q", got, "employee.result")
	}
}
