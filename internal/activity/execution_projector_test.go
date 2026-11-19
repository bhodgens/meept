package activity

import (
	"context"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/bus"
)

// TestWireExecutionProjector_PersistsBothTiers drives real bus publishes on
// bot.result and employee.result and asserts one durable row per event with
// the tier derived from the topic name (no in-payload discriminator).
func TestWireExecutionProjector_PersistsBothTiers(t *testing.T) {
	s := openTestExecutionStore(t)
	b := bus.New(nil, nil)
	defer b.Close()

	subs := WireExecutionProjector(b, s, nil)
	if len(subs) != 2 {
		t.Fatalf("subscribers = %d, want 2", len(subs))
	}
	defer func() {
		for _, sub := range subs {
			b.Unsubscribe(sub)
		}
	}()

	now := time.Now().UTC().Truncate(time.Second)
	bus.PublishT(b, agent.TopicBotResult, "bot", agent.ExecutionResultEvent{
		AgentID: "ci-bot", Status: "completed", TokensUsed: 10,
		DurationMS: 500, StopReason: agent.StopReasonEndTurn,
		TriggerRef: "cron:ci", Timestamp: now,
	})
	bus.PublishT(b, agent.TopicEmployeeResult, "employee", agent.ExecutionResultEvent{
		AgentID: "emp-1", Status: "failed", Error: "boom", TokensUsed: 3,
		DurationMS: 200, StopReason: agent.StopReasonError, Timestamp: now,
	})

	// The projector appends asynchronously; poll briefly for both rows.
	deadline := time.Now().Add(3 * time.Second)
	var runs []ExecutionRun
	for time.Now().Before(deadline) {
		var err error
		runs, _, err = s.List(context.Background(), ExecutionQuery{})
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if len(runs) == 2 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if len(runs) != 2 {
		t.Fatalf("persisted runs = %d, want 2 (projector did not drain both topics)", len(runs))
	}

	byAgent := map[string]ExecutionRun{}
	for _, r := range runs {
		byAgent[r.AgentID] = r
	}
	bot, ok := byAgent["ci-bot"]
	if !ok || bot.Tier != "bot" || bot.Status != "completed" || bot.TriggerRef != "cron:ci" {
		t.Errorf("bot row wrong: %+v (present=%v)", bot, ok)
	}
	emp, ok := byAgent["emp-1"]
	if !ok || emp.Tier != "employee" || emp.Status != "failed" || emp.Error != "boom" {
		t.Errorf("employee row wrong: %+v (present=%v)", emp, ok)
	}
}

func TestWireExecutionProjector_NilSafe(t *testing.T) {
	if subs := WireExecutionProjector(nil, nil, nil); subs != nil {
		t.Errorf("nil bus/store must return nil subs, got %d", len(subs))
	}
}
