//go:build e2e

// Package busdelivery is the 01-safety TEST-GATE T10 e2e suite for the
// C6 critical event delivery contract (W4/W24; master matrix F4,
// scenarios bus-delivery-01..02).
//
// Boots-leg tests through the live bus.subscribe/bus.poll RPC surface:
// a subscription flooded with progress events must still deliver the
// critical terminal events (turn.terminal, task.completed) - critical
// topics are exempt from the drop-oldest trim (separate ring, cap 256),
// and dropped>0 stays in-band.
//
// RED today: internal/rpc/proxy.go buffers MaxEvents:100 with an
// indiscriminate drop-oldest trim, so 150 progress events evict the
// critical terminal published before them.

package busdelivery

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/caimlas/meept/e2e/harness"
)

// floodResult carries the accumulated poll observations.
type floodResult struct {
	criticalSeen int
	dropped      int64
}

// floodAndCollect subscribes to progress + one critical topic, publishes
// 150 progress events THEN one critical event, and polls (accumulating
// batch-wise, echoing next_seq) until the drop accounting shows overflow.
// The critical event is published LAST so a correct implementation keeps
// it in the newest 100 even without the ring; the gate therefore ALSO
// publishes a second critical event BEFORE the flood - that one is the
// drop-oldest casualty today and the ring survivor after the fix.
func floodAndCollect(t *testing.T, s *harness.Stack, criticalTopic string, criticalPayload map[string]any) floodResult {
	t.Helper()
	client := harness.DialRPC(t, s.SocketPath)
	sub := client.CallResult("bus.subscribe", map[string]any{
		"topics": []string{"task.progress", criticalTopic},
	})
	subID, _ := sub["subscription_id"].(string)
	if subID == "" {
		t.Fatalf("bus.subscribe returned no subscription_id: %v", sub)
	}

	// Critical event FIRST: under drop-oldest it is the first casualty.
	client.CallResult("bus.publish", map[string]any{
		"topic":   criticalTopic,
		"payload": criticalPayload,
	})
	for i := 0; i < 150; i++ {
		client.CallResult("bus.publish", map[string]any{
			"topic": "task.progress",
			"payload": map[string]any{
				"task_id":  "task-flood",
				"progress": i,
			},
		})
	}

	var out floodResult
	var sinceSeq int64
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		raw := client.CallResult("bus.poll", map[string]any{
			"subscription_id": subID,
			"since_seq":       sinceSeq,
		})
		data, err := json.Marshal(raw)
		if err != nil {
			t.Fatalf("marshal poll result: %v", err)
		}
		var page struct {
			Events []struct {
				Topic   string          `json:"topic"`
				Payload json.RawMessage `json:"payload"`
				Seq     int64           `json:"seq"`
			} `json:"events"`
			NextSeq int64 `json:"next_seq"`
			Dropped int64 `json:"dropped"`
		}
		if err := json.Unmarshal(data, &page); err != nil {
			t.Fatalf("decode poll page: %v", err)
		}
		for _, ev := range page.Events {
			if ev.Topic == criticalTopic {
				out.criticalSeen++
			}
		}
		if page.NextSeq > sinceSeq {
			sinceSeq = page.NextSeq
		}
		if page.Dropped > out.dropped {
			out.dropped = page.Dropped
		}
		if out.dropped > 0 {
			// Overflow observed; one more settle poll for stragglers.
			time.Sleep(500 * time.Millisecond)
			continue
		}
		time.Sleep(250 * time.Millisecond)
	}
	return out
}

// TestCriticalTerminalSurvivesProgressFlood pins C6 for turn.terminal:
// after 150 progress events overflow the 100-event buffer, the terminal
// published BEFORE the flood must still be delivered (critical ring),
// and the loss of progress events must be counted in-band (dropped>0).
// RED today: the terminal is trimmed away with the progress events.
func TestCriticalTerminalSurvivesProgressFlood(t *testing.T) {
	s := harness.Start(t)
	got := floodAndCollect(t, s, "turn.terminal", map[string]any{
		"conversation_id": "conv-crit",
		"session_id":      "sess-crit",
		"turn_id":         "turn-crit-01",
		"status":          "completed",
		"reply":           "critical terminal probe",
	})
	if got.dropped == 0 {
		t.Fatalf("flood produced no in-band drop accounting (dropped=0); the overflow rig did not exercise the trim")
	}
	if got.criticalSeen == 0 {
		t.Errorf("CURRENT DEFECT (C6): turn.terminal was lost to the drop-oldest trim under a 150-event progress flood (dropped=%d); critical topics need the exempt ring (cap 256)", got.dropped)
	}
}

// TestTaskCompletedSurvivesProgressFlood pins C6 for task.completed:
// the same flood must not evict a task completion event. RED today:
// trimmed like any progress event.
func TestTaskCompletedSurvivesProgressFlood(t *testing.T) {
	s := harness.Start(t)
	got := floodAndCollect(t, s, "task.completed", map[string]any{
		"task_id": "task-crit-01",
		"name":    "critical completion probe",
	})
	if got.dropped == 0 {
		t.Fatalf("flood produced no in-band drop accounting (dropped=0); the overflow rig did not exercise the trim")
	}
	if got.criticalSeen == 0 {
		t.Errorf("CURRENT DEFECT (C6): task.completed was lost to the drop-oldest trim under a 150-event progress flood (dropped=%d); C6 requires blocking publish + critical-ring delivery", got.dropped)
	}
}
