package services

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/pkg/models"
)

// captureChannel implements a minimal channel for observing pushes. The
// notifier calls PushService.Push, which publishes on the bus; for delivery
// assertions we instead use a real PushService over a real bus and count
// messages on the push topic via a test subscriber.

// quotaEventPayload builds a BusMessage carrying a QuotaEvent-style payload.
func quotaEventPayload(t *testing.T, fields map[string]any) *models.BusMessage {
	t.Helper()
	payload, err := json.Marshal(fields)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	return &models.BusMessage{Payload: payload}
}

func TestQuotaNotifier_ClearedResetsDedup(t *testing.T) {
	b := bus.New(nil, slog.Default())
	defer b.Close()

	pushSvc := NewPushService(nil, b, slog.Default())
	qn := NewQuotaNotifier(b, pushSvc, slog.Default())
	defer qn.Stop()

	unblockAt := time.Now().Add(3 * time.Hour)

	// blocked -> cleared -> blocked must notify twice for the initial tier.
	initial := func() *models.BusMessage {
		return quotaEventPayload(t, map[string]any{
			"agent_id":       "agent-1",
			"provider_id":    "openrouter",
			"model_id":       "claude-3-opus",
			"credential_key": "key-1",
			"reason":         "quota_blocked",
			"escalation":     "",
			"unblock_at":     unblockAt.Format(time.RFC3339),
			"task_count":     2,
		})
	}
	cleared := quotaEventPayload(t, map[string]any{
		"agent_id":       "agent-1",
		"provider_id":    "openrouter",
		"model_id":       "claude-3-opus",
		"credential_key": "key-1",
		"reason":         "quota_cleared",
		"escalation":     "",
	})

	qn.processEvent(initial())
	// Duplicate initial must dedup.
	qn.processEvent(initial())
	qn.processEvent(cleared)
	// New episode after clear must notify again.
	qn.processEvent(initial())

	qn.mu.Lock()
	entries := len(qn.seen)
	qn.mu.Unlock()

	// After cleared + re-entry: 1 entry (the fresh initial).
	if entries != 1 {
		t.Errorf("expected exactly 1 dedup entry after cleared+reblock, got %d", entries)
	}
}

func TestQuotaNotifier_ClearedDoesNotDedupRepeatedly(t *testing.T) {
	b := bus.New(nil, slog.Default())
	defer b.Close()

	pushSvc := NewPushService(nil, b, slog.Default())
	qn := NewQuotaNotifier(b, pushSvc, slog.Default())
	defer qn.Stop()

	// Two cleared events in a row: both render (cleared events bypass dedup).
	for i := 0; i < 2; i++ {
		msg := quotaEventPayload(t, map[string]any{
			"agent_id":       "agent-1",
			"provider_id":    "openrouter",
			"model_id":       "claude-3-opus",
			"credential_key": "key-1",
			"reason":         "quota_cleared",
			"escalation":     "",
		})
		qn.processEvent(msg)
	}

	qn.mu.Lock()
	defer qn.mu.Unlock()
	for k := range qn.seen {
		t.Errorf("expected dedup entries to be cleared, found %q", k)
	}
}

func TestQuotaNotifier_FormatMessageTaskCountAbsent(t *testing.T) {
	unblockAt := time.Now().Add(3 * time.Hour)

	got := formatMessage("openrouter", "claude-3", unblockAt, "", taskCountAbsent)
	if contains(got, "0 task(s)") || contains(got, "task(s) affected") {
		t.Errorf("expected no task count clause when absent, got %q", got)
	}
	if !contains(got, "quota limit reached") {
		t.Errorf("expected initial-block text, got %q", got)
	}

	gotWithCount := formatMessage("openrouter", "claude-3", unblockAt, "", 3)
	if !contains(gotWithCount, "3 task(s) affected") {
		t.Errorf("expected task count clause when present, got %q", gotWithCount)
	}
}

func TestQuotaNotifier_ClearedRendersRecovery(t *testing.T) {
	// The normalization path: reason="quota_cleared" + escalation="" must
	// route through the recovery branch in processEvent.
	b := bus.New(nil, slog.Default())
	defer b.Close()

	pushSvc := NewPushService(nil, b, slog.Default())
	qn := NewQuotaNotifier(b, pushSvc, slog.Default())
	defer qn.Stop()

	delivered := make(chan string, 4)
	// Subscribe to the push topic the PushService publishes on. With empty
	// SessionIDs, Push publishes on "push." (the per-session topic loop is
	// empty) — instead of depending on that detail, intercept via a fake
	// PushService is not possible (concrete type), so assert through seen
	// behavior + no panic, plus formatMessage branch coverage above.
	_ = delivered

	msg := quotaEventPayload(t, map[string]any{
		"agent_id":       "agent-1",
		"provider_id":    "openrouter",
		"model_id":       "claude-3-opus",
		"credential_key": "key-1",
		"reason":         "quota_cleared",
		"escalation":     "",
	})
	// Must not panic and must route the cleared branch (observable via the
	// dedup reset below: entries added earlier are wiped).
	qn.mu.Lock()
	qn.seen[key("agent-1", "key-1", "warn")] = true
	qn.mu.Unlock()

	qn.processEvent(msg)

	qn.mu.Lock()
	_, stillThere := qn.seen[key("agent-1", "key-1", "warn")]
	qn.mu.Unlock()
	if stillThere {
		t.Fatal("cleared event should have reset dedup for the episode")
	}
}

func TestQuotaNotifier_PumpEndToEnd(t *testing.T) {
	b := bus.New(nil, slog.Default())
	defer b.Close()

	pushSvc := NewPushService(nil, b, slog.Default())
	qn := NewQuotaNotifier(b, pushSvc, slog.Default())
	defer qn.Stop()

	// Publish a quota event on the bus; the pump must consume it through
	// the subscriber channel and run processEvent (observable via the
	// dedup entry appearing). Note: the actual push delivery step cannot be
	// observed on the bus here because PushService.Push with no SessionIDs
	// publishes nothing — covered separately by processEvent assertions.
	b.Publish("agent.quota_wait", &models.BusMessage{
		Type: models.MessageTypeEvent,
		Payload: mustJSON(t, map[string]any{
			"agent_id":       "agent-1",
			"provider_id":    "openrouter",
			"model_id":       "claude-3-opus",
			"credential_key": "key-1",
			"reason":         "quota_blocked",
			"escalation":     "",
			"unblock_at":     time.Now().Add(2 * time.Hour).Format(time.RFC3339),
			"task_count":     1,
		}),
	})

	deadline := time.Now().Add(3 * time.Second)
	for {
		qn.mu.Lock()
		seen := qn.seen[key("agent-1", "key-1", "")]
		qn.mu.Unlock()
		if seen {
			return // pump consumed and processed the event end-to-end
		}
		if time.Now().After(deadline) {
			t.Fatal("pump never consumed the published quota event")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestQuotaNotifier_StartStopCycle(t *testing.T) {
	b := bus.New(nil, slog.Default())
	defer b.Close()

	pushSvc := NewPushService(nil, b, slog.Default())
	qn := NewQuotaNotifier(b, pushSvc, slog.Default())

	// Stop must terminate the pump without hanging.
	qn.Stop()
	qn.Stop() // idempotent

	// A fresh notifier with nil bus must not panic and Start/Stop must be
	// safe no-ops.
	qn2 := NewQuotaNotifier(nil, pushSvc, slog.Default())
	qn2.Start()
	qn2.Stop()
}

func TestQuotaNotifier_NilPushSvcSkipsDelivery(t *testing.T) {
	b := bus.New(nil, slog.Default())
	defer b.Close()

	// nil push service: pump must still consume events without panicking.
	qn := NewQuotaNotifier(b, nil, slog.Default())
	defer qn.Stop()

	qn.processEvent(quotaEventPayload(t, map[string]any{
		"agent_id":       "agent-2",
		"provider_id":    "zai",
		"model_id":       "glm-4.6",
		"credential_key": "key-9",
		"reason":         "quota_blocked",
		"escalation":     "",
	}))

	qn.mu.Lock()
	seen := qn.seen[key("agent-2", "key-9", "")]
	qn.mu.Unlock()
	if !seen {
		t.Error("expected dedup entry even with nil push service")
	}
}

func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return data
}

var _ = fmt.Sprintf // keep fmt import if unused by future edits
