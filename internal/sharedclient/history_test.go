package sharedclient

import (
	"testing"
)

func TestNewHistory(t *testing.T) {
	h := NewHistory(100)
	if h == nil {
		t.Fatal("NewHistory() returned nil")
	}
	if h.maxSize != 100 {
		t.Errorf("NewHistory(100).maxSize = %d, want 100", h.maxSize)
	}
	if h.current != -1 {
		t.Errorf("NewHistory(100).current = %d, want -1", h.current)
	}
	if len(h.entries) != 0 {
		t.Errorf("NewHistory(100).entries = %v, want empty", h.entries)
	}
}

func TestHistoryAdd(t *testing.T) {
	h := NewHistory(10)

	h.Add("first")
	h.Add("second")
	h.Add("third")

	if h.Len() != 3 {
		t.Errorf("History.Len() = %d, want 3", h.Len())
	}

	// Test consecutive duplicate prevention
	h.Add("third")
	if h.Len() != 3 {
		t.Errorf("History.Add() duplicate not prevented: Len() = %d, want 3", h.Len())
	}

	// Test non-consecutive duplicate is allowed
	h.Add("first")
	if h.Len() != 4 {
		t.Errorf("History.Add() non-consecutive duplicate wrong: Len() = %d, want 4", h.Len())
	}
}

func TestHistoryAddEmpty(t *testing.T) {
	h := NewHistory(10)
	h.Add("")
	if h.Len() != 0 {
		t.Errorf("History.Add(\"\") should not add entry: Len() = %d, want 0", h.Len())
	}
}

func TestHistoryUp(t *testing.T) {
	h := NewHistory(10)
	h.Add("first")
	h.Add("second")
	h.Add("third")

	// First up should return "third" (most recent)
	got, ok := h.Up("current-input")
	if !ok {
		t.Fatal("History.Up() returned false, want true")
	}
	if got != "third" {
		t.Errorf("History.Up() = %q, want \"third\"", got)
	}

	// Second up should return "second"
	got, ok = h.Up("current-input")
	if !ok || got != "second" {
		t.Errorf("History.Up() = %q (ok=%v), want \"second\" (ok=true)", got, ok)
	}

	// Third up should return "first"
	got, ok = h.Up("current-input")
	if !ok || got != "first" {
		t.Errorf("History.Up() = %q (ok=%v), want \"first\" (ok=true)", got, ok)
	}

	// Fourth up should still return "first" but with ok=false (at oldest)
	got, ok = h.Up("current-input")
	if ok {
		t.Errorf("History.Up() at oldest returned ok=true, want false (already at oldest)")
	}
	if got != "first" {
		t.Errorf("History.Up() at oldest = %q, want \"first\"", got)
	}
}

func TestHistoryUpEmpty(t *testing.T) {
	h := NewHistory(10)
	_, ok := h.Up("input")
	if ok {
		t.Error("History.Up() on empty history returned true, want false")
	}
}

func TestHistoryDown(t *testing.T) {
	h := NewHistory(10)
	h.Add("first")
	h.Add("second")
	h.Add("third")

	// Navigate up first - this stores "input" as temporary
	h.Up("input") // at "third", temporary="input"
	h.Up("input") // at "second"

	// Down should return "third"
	got, ok := h.Down("ignored") // temporary already set, parameter ignored
	if !ok || got != "third" {
		t.Errorf("History.Down() = %q (ok=%v), want \"third\" (ok=true)", got, ok)
	}

	// Down should return the temporary stored on first Up()
	got, ok = h.Down("ignored")
	if !ok || got != "input" {
		t.Errorf("History.Down() at end = %q (ok=%v), want \"input\" (ok=true)", got, ok)
	}

	// Further down should return false (at end)
	_, ok = h.Down("ignored")
	if ok {
		t.Error("History.Down() past end returned true, want false")
	}
}

func TestHistoryDownFromStart(t *testing.T) {
	h := NewHistory(10)
	h.Add("first")
	h.Add("second")

	// Down from start (current=-1) should return false
	_, ok := h.Down("input")
	if ok {
		t.Error("History.Down() from start returned true, want false")
	}
}

func TestHistoryReset(t *testing.T) {
	h := NewHistory(10)
	h.Add("first")
	h.Add("second")
	h.Up("input") // Navigate up
	h.Up("input")

	h.Reset()

	if h.current != -1 {
		t.Errorf("History.Reset().current = %d, want -1", h.current)
	}
	if h.temporary != "" {
		t.Errorf("History.Reset().temporary = %q, want \"\"", h.temporary)
	}
	if h.Len() != 2 {
		t.Errorf("History.Reset() cleared entries: Len() = %d, want 2", h.Len())
	}
}

func TestHistoryMaxSize(t *testing.T) {
	h := NewHistory(5)

	// Add more than max size
	for i := 0; i < 10; i++ {
		h.Add(string(rune('a' + i)))
	}

	if h.Len() != 5 {
		t.Errorf("History exceeded maxSize: Len() = %d, want 5", h.Len())
	}

	// Should have the most recent 5 entries
	entries := h.Entries()
	expected := []string{"f", "g", "h", "i", "j"}
	for i, want := range expected {
		if i < len(entries) && entries[i] != want {
			t.Errorf("History.Entries()[%d] = %q, want %q", i, entries[i], want)
		}
	}
}

func TestHistoryHasPrevious(t *testing.T) {
	h := NewHistory(10)

	if h.HasPrevious() {
		t.Error("History.HasPrevious() on empty returned true, want false")
	}

	h.Add("first")
	if !h.HasPrevious() {
		t.Error("History.HasPrevious() with entries returned false, want true")
	}
}

func TestHistoryHasNext(t *testing.T) {
	h := NewHistory(10)
	h.Add("first")
	h.Add("second")

	if h.HasNext() {
		t.Error("History.HasNext() at start returned true, want false")
	}

	h.Up("input") // Now at "second"
	h.Up("input") // Now at "first"

	if !h.HasNext() {
		t.Error("History.HasNext() after navigating up returned false, want true")
	}
}

func TestHistoryClear(t *testing.T) {
	h := NewHistory(10)
	h.Add("first")
	h.Add("second")
	h.Up("input")

	h.Clear()

	if h.Len() != 0 {
		t.Errorf("History.Clear() Len() = %d, want 0", h.Len())
	}
	if h.current != -1 {
		t.Errorf("History.Clear().current = %d, want -1", h.current)
	}
}

func TestHistoryEntries(t *testing.T) {
	h := NewHistory(10)
	h.Add("first")
	h.Add("second")
	h.Add("third")

	entries := h.Entries()

	if len(entries) != 3 {
		t.Errorf("History.Entries() len = %d, want 3", len(entries))
	}

	// Verify it's a copy (modifying shouldn't affect original)
	entries[0] = "modified"
	if h.Entries()[0] == "modified" {
		t.Error("History.Entries() returned reference instead of copy")
	}
}
