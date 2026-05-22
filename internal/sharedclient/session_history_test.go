package sharedclient

import (
	"testing"
)

func TestSessionHistory(t *testing.T) {
	sh := NewSessionHistory(10)

	// Test adding to different sessions
	sh.Add("session1", "cmd1")
	sh.Add("session1", "cmd2")
	sh.Add("session2", "other-cmd")

	// Verify session1 has its own history
	entries1 := sh.GetEntries("session1")
	if len(entries1) != 2 {
		t.Errorf("SessionHistory.GetEntries(session1) len=%d, want 2", len(entries1))
	}

	// Verify session2 has separate history
	entries2 := sh.GetEntries("session2")
	if len(entries2) != 1 {
		t.Errorf("SessionHistory.GetEntries(session2) len=%d, want 1", len(entries2))
	}
	if entries2[0] != "other-cmd" {
		t.Errorf("SessionHistory.GetEntries(session2)[0] = %q, want \"other-cmd\"", entries2[0])
	}
}

func TestSessionHistoryUp(t *testing.T) {
	sh := NewSessionHistory(10)
	sh.Add("s1", "first")
	sh.Add("s1", "second")

	// Navigate up
	got, ok := sh.Up("s1", "current")
	if !ok || got != "second" {
		t.Errorf("SessionHistory.Up() = %q (ok=%v), want \"second\" (ok=true)", got, ok)
	}

	got, ok = sh.Up("s1", "current")
	if !ok || got != "first" {
		t.Errorf("SessionHistory.Up() = %q (ok=%v), want \"first\" (ok=true)", got, ok)
	}
}

func TestSessionHistoryDown(t *testing.T) {
	sh := NewSessionHistory(10)
	sh.Add("s1", "first")
	sh.Add("s1", "second")

	// Navigate up twice (stores "temp" as temporary)
	sh.Up("s1", "temp") // at "second"
	sh.Up("s1", "temp") // at "first"

	// Navigate down once - should return "second"
	got, ok := sh.Down("s1", "temp")
	if !ok || got != "second" {
		t.Errorf("SessionHistory.Down() step1 = %q (ok=%v), want \"second\" (ok=true)", got, ok)
	}

	// Navigate down again - should return temporary ("temp")
	got, ok = sh.Down("s1", "temp")
	if !ok || got != "temp" {
		t.Errorf("SessionHistory.Down() step2 = %q (ok=%v), want \"temp\" (ok=true)", got, ok)
	}
}

func TestSessionHistoryClearSession(t *testing.T) {
	sh := NewSessionHistory(10)
	sh.Add("s1", "cmd1")
	sh.Add("s2", "cmd2")

	sh.ClearSession("s1")

	entries1 := sh.GetEntries("s1")
	if len(entries1) != 0 {
		t.Errorf("SessionHistory.ClearSession() len(entries1)=%d, want 0", len(entries1))
	}

	// s2 should be unaffected
	entries2 := sh.GetEntries("s2")
	if len(entries2) != 1 {
		t.Errorf("SessionHistory.ClearSession() affected s2: len=%d, want 1", len(entries2))
	}
}

func TestSessionHistoryClearAll(t *testing.T) {
	sh := NewSessionHistory(10)
	sh.Add("s1", "cmd1")
	sh.Add("s2", "cmd2")

	sh.ClearAll()

	entries1 := sh.GetEntries("s1")
	entries2 := sh.GetEntries("s2")

	if len(entries1) != 0 || len(entries2) != 0 {
		t.Errorf("SessionHistory.ClearAll() failed: s1=%d entries, s2=%d entries", len(entries1), len(entries2))
	}
}

func TestSessionHistoryReset(t *testing.T) {
	sh := NewSessionHistory(10)
	sh.Add("s1", "first")
	sh.Add("s1", "second")

	// Navigate up to change state
	sh.Up("s1", "temp")

	// Reset navigation
	sh.Reset("s1")

	// After reset, current=-1, temporary=""
	// HasPrevious should be true since we have entries and current < len-1
	hasPrev := sh.HasPrevious("s1")
	if !hasPrev {
		t.Error("SessionHistory.Reset() should have HasPrevious=true with entries")
	}
}

func TestSessionHistoryHasPrevious(t *testing.T) {
	sh := NewSessionHistory(10)
	sh.Add("s1", "cmd")

	// With entries and current=-1, HasPrevious should be true
	if !sh.HasPrevious("s1") {
		t.Error("SessionHistory.HasPrevious() should be true with entries")
	}
}

func TestSessionHistoryHasNext(t *testing.T) {
	sh := NewSessionHistory(10)
	sh.Add("s1", "cmd")

	// At start (current=-1), HasNext should be false
	if sh.HasNext("s1") {
		t.Error("SessionHistory.HasNext() should be false at start")
	}

	// Navigate up
	sh.Up("s1", "temp")

	// After navigating up (current>=0), HasNext should be true
	if !sh.HasNext("s1") {
		t.Error("SessionHistory.HasNext() should be true after navigating up")
	}
}
