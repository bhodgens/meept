package services

import (
	"testing"
	"time"
)

func TestNotificationPreferences_IsTypeEnabled(t *testing.T) {
	prefs := DefaultNotificationPreferences()
	
	// Empty = allow all
	if !prefs.IsTypeEnabled("test") {
		t.Error("expected true for empty config")
	}
	
	// Explicit disable
	prefs.PerType["alert"] = false
	if prefs.IsTypeEnabled("alert") {
		t.Error("expected false for explicitly disabled type")
	}
	
	// Explicit enable
	prefs.PerType["info"] = true
	if !prefs.IsTypeEnabled("info") {
		t.Error("expected true for explicitly enabled type")
	}
}

func TestNotificationPreferences_IsInDND(t *testing.T) {
	prefs := DefaultNotificationPreferences()
	
	// DND disabled = never in DND
	if prefs.IsInDND(time.Now()) {
		t.Error("expected false when DND disabled")
	}
	
	// Test overnight DND (22:00-07:00)
	prefs.DoNotDisturb.Enabled = true
	prefs.DoNotDisturb.StartTime = "22:00"
	prefs.DoNotDisturb.EndTime = "07:00"
	
	// 23:00 should be in DND
	dndTime := time.Date(2026, 1, 1, 23, 0, 0, 0, time.UTC)
	if !prefs.IsInDND(dndTime) {
		t.Error("expected true for 23:00 during overnight DND")
	}
	
	// 03:00 should be in DND
	nightTime := time.Date(2026, 1, 1, 3, 0, 0, 0, time.UTC)
	if !prefs.IsInDND(nightTime) {
		t.Error("expected true for 03:00 during overnight DND")
	}
	
	// 12:00 should NOT be in DND
	dayTime := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	if prefs.IsInDND(dayTime) {
		t.Error("expected false for 12:00 outside overnight DND")
	}
}

func TestNotificationRateLimiter(t *testing.T) {
	lim := NewNotificationRateLimiter(3) // 3 per hour
	
	// First 3 should be allowed
	for i := 0; i < 3; i++ {
		if !lim.Allow("sess-1") {
			t.Errorf("expected true for request %d", i+1)
		}
	}
	
	// 4th should be blocked
	if lim.Allow("sess-1") {
		t.Error("expected false for 4th request")
	}
	
	// Different session should be allowed
	if !lim.Allow("sess-2") {
		t.Error("expected true for different session")
	}
}

func TestNotificationRateLimiter_NoLimit(t *testing.T) {
	lim := NewNotificationRateLimiter(0) // no limit
	
	for i := 0; i < 100; i++ {
		if !lim.Allow("sess-1") {
			t.Errorf("expected true for request %d with no limit", i+1)
		}
	}
}
