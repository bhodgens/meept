package session

import (
	"log/slog"
	"testing"
)

// TestSQLiteStore_ForegroundRoundTrip verifies the D11 foreground-session
// flag persists: set → store → load → still set, and that a fresh session
// defaults to false so existing deployments see no behavior change.
func TestSQLiteStore_ForegroundRoundTrip(t *testing.T) {
	store, _ := testHelper(t)
	defer store.Close()

	s, err := store.Create("foreground-roundtrip")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Default must be false.
	got := store.Get(s.ID)
	if got == nil {
		t.Fatal("Get returned nil")
	}
	if got.Foreground {
		t.Fatal("fresh session must default to Foreground=false")
	}

	if err := store.SetForeground(s.ID, true); err != nil {
		t.Fatalf("SetForeground(true): %v", err)
	}
	got = store.Get(s.ID)
	if got == nil {
		t.Fatal("Get after SetForeground returned nil")
	}
	if !got.Foreground {
		t.Fatal("Foreground=true lost after store round-trip")
	}

	if err := store.SetForeground(s.ID, false); err != nil {
		t.Fatalf("SetForeground(false): %v", err)
	}
	got = store.Get(s.ID)
	if got == nil {
		t.Fatal("Get after SetForeground(false) returned nil")
	}
	if got.Foreground {
		t.Fatal("Foreground=false lost after store round-trip")
	}
}

// TestSQLiteStore_ForegroundMissingSession checks the typed error path on
// an unknown session ID instead of a silent no-op.
func TestSQLiteStore_ForegroundMissingSession(t *testing.T) {
	store, _ := testHelper(t)
	defer store.Close()

	if err := store.SetForeground("does-not-exist", true); err == nil {
		t.Fatal("expected error setting Foreground on missing session, got nil")
	}
}

// TestMemoryStore_ForegroundRoundTrip covers the in-memory store so both
// Store implementations honor the flag.
func TestMemoryStore_ForegroundRoundTrip(t *testing.T) {
	store := NewMemoryStore(slog.Default())
	defer store.Close()

	s, err := store.Create("mem-session")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := store.SetForeground(s.ID, true); err != nil {
		t.Fatalf("SetForeground(true): %v", err)
	}
	got := store.Get(s.ID)
	if got == nil {
		t.Fatal("Get returned nil")
	}
	if !got.Foreground {
		t.Fatal("Foreground=true lost in memory store")
	}

	if err := store.SetForeground(s.ID, false); err != nil {
		t.Fatalf("SetForeground(false): %v", err)
	}
	if store.Get(s.ID).Foreground {
		t.Fatal("Foreground=false lost in memory store")
	}

	if err := store.SetForeground("missing", true); err == nil {
		t.Fatal("expected error setting Foreground on missing session, got nil")
	}
}
