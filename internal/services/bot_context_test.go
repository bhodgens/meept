package services

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/caimlas/meept/internal/bus"
)

func TestBotContext_PushNotification(t *testing.T) {
	bus := bus.New(&bus.Config{BufferSize: 100}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	defer bus.Close()

	pushSvc := NewPushService(nil, bus, nil)
	ctx := NewBotContext(pushSvc, nil)

	if ctx == nil {
		t.Fatal("expected non-nil bot context")
	}

	err := ctx.PushNotification(context.Background(), "sess-1", "Test", "Hello")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestBotContext_NilPushService(t *testing.T) {
	ctx := NewBotContext(nil, nil)
	if ctx == nil {
		t.Fatal("expected non-nil bot context")
	}

	// Should not panic when push service is nil
	err := ctx.PushNotification(context.Background(), "sess-1", "Test", "Hello")
	if err != nil {
		t.Errorf("expected no error with nil push service, got: %v", err)
	}
}
