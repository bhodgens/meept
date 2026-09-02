package employee

// Leaf 04 (universal parking observability) tests: the goal-loop park site
// emits park events through the SHARED parker's event bus on the existing
// agent.quota_wait topic (DECISIONS.md D9 — all turn types observable; the
// employee package needs no bus wiring of its own). Payload keys are
// asserted as two-value map reads per repo convention.

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/llm"
)

// TestEpisodeParker_ParkEmitsQuotaWaitEvent pins that parking a goal-loop
// episode publishes ONE ParkTurnEvent on agent.quota_wait via the shared
// parker (class=quota for a quota-class park, reason=quota_wait).
func TestEpisodeParker_ParkEmitsQuotaWaitEvent(t *testing.T) {
	b := bus.New(nil, nil)
	sub := b.Subscribe("goal-park-events", "agent.quota_wait")
	defer b.Unsubscribe(sub)

	parker := agent.NewTurnParker(nil, func(context.Context, agent.ParkedTurnRecord) {}, time.Hour)
	parker.SetParkEventBus(b)
	park := NewEpisodeParker(parker, llm.FailurePolicyConfig{Horizon: time.Hour}, nil)

	resumeAt := time.Now().Add(time.Hour)
	if !park.park("emp-1", "emp-1", llm.FailureQuota, resumeAt, goalTurnPayload{Phase: "assess"}) {
		t.Fatal("goal episode park refused")
	}

	timer := time.NewTimer(500 * time.Millisecond)
	defer timer.Stop()
	select {
	case msg := <-sub.Channel:
		payload := map[string]any{}
		if err := json.Unmarshal(msg.Payload, &payload); err != nil {
			t.Fatalf("payload unmarshal: %v (raw %s)", err, msg.Payload)
		}
		if reason, ok := payload["reason"].(string); !ok || reason != "quota_wait" {
			t.Errorf("reason = %v, want quota_wait", payload["reason"])
		}
		if class, ok := payload["class"].(string); !ok || class != "quota" {
			t.Errorf("class = %v, want quota", payload["class"])
		}
		if agentID, ok := payload["agent_id"].(string); !ok || agentID != "emp-1" {
			t.Errorf("agent_id = %v, want emp-1", payload["agent_id"])
		}
		if to, ok := payload["to"].(string); !ok || to != "quota_wait" {
			t.Errorf("to = %v, want quota_wait", payload["to"])
		}
		if resumeAtStr, ok := payload["resume_at"].(string); !ok || resumeAtStr == "" {
			t.Errorf("resume_at = %v, want RFC3339", payload["resume_at"])
		}
	case <-timer.C:
		t.Fatal("no agent.quota_wait event delivered within 500ms")
	}
}
