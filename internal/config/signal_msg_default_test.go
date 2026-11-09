package config

import "testing"

// Leaf 11b T4 pin: [signal].msg_per_minute default 3.
func TestSignalMsgPerMinuteDefault(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Signal.MsgPerMinute != 3 {
		t.Fatalf("Signal.MsgPerMinute = %d, want 3", cfg.Signal.MsgPerMinute)
	}
	if cfg.Signal.AllowedUUIDs != nil {
		t.Fatalf("Signal.AllowedUUIDs = %v, want nil (empty default)", cfg.Signal.AllowedUUIDs)
	}
}
