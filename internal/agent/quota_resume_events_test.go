package agent

// Tree 03 leaf 04 gap coverage: the quota watcher must forward the park-event
// bus to its inner TurnParker so handler-side quota parks (leaf 06 deferral
// path, handler.go resumeQuotaParkedTurn's Park call) emit on the EXISTING
// agent.quota_wait topic with reason "quota_wait" + class "quota" — the same
// event the loop's throttle parks and the employee goal-loop parks emit.
// Without the forwarding, chat-side quota parks are invisible to the TUI/GUI
// agents-tab wait labels.

import (
	"context"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
)

// TestQuotaResumeWatcher_SetParkEventBusForwards: Park on the watcher emits
// the quota-class park event through the inner parker (reason "quota_wait",
// class "quota"), and a nil bus is refused cleanly (parking keeps working).
func TestQuotaResumeWatcher_SetParkEventBusForwards(t *testing.T) {
	b := bus.New(nil, testLogger())
	spy := newParkEventSpy(b)
	defer spy.stop(b)

	w := NewQuotaResumeWatcher(testLogger(), func(context.Context, QuotaParkedTurn) {}, 24*time.Hour)
	w.SetParkEventBus(b)

	if !w.Park(QuotaParkedTurn{
		SessionID:      "sess-q-forward",
		ConversationID: "conv-q-forward",
		Message:        "hi",
		AgentID:        "agent-fwd",
		ProviderID:     "pq",
		UnblockAt:      time.Now().Add(time.Hour),
	}) {
		t.Fatal("quota watcher Park refused")
	}

	events := spy.collect(t, 500*time.Millisecond)
	if len(events) != 1 {
		t.Fatalf("park events = %d, want 1 (%+v)", len(events), events)
	}
	ev := events[0]
	if reason, ok := ev["reason"].(string); !ok || reason != ReasonQuotaWait {
		t.Errorf("reason = %v, want %q", ev["reason"], ReasonQuotaWait)
	}
	if class, ok := ev["class"].(string); !ok || class != "quota" {
		t.Errorf("class = %v, want \"quota\"", ev["class"])
	}
	if to, ok := ev["to"].(string); !ok || to != "quota_wait" {
		t.Errorf("to = %v, want quota_wait", ev["to"])
	}
	if sessionID, ok := ev["session_id"].(string); !ok || sessionID != "sess-q-forward" {
		t.Errorf("session_id = %v, want sess-q-forward", ev["session_id"])
	}

	// Nil bus: refused, parking unaffected (the nil-safe default).
	w.SetParkEventBus(nil)
	if !w.Park(QuotaParkedTurn{
		SessionID:      "sess-q-nil",
		ConversationID: "conv-q-nil",
		Message:        "hi",
		ProviderID:     "pq",
		UnblockAt:      time.Now().Add(time.Hour),
	}) {
		t.Fatal("Park refused after nil SetParkEventBus")
	}
	if got := w.Pending(); got != 2 {
		t.Errorf("Pending = %d, want 2", got)
	}

	// Nil receiver is safe.
	var nilWatcher *QuotaResumeWatcher
	nilWatcher.SetParkEventBus(b) // must not panic
}

// TestQuotaResumeWatcher_ParkEventQuotaClassOnTopic pins the topic: the
// forwarded park event rides agent.quota_wait (never a new topic), so the
// WS agent.quota prefix classification covers chat-side quota parks too.
func TestQuotaResumeWatcher_ParkEventQuotaClassOnTopic(t *testing.T) {
	b := bus.New(nil, testLogger())
	defer b.Close()
	sub := b.Subscribe("topic-pin", "agent.quota_wait")
	defer b.Unsubscribe(sub)

	w := NewQuotaResumeWatcher(testLogger(), func(context.Context, QuotaParkedTurn) {}, 24*time.Hour)
	w.SetParkEventBus(b)
	if !w.Park(QuotaParkedTurn{
		SessionID:      "s",
		ConversationID: "c",
		Message:        "hi",
		ProviderID:     "pq",
		UnblockAt:      time.Now().Add(time.Hour),
	}) {
		t.Fatal("Park refused")
	}

	select {
	case msg := <-sub.Channel:
		if msg.Topic != "agent.quota_wait" {
			t.Errorf("topic = %q, want agent.quota_wait", msg.Topic)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("no park event on agent.quota_wait within deadline")
	}
}
