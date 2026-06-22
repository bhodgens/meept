package session

import (
	"sync"
	"testing"
	"time"
)

func TestActivityTracker_RecordActivity(t *testing.T) {
	tr := NewActivityTracker()

	tr.RecordActivity("session-1", "client-a")

	if !tr.HasRecentActivity("session-1", 5*time.Second) {
		t.Error("expected session-1 to have recent activity")
	}

	if tr.HasRecentActivity("session-2", 5*time.Second) {
		t.Error("expected HasRecentActivity to return false for unknown session")
	}
}

func TestActivityTracker_ActiveSessions(t *testing.T) {
	tr := NewActivityTracker()

	tr.RecordActivity("session-1", "client-a")
	tr.RecordActivity("session-2", "client-b")

	active := tr.GetActiveSessions(5 * time.Second)
	if len(active) != 2 {
		t.Fatalf("expected 2 active sessions, got %d", len(active))
	}
}

func TestActivityTracker_TimeWindow(t *testing.T) {
	tr := NewActivityTracker()

	// Record activity, then wait past the window
	tr.RecordActivity("session-1", "client-a")
	// Use a zero-duration window to verify expiry (time.Now() was already called
	// inside RecordActivity, so 0ns window means "expired").
	time.Sleep(10 * time.Millisecond)

	// Zero-duration window should never match since some time passed
	if tr.HasRecentActivity("session-1", 0) {
		t.Error("expected session-1 to have no recent activity with 0 duration")
	}

	active := tr.GetActiveSessions(0)
	if len(active) != 0 {
		t.Errorf("expected 0 active sessions with 0 duration, got %d", len(active))
	}
}

func TestActivityTracker_ClientIDTracking(t *testing.T) {
	tr := NewActivityTracker()

	tr.RecordActivity("session-1", "client-a")
	tr.RecordActivity("session-1", "client-b")

	tr.mu.RLock()
	state := tr.activity["session-1"]
	tr.mu.RUnlock()

	if state.ClientID != "client-b" {
		t.Errorf("expected clientID to be updated to client-b, got %s", state.ClientID)
	}
}

func TestActivityTracker_Concurrent(t *testing.T) {
	tr := NewActivityTracker()
	var wg sync.WaitGroup
	n := 100
	wg.Add(n * 3)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			tr.RecordActivity("session", "client")
		}()
		go func() {
			defer wg.Done()
			tr.HasRecentActivity("session", time.Minute)
		}()
		go func() {
			defer wg.Done()
			tr.GetActiveSessions(time.Minute)
		}()
	}
	wg.Wait()
}

func TestActivityTracker_Consistency(t *testing.T) {
	tr := NewActivityTracker()
	for i := 0; i < 100; i++ {
		tr.RecordActivity("session", "client")
		_ = tr.GetActiveSessions(time.Minute)
		_ = tr.HasRecentActivity("session", time.Minute)
	}
}
