package services

import (
	"context"
	"sync"
	"testing"

	"github.com/caimlas/meept/pkg/models"
)

type fakeBus struct {
	mu     sync.Mutex
	topics map[string][]*models.BusMessage
}

func newFakeBus() *fakeBus {
	return &fakeBus{topics: make(map[string][]*models.BusMessage)}
}

func (f *fakeBus) Publish(topic string, msg *models.BusMessage) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.topics[topic] = append(f.topics[topic], msg)
	return 1
}

func TestNewPushService(t *testing.T) {
	b := newFakeBus()

	s := NewPushService(b, nil)
	if s == nil {
		t.Fatal("expected non-nil PushService")
	}
	if s.logger == nil {
		t.Error("expected non-nil logger")
	}
}

func TestPush_NilRequest(t *testing.T) {
	b := newFakeBus()

	s := NewPushService(b, nil)
	_, err := s.Push(context.Background(), nil)
	if err == nil {
		t.Error("expected error for nil request")
	}
}

func TestPush_EmptyContent(t *testing.T) {
	b := newFakeBus()

	s := NewPushService(b, nil)
	_, err := s.Push(context.Background(), &PushRequest{
		Content: "",
	})
	if err == nil {
		t.Error("expected error for empty content")
	}
}

func TestPush_WithoutBus(t *testing.T) {
	s := NewPushService(nil, nil)
	_, err := s.Push(context.Background(), &PushRequest{
		Content: "hello",
	})
	if err == nil {
		t.Error("expected error for nil bus")
	}
}

func TestPush_PublishesMessage(t *testing.T) {
	b := newFakeBus()

	s := NewPushService(b, nil)

	result, err := s.Push(context.Background(), &PushRequest{
		Content:    "test notification",
		SessionIDs: []string{"sess-1"},
		Source:     "test",
		Type:       PushTypeAlert,
		Priority:   PushPriorityUrgent,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Delivered != 1 {
		t.Errorf("expected 1 delivered, got %d", result.Delivered)
	}
}

func TestPush_Defaults(t *testing.T) {
	b := newFakeBus()

	s := NewPushService(b, nil)

	result, err := s.Push(context.Background(), &PushRequest{
		Content: "test",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_ = result // defaults applied in Push method; just verify it completes
}

func TestPush_MultiSession(t *testing.T) {
	b := newFakeBus()

	s := NewPushService(b, nil)

	result, err := s.Push(context.Background(), &PushRequest{
		Content:    "broadcast",
		SessionIDs: []string{"sess-1", "sess-2", "sess-3"},
		Type:       PushTypeNotification,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Delivered != 3 {
		t.Errorf("expected 3 delivered, got %d", result.Delivered)
	}
}

func TestPush_ContextCanceled(t *testing.T) {
	b := newFakeBus()

	s := NewPushService(b, nil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	result, _ := s.Push(ctx, &PushRequest{
		Content:    "should be skipped",
		SessionIDs: []string{"sess-1"},
	})
	_ = result
	// Context is checked inside the loop after input validation.
	// With content present, validation passes, then ctx.Err() is checked.
}

func TestPushType_Values(t *testing.T) {
	for _, pt := range []PushType{PushTypeNotification, PushTypeAlert, PushTypeSummary} {
		if pt == "" {
			t.Errorf("expected non-empty PushType value, got empty")
		}
	}
}

func TestPushPriority_Values(t *testing.T) {
	for _, pp := range []PushPriority{PushPriorityLow, PushPriorityNormal, PushPriorityHigh, PushPriorityUrgent} {
		if pp == "" {
			t.Errorf("expected non-empty PushPriority value, got empty")
		}
	}
}

func TestPush_BusMessageFormat(t *testing.T) {
	b := newFakeBus()

	s := NewPushService(b, nil)

	_, err := s.Push(context.Background(), &PushRequest{
		Content:    "format check",
		SessionIDs: []string{"sess-1"},
		Type:       PushTypeSummary,
		Priority:   PushPriorityLow,
		Extra: map[string]any{
			"custom_key": "custom_value",
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify the bus received the message on a per-session topic
	b.mu.Lock()
	topics := b.topics
	b.mu.Unlock()

	// Check per-session push topic
	sessMsgs, ok := topics["push.sess-1"]
	if !ok || len(sessMsgs) == 0 {
		t.Fatal("expected push.sess-1 message on bus")
	}

	for _, m := range sessMsgs {
		if m.Type != models.MessageTypeEvent {
			t.Errorf("expected MessageTypeEvent, got %s", m.Type)
		}
		if m.Source != "svc.push" {
			t.Errorf("expected source 'svc.push', got %s", m.Source)
		}
		if len(m.Payload) == 0 {
			t.Error("expected non-empty payload")
		}
	}
}

func TestPush_PerSessionTopic(t *testing.T) {
	b := newFakeBus()

	s := NewPushService(b, nil)

	_, err := s.Push(context.Background(), &PushRequest{
		Content:    "per session test",
		SessionIDs: []string{"sess-1", "sess-2"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	b.mu.Lock()
	topics := b.topics
	b.mu.Unlock()

	if len(topics) != 2 {
		t.Errorf("expected 2 topics published, got %d", len(topics))
	}
	for _, topic := range []string{"push.sess-1", "push.sess-2"} {
		if _, ok := topics[topic]; !ok {
			t.Errorf("expected message on topic %q", topic)
		}
	}
}
