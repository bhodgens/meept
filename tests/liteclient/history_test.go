package sharedclient

import (
	"testing"

	"github.com/caimlas/meept/internal/sharedclient"
)

// ============================================================================
// Test History basic operations
// ============================================================================

func TestHistory_Add(t *testing.T) {
	h := sharedclient.NewHistory(10)

	h.Add("command1")
	h.Add("command2")

	if h.Len() != 2 {
		t.Errorf("Len() = %d, want 2", h.Len())
	}

	entries := h.Entries()
	if len(entries) != 2 {
		t.Fatalf("entries length = %d, want 2", len(entries))
	}
	if entries[0] != "command1" {
		t.Errorf("entries[0] = %q, want %q", entries[0], "command1")
	}
	if entries[1] != "command2" {
		t.Errorf("entries[1] = %q, want %q", entries[1], "command2")
	}
}

func TestHistory_EmptyInput(t *testing.T) {
	h := sharedclient.NewHistory(10)
	h.Add("")

	if h.Len() != 0 {
		t.Errorf("Len() after empty add = %d, want 0", h.Len())
	}
}

func TestHistory_ConsecutiveDuplicates(t *testing.T) {
	h := sharedclient.NewHistory(10)
	h.Add("same")
	h.Add("same")
	h.Add("same")

	if h.Len() != 1 {
		t.Errorf("Len() = %d, want 1 (duplicates not added)", h.Len())
	}

	h.Add("different")
	if h.Len() != 2 {
		t.Errorf("Len() after new entry = %d, want 2", h.Len())
	}
}

func TestHistory_MaxSizeTrimming(t *testing.T) {
	h := sharedclient.NewHistory(3)
	h.Add("a")
	h.Add("b")
	h.Add("c")
	h.Add("d") // should trim "a"

	if h.Len() != 3 {
		t.Errorf("Len() = %d, want 3", h.Len())
	}

	entries := h.Entries()
	if entries[0] != "b" {
		t.Errorf("entries[0] = %q, want %q", entries[0], "b")
	}
	if entries[2] != "d" {
		t.Errorf("entries[2] = %q, want %q", entries[2], "d")
	}
}

// ============================================================================
// Test History navigation
// ============================================================================

func TestHistory_Up(t *testing.T) {
	h := sharedclient.NewHistory(10)
	h.Add("cmd1")
	h.Add("cmd2")
	h.Add("cmd3")

	// First Up from current position (-1)
	result, ok := h.Up("current input")
	if !ok {
		t.Fatal("Up returned ok=false on first navigation")
	}
	if result != "cmd3" {
		t.Errorf("Up() = %q, want %q", result, "cmd3")
	}

	// Second Up
	result, ok = h.Up("current input")
	if !ok {
		t.Fatal("Up returned ok=false on second navigation")
	}
	if result != "cmd2" {
		t.Errorf("Up() = %q, want %q", result, "cmd2")
	}

	// Third Up - oldest entry
	result, ok = h.Up("current input")
	if !ok {
		// This is allowed - returns oldest entry with ok=false
		t.Logf("Up returned ok=false at oldest entry")
	}

	// Fourth Up - should return oldest entry (boundary)
	result, ok = h.Up("current input")
	if ok && result != "cmd1" {
		t.Errorf("Up() at boundary = %q, want %q", result, "cmd1")
	}
}

func TestHistory_Down(t *testing.T) {
	h := sharedclient.NewHistory(10)
	h.Add("cmd1")
	h.Add("cmd2")
	h.Add("cmd3")

	// Go up first
	h.Up("temp") // at cmd3
	h.Up("temp") // at cmd2

	// Down
	result, ok := h.Down("temp")
	if !ok {
		t.Fatal("Down returned ok=false when there's a next entry")
	}
	if result != "cmd3" {
		t.Errorf("Down() = %q, want %q", result, "cmd3")
	}

	// Down at the end - should return the stored temporary
	result, ok = h.Down("ignored")
	if !ok {
		t.Error("Down() at end returned ok=false")
	}
	if result != "temp" {
		t.Errorf("Down() at end = %q, want %q", result, "temp")
	}
}

func TestHistory_DownFromStart(t *testing.T) {
	h := sharedclient.NewHistory(10)
	h.Add("cmd1")

	// No navigation yet, shouldn't go down
	result, ok := h.Down("some input")
	if ok {
		t.Errorf("Down() before navigation returned ok=true with value %q", result)
	}
	if result != "" {
		t.Errorf("Down() before navigation = %q, want empty", result)
	}
}

func TestHistory_ScrollThenRestore(t *testing.T) {
	h := sharedclient.NewHistory(10)
	h.Add("a")
	h.Add("b")
	h.Add("c")
	h.Add("d")
	h.Add("e")

	type nav struct {
		dir string
	}
	var results []string

	// Navigate up 3 steps
	for i := 0; i < 3; i++ {
		r, ok := h.Up("temp")
		if ok {
			results = append(results, r)
		}
	}

	// Expected: e, d, c (most recent first)
	if len(results) != 3 {
		t.Fatalf("results length = %d, want 3: %v", len(results), results)
	}
	if results[0] != "e" || results[1] != "d" || results[2] != "c" {
		t.Errorf("navigation order = %v, want [e d c]", results)
	}

	// Scroll back down
	result, ok := h.Down("temp")
	if ok {
		if result != "d" {
			t.Errorf("Down() after 3 up = %q, want %q", result, "d")
		}
	}

	result, ok = h.Down("original")
	if ok {
		if result != "e" {
			t.Errorf("Down() again = %q, want %q", result, "e")
		}
	}

	// Note: Down returns the stored temporary from Up(), not the passed value
	result, ok = h.Down("ignored")
	if !ok {
		t.Error("Down() to top returned ok=false")
	}
	if result != "temp" {
		t.Errorf("Down() to top = %q, want 'temp'", result)
	}
}

func TestHistory_Reset(t *testing.T) {
	h := sharedclient.NewHistory(10)
	h.Add("a")
	h.Add("b")
	h.Add("c")

	h.Up("temp")
	h.Up("temp") // at cmd1

	h.Reset()

	// After reset, Up should start fresh from most recent
	result, ok := h.Up("temp again")
	if !ok {
		t.Fatal("Up after Reset returned ok=false")
	}
	if result != "c" {
		t.Errorf("Up after reset = %q, want %q", result, "c")
	}
}

// ============================================================================
// Test History edge cases
// ============================================================================

func TestHistory_HasPrevious_Next(t *testing.T) {
	h := sharedclient.NewHistory(10)

	if h.HasPrevious() {
		t.Error("HasPrevious() on empty history = true, want false")
	}
	if h.HasNext() {
		t.Error("HasNext() on empty history = true, want false")
	}

	h.Add("a")
	h.Add("b")

	if !h.HasPrevious() {
		t.Error("HasPrevious() after adding, before navigation = false, want true")
	}

	h.Up("temp")
	if !h.HasPrevious() {
		t.Error("HasPrevious() after navigating up = false, want true")
	}

	if !h.HasNext() {
		t.Error("HasNext() after one Up() = false, want true")
	}
}

func TestHistory_AddAfterNavigation(t *testing.T) {
	h := sharedclient.NewHistory(10)
	h.Add("a")
	h.Add("b")
	h.Add("c")

	// Navigate up in history
	h.Up("temp") // at c
	h.Up("temp") // at b

	// Add new entry - should reset navigation position
	h.Add("new")
	if h.HasNext() {
		t.Error("HasNext() after Add() should be false (navigation reset)")
	}

	// New entry should be at the end
	result, ok := h.Up("temp")
	if !ok {
		t.Fatal("Up after navigation reset returned ok=false")
	}
	if result != "new" {
		t.Errorf("Up() after Add() = %q, want %q", result, "new")
	}
}

func TestHistory_Clear(t *testing.T) {
	h := sharedclient.NewHistory(10)
	h.Add("a")
	h.Add("b")
	h.Add("c")
	h.Clear()

	if h.Len() != 0 {
		t.Errorf("Len() after Clear = %d, want 0", h.Len())
	}

	result, ok := h.Up("temp")
	if ok {
		t.Errorf("Up() after Clear returned ok=true with %q", result)
	}
	if result != "" {
		t.Errorf("Up() after Clear = %q, want empty", result)
	}

	h.Clear() // should be idempotent
}

func TestHistory_EntriesCopy(t *testing.T) {
	h := sharedclient.NewHistory(10)
	h.Add("a")
	h.Add("b")

	entries := h.Entries()
	// Mutate the copy
	entries[0] = "mutated"

	// Original should be unchanged
	orig := h.Entries()
	if orig[0] == "mutated" {
		t.Error("Entries() should return a copy, but mutation affected original")
	}
}
