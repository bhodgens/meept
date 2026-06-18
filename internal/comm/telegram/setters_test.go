package telegram

import (
	"testing"
)

// TestAllSetters_NilSafe verifies that every Set* method on telegram-package
// structs that accepts a pointer, interface, slice, map, or func argument is
// nil-safe. See CLAUDE.md "Setter methods" coding practice.
func TestAllSetters_NilSafe(t *testing.T) {
	// Bot.SetResetter only assigns the field; a zero-value Bot is sufficient.
	// NewBot requires a non-empty token + handler, which is unnecessary here.
	bot := &Bot{}

	tests := []struct {
		name    string
		setFunc func()
	}{
		// Bot setters (internal/comm/telegram/bot.go)
		{"Bot.SetResetter", func() { bot.SetResetter(nil) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("Set method panicked on nil: %v", r)
				}
			}()
			tt.setFunc()
		})
	}
}
