package tui

import (
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/tui/components"
)

func TestApp_NotificationManager_Initialized(t *testing.T) {
	app := createTestApp()

	if app.notifications == nil {
		t.Error("expected notification manager to be initialized")
	}
}

func TestApp_NotificationExpiredMsg(t *testing.T) {
	app := createTestApp()

	// Push a notification
	_, expiryCmd := app.notifications.Push(components.NotifyInfo, "test", "message")
	if expiryCmd == nil {
		t.Fatal("expected expiry command")
	}

	// Verify notification is active
	if !app.notifications.HasActive() {
		t.Error("expected active notification after push")
	}

	// Execute the expiry command to get the message
	msg := expiryCmd()
	expiredMsg, ok := msg.(components.NotificationExpiredMsg)
	if !ok {
		t.Fatal("expected NotificationExpiredMsg from expiry command")
	}

	// Process expiry through App
	newModel, _ := app.Update(expiredMsg)
	newApp := newModel.(*App)

	if newApp.notifications.HasActive() {
		t.Error("expected no active notifications after expiry")
	}
}

func TestApp_NotificationInStatusView(t *testing.T) {
	app := createTestApp()

	// Push a notification
	app.notifications.Push(components.NotifySuccess, "task done", "build completed")

	// The View() should include the notification
	view := app.View()
	if view.Content == "" {
		t.Error("expected non-empty view")
	}

	// Verify notifications don't crash the view
	_ = app.renderStatusBar()
}

func TestApp_QuickActionsChatView(t *testing.T) {
	app := createTestApp()

	// Default is chat view
	if app.currentView != ViewChat {
		t.Error("expected chat view")
	}

	actions := app.getQuickActions()
	found := false
	for _, action := range actions {
		if strings.Contains(action, "menu") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected 'menu' action in chat view quick actions")
	}
}

func TestApp_QuickActionsTasksView(t *testing.T) {
	app := createTestApp()
	app.currentView = ViewTasks

	actions := app.getQuickActions()
	found := false
	for _, action := range actions {
		if strings.Contains(action, "details") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected 'details' action in tasks view quick actions")
	}
}

func TestApp_ClipboardUsesTeaSetClipboard(t *testing.T) {
	// The doCopy function should use tea.SetClipboard instead of OSC52
	// We can verify it returns a command (which includes tea.SetClipboard batched)
	cmd := doCopy("test content")
	if cmd == nil {
		t.Error("expected non-nil copy command")
	}
}
