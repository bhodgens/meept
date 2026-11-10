package services

import (
	"context"
	"testing"
)

// TestPushService_SetNotificationPreferences_NilGuard pins the setter
// nil-guard convention (setters_test.go): a nil prefs value is ignored —
// the live gate is never replaced by a nil pointer.
func TestPushService_SetNotificationPreferences_NilGuard(t *testing.T) {
	ps := NewPushServiceWithChannels(nil, nil, nil)

	// Nil on a fresh service: ignored, PreferencesAllowed default-allows.
	ps.SetNotificationPreferences(nil)
	if !ps.PreferencesAllowed("notification", "telegram") {
		t.Fatal("fresh service should default-allow")
	}

	// Attach a restrictive gate, then try to nil it out.
	prefs := DefaultNotificationPreferences()
	prefs.PerType = map[string]bool{"task_completed": false}
	ps.SetNotificationPreferences(&prefs)
	ps.SetNotificationPreferences(nil) // must be a no-op
	if ps.PreferencesAllowed("task_completed", "telegram") {
		t.Fatal("nil SetNotificationPreferences overwrote a live gate")
	}
	if !ps.PreferencesAllowed("notification", "telegram") {
		t.Fatal("unlisted type should still be allowed")
	}
}

// TestPushService_SetPushHistory_NilGuard pins the history setter
// nil-guard: a nil history value is ignored, and recordHistory is a
// no-op without one.
func TestPushService_SetPushHistory_NilGuard(t *testing.T) {
	ps := NewPushServiceWithChannels(nil, nil, nil)

	// No history: recording is a no-op (exercise the internal path via
	// a Push on a nil-bus service — Push errors before recording, which
	// is also the correct contract: failed pushes don't record).
	if _, err := ps.Push(context.Background(), &PushRequest{Content: "x"}); err == nil {
		t.Fatal("Push without a bus should error")
	}

	ps.SetPushHistory(nil) // must be a no-op, never a panic
	hist := NewPushHistory(4)
	ps.SetPushHistory(hist)
	ps.SetPushHistory(nil) // must NOT detach the live history
	ps.recordHistory(&PushRequest{
		SessionIDs: []string{"s1"},
		Source:     "test",
		Type:       PushTypeNotification,
		Content:    "kept",
	}, 1)
	if got := hist.QueryAll(4); len(got) != 1 || got[0].Content != "kept" {
		t.Fatalf("history after nil-set = %+v, want the recorded entry kept", got)
	}
}
