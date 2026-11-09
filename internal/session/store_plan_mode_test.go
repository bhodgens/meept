package session

import (
	"testing"
)

// TestSQLiteStore_PlanModeRoundTrip pins DSH-9c: a session's per-session
// plan_mode flag survives the store round-trip (set -> Get from the SQLite
// store after a fresh SELECT, not just the shared pointer). This is the
// durable backing for the PlanModeController's PlanModePersistence seam — a
// daemon restart reads the flag back from this column.
func TestSQLiteStore_PlanModeRoundTrip(t *testing.T) {
	store, _ := testHelper(t)
	defer store.Close()

	sess, err := store.Create("plan-mode-roundtrip")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if sess.PlanMode {
		t.Fatal("fresh session PlanMode = true, want false")
	}

	if err := store.SetPlanMode(sess.ID, true); err != nil {
		t.Fatalf("SetPlanMode: %v", err)
	}

	// Re-read through getByColumn (a fresh SELECT, not the shared pointer).
	got := store.Get(sess.ID)
	if got == nil {
		t.Fatal("session not found after SetPlanMode")
	}
	if !got.PlanMode {
		t.Error("PlanMode = false after SetPlanMode(true); the flag did not survive the store round-trip")
	}

	// List path reads the same column (scanSessionRow column-order guard).
	list, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	found := false
	for _, s := range list {
		if s.ID == sess.ID {
			found = true
			if !s.PlanMode {
				t.Error("List row PlanMode = false; column order drifted from the scan")
			}
		}
	}
	if !found {
		t.Error("session missing from List")
	}

	// Clear.
	if err := store.SetPlanMode(sess.ID, false); err != nil {
		t.Fatalf("SetPlanMode clear: %v", err)
	}
	got = store.Get(sess.ID)
	if got == nil || got.PlanMode {
		t.Fatalf("PlanMode after clear = %v, want false", got)
	}

	// Unknown session surfaces an error (mirrors SetDetectionContext).
	if err := store.SetPlanMode("session-does-not-exist", true); err == nil {
		t.Error("SetPlanMode on missing session: expected error, got nil")
	}
}

// TestMemoryStore_PlanModeRoundTrip pins the memory-store parity path.
func TestMemoryStore_PlanModeRoundTrip(t *testing.T) {
	store := NewMemoryStore(nil)

	sess, err := store.Create("plan-mode-roundtrip-mem")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := store.SetPlanMode(sess.ID, true); err != nil {
		t.Fatalf("SetPlanMode: %v", err)
	}
	got := store.Get(sess.ID)
	if got == nil || !got.PlanMode {
		t.Fatalf("PlanMode = %v, want true", got)
	}
	if err := store.SetPlanMode("session-missing", true); err == nil {
		t.Error("SetPlanMode on missing session: expected error, got nil")
	}
}
