package services

import (
	"context"
	"log/slog"
	"testing"

	"github.com/caimlas/meept/internal/bus"
)

func testBus() *bus.MessageBus {
	return bus.New(&bus.Config{
		BufferSize: 100,
	}, slog.New(slog.DiscardHandler))
}

func TestNewPushServiceWithChannels_BusOnly(t *testing.T) {
	b := testBus()
	defer b.Close()

	s := NewPushServiceWithChannels(b, nil, nil)
	if s == nil {
		t.Fatal("expected non-nil PushService")
	}
	if s.logger == nil {
		t.Error("expected non-nil logger")
	}
}

func TestPush_NilRequest(t *testing.T) {
	b := testBus()
	defer b.Close()

	s := NewPushServiceWithChannels(b, nil, nil)
	_, err := s.Push(context.Background(), nil)
	if err == nil {
		t.Error("expected error for nil request")
	}
}

func TestPush_EmptyContent(t *testing.T) {
	b := testBus()
	defer b.Close()

	s := NewPushServiceWithChannels(b, nil, nil)
	_, err := s.Push(context.Background(), &PushRequest{
		Content: "",
	})
	if err == nil {
		t.Error("expected error for empty content")
	}
}

func TestPush_WithoutBus(t *testing.T) {
	s := NewPushServiceWithChannels(nil, nil, nil)
	_, err := s.Push(context.Background(), &PushRequest{
		Content: "hello",
	})
	if err == nil {
		t.Error("expected error for nil bus")
	}
}

func TestPush_PublishesMessage(t *testing.T) {
	b := testBus()
	defer b.Close()

	s := NewPushServiceWithChannels(b, nil, nil)

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
	b := testBus()
	defer b.Close()

	s := NewPushServiceWithChannels(b, nil, nil)

	result, err := s.Push(context.Background(), &PushRequest{
		Content: "test",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_ = result
}

func TestPush_MultiSession(t *testing.T) {
	b := testBus()
	defer b.Close()

	s := NewPushServiceWithChannels(b, nil, nil)

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
	b := testBus()
	defer b.Close()

	s := NewPushServiceWithChannels(b, nil, nil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, _ = s.Push(ctx, &PushRequest{
		Content:    "should be skipped",
		SessionIDs: []string{"sess-1"},
	})
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
