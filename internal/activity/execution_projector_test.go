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

	proj := WireExecutionProjector(b, s, nil)
	if proj == nil {
		t.Fatal("WireExecutionProjector returned nil for a live bus/store")
	}
	defer proj.Stop(b)

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
	if proj := WireExecutionProjector(nil, nil, nil); proj != nil {
		t.Errorf("nil bus/store must return nil projector, got %+v", proj)
	}
	// Stop is nil-safe (the daemon stop path calls it unconditionally).
	var zero *ExecutionProjector
	zero.Stop(nil)
}

// TestExecutionProjector_StopDrainsBeforeClose pins the issue #160 M5 fix:
// events published immediately before the shutdown seam must ALL persist.
// The old teardown unsubscribed and closed the store without waiting for
// the reader goroutines to drain their buffered items, so a buffered
// Append raced store.Close and the row was lost with only a Warn. The
// projector's Stop waits for the readers, so the daemon's
// stop-projector-then-close-store ordering is loss-free.
func TestExecutionProjector_StopDrainsBeforeClose(t *testing.T) {
	const n = 150 // above the bus default buffer (100): forces a real backlog

	path := t.TempDir() + "/drain.db"
	s, err := OpenExecutionStore(path, nil)
	if err != nil {
		t.Fatalf("OpenExecutionStore: %v", err)
	}
	b := bus.New(nil, nil)
	defer b.Close()

	proj := WireExecutionProjector(b, s, nil)
	if proj == nil {
		t.Fatal("nil projector")
	}

	// PublishBlocking guarantees channel delivery (no bus-layer drop), so
	// when the last call returns every event is either appended already or
	// sitting in the projector's buffered channel awaiting drain - the
	// exact M5 window.
	now := time.Now().UTC().Truncate(time.Second)
	for i := 0; i < n; i++ {
		bus.PublishBlockingT(b, agent.TopicBotResult, "bot", agent.ExecutionResultEvent{
			AgentID: "drain-bot", Status: "completed", TokensUsed: i,
			DurationMS: 1, Timestamp: now,
		})
	}

	// Immediate shutdown seam: Stop (unsubscribe + drain), then Close -
	// exactly the daemon's stopActivityRing ordering.
	proj.Stop(b)
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Count on a fresh handle (the old one is closed, as after shutdown).
	s2, err := OpenExecutionStore(path, nil)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s2.Close()
	_, total, err := s2.List(context.Background(), ExecutionQuery{Limit: 1})
	if err != nil {
		t.Fatalf("List after reopen: %v", err)
	}
	if total != n {
		t.Fatalf("M5 regression: %d of %d published runs survived the Stop+Close seam (lost=%d)",
			total, n, n-total)
	}
}

// TestExecutionProjector_StopIsIdempotent pins the teardown guard: calling
// Stop twice (or after the bus closed) must not panic or double-unsubscribe.
func TestExecutionProjector_StopIsIdempotent(t *testing.T) {
	s := openTestExecutionStore(t)
	b := bus.New(nil, nil)
	proj := WireExecutionProjector(b, s, nil)
	proj.Stop(b)
	proj.Stop(b) // second call: no-op, must not panic
	b.Close()
	proj.Stop(b) // after bus close: still safe
}
