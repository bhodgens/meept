package components

import (
	"strings"
	"testing"
	"time"
)

func TestNotificationManager_Push(t *testing.T) {
	nm := NewNotificationManager()

	n, cmd := nm.Push(NotifyInfo, "test title", "test message")
	if n.Title != "test title" {
		t.Errorf("expected title 'test title', got %q", n.Title)
	}
	if n.Message != "test message" {
		t.Errorf("expected message 'test message', got %q", n.Message)
	}
	if n.Level != NotifyInfo {
		t.Errorf("expected NotifyInfo level")
	}
	if cmd == nil {
		t.Error("expected expiry command to be returned")
	}
	if !nm.HasActive() {
		t.Error("expected active notifications")
	}
}

func TestNotificationManager_PushWithAction(t *testing.T) {
	nm := NewNotificationManager()

	n, _ := nm.PushWithAction(NotifySuccess, "done", "task completed", "view task")
	if n.Action != "view task" {
		t.Errorf("expected action 'view task', got %q", n.Action)
	}
}

func TestNotificationManager_Dismiss(t *testing.T) {
	nm := NewNotificationManager()

	n, _ := nm.Push(NotifyInfo, "test", "message")
	if !nm.HasActive() {
		t.Error("expected active notification")
	}

	nm.Dismiss(n.ID)
	if nm.HasActive() {
		t.Error("expected no active notifications after dismiss")
	}
}

func TestNotificationManager_DismissAll(t *testing.T) {
	nm := NewNotificationManager()

	nm.Push(NotifyInfo, "test1", "msg1")
	nm.Push(NotifySuccess, "test2", "msg2")
	nm.Push(NotifyError, "test3", "msg3")

	active := nm.Active()
	if len(active) != 3 {
		t.Errorf("expected 3 active, got %d", len(active))
	}

	nm.DismissAll()
	if nm.HasActive() {
		t.Error("expected no active notifications after dismiss all")
	}
}

func TestNotificationManager_MaxVisible(t *testing.T) {
	nm := NewNotificationManager()
	nm.maxVisible = 3

	for i := 0; i < 5; i++ {
		nm.Push(NotifyInfo, "test", "message")
	}

	active := nm.Active()
	if len(active) > 3 {
		t.Errorf("expected at most 3 active, got %d", len(active))
	}
}

func TestNotificationManager_UpdateExpired(t *testing.T) {
	nm := NewNotificationManager()

	n, _ := nm.Push(NotifyInfo, "test", "message")
	if !nm.HasActive() {
		t.Error("expected active notification")
	}

	nm.Update(NotificationExpiredMsg{ID: n.ID})
	if nm.HasActive() {
		t.Error("expected no active notifications after expiry")
	}
}

func TestNotificationManager_View(t *testing.T) {
	nm := NewNotificationManager()

	// Empty case
	view := nm.View(80)
	if view != "" {
		t.Error("expected empty view with no notifications")
	}

	// With notification
	nm.Push(NotifySuccess, "task done", "completed successfully")
	view = nm.View(80)
	if view == "" {
		t.Error("expected non-empty view with active notifications")
	}
	if !strings.Contains(view, "task done") {
		t.Error("expected title in view")
	}
}

func TestNotificationLevels(t *testing.T) {
	tests := []struct {
		level   NotificationLevel
		icon    string
		name    string
	}{
		{NotifyInfo, "i", "info"},
		{NotifySuccess, "+", "success"},
		{NotifyWarning, "!", "warning"},
		{NotifyError, "x", "error"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nm := NewNotificationManager()
			nm.Push(tt.level, "title", "message")
			view := nm.View(80)
			if !strings.Contains(view, tt.icon) {
				t.Errorf("expected icon %q in view for level %s", tt.icon, tt.name)
			}
		})
	}
}

func TestNotificationTTL(t *testing.T) {
	nm := NewNotificationManager()
	nm.defaultTTL = 100 * time.Millisecond

	n, _ := nm.Push(NotifyInfo, "test", "expires fast")
	if n.TTL != 100*time.Millisecond {
		t.Errorf("expected TTL 100ms, got %v", n.TTL)
	}
}
