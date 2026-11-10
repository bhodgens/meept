package bot

import (
	"context"
	"testing"
)

// TestNotificationBot_SetTelegram_NilGuard pins the setter nil-guard
// convention (setters_test.go): nil receiver and nil telegram value are
// both safe; the bot keeps logging-only delivery.
func TestNotificationBot_SetTelegram_NilGuard(t *testing.T) {
	// Nil receiver: no panic.
	var nb *NotificationBot
	nb.SetTelegram(nil)

	// Nil value on a live bot: no panic, no change in construction state.
	constructed := NewNotificationBot(NotificationBotConfig{Enabled: true}, nil, nil)
	if constructed == nil {
		t.Fatal("NewNotificationBot returned nil")
	}
	constructed.SetTelegram(nil) // must be a no-op
}

// TestNewNotificationBot_NilLoggerAndEmitter pins the constructor
// contract: nil logger falls back to slog.Default and a nil emitter puts
// the bot in passive mode (Start returns without a goroutine, no panic).
func TestNewNotificationBot_NilLoggerAndEmitter(t *testing.T) {
	nb := NewNotificationBot(NotificationBotConfig{Enabled: true}, nil, nil)
	nb.Start(context.Background())
	nb.Stop()
}
