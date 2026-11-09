package daemon

import (
	"log/slog"
	"testing"

	"github.com/caimlas/meept/internal/session"
	"github.com/caimlas/meept/internal/tools"
	"github.com/caimlas/meept/internal/tools/builtin"
)

// planModeTestStore wraps a real session.MemoryStore with a "missing row"
// mode: sessions registered in `missing` report Get=nil and SetPlanMode
// errors ("session not found"), simulating a session that has no durable
// row yet (brand-new session / store without the row).
type planModeTestStore struct {
	*session.MemoryStore
	missing map[string]bool
}

func (s *planModeTestStore) Get(id string) *session.Session {
	if s.missing[id] {
		return nil
	}
	return s.MemoryStore.Get(id)
}

func (s *planModeTestStore) SetPlanMode(sessionID string, on bool) error {
	if s.missing[sessionID] {
		return &sessionNotFoundError{id: sessionID}
	}
	return s.MemoryStore.SetPlanMode(sessionID, on)
}

type sessionNotFoundError struct{ id string }

func (e *sessionNotFoundError) Error() string { return "session not found: " + e.id }

func newPlanModeTestStore() *planModeTestStore {
	return &planModeTestStore{MemoryStore: session.NewMemoryStore(nil), missing: make(map[string]bool)}
}

// TestPlanModeStorePersistence_DurableRoundTrip (DSH-9c) pins the durable
// lane: SetPlanMode writes through to the session store's row and GetPlanMode
// reads it back from the store — the shape a fresh process uses to restore
// the flag after a restart.
func TestPlanModeStorePersistence_DurableRoundTrip(t *testing.T) {
	store := newPlanModeTestStore()
	// The session row must exist before SetPlanMode can persist to it (the
	// real store's contract: UPDATE ... WHERE id = ?). Create mints the ID.
	sessRow, err := store.Create("sess-live")
	if err != nil {
		t.Fatalf("create session row: %v", err)
	}
	sessID := sessRow.ID
	p := newPlanModeStorePersistence(store, newPlanModeMemoryPersistence())

	if err := p.SetPlanMode(sessID, true); err != nil {
		t.Fatalf("SetPlanMode: %v", err)
	}
	if on, err := p.GetPlanMode(sessID); err != nil || !on {
		t.Fatalf("GetPlanMode = %v, %v; want true, nil (durable round trip)", on, err)
	}

	// Turn off and verify the durable read side reflects it.
	if err := p.SetPlanMode(sessID, false); err != nil {
		t.Fatalf("SetPlanMode off: %v", err)
	}
	if on, err := p.GetPlanMode(sessID); err != nil || on {
		t.Fatalf("GetPlanMode after off = %v, %v; want false, nil", on, err)
	}

	// The row itself carries the flag (what a restarted daemon reads via
	// Store.Get).
	if row := store.Get(sessID); row == nil || row.PlanMode {
		t.Fatalf("session row PlanMode = %v, want false after off", row)
	}
}

// TestPlanModeStorePersistence_FallbackOnMissingRow pins the fallback
// contract: a session with NO store row (brand-new session, or store
// unavailability) still toggles through the memory lane instead of erroring
// or resurrecting stale durable state.
func TestPlanModeStorePersistence_FallbackOnMissingRow(t *testing.T) {
	store := newPlanModeTestStore()
	store.missing["sess-new"] = true // no row, writes fail
	mem := newPlanModeMemoryPersistence()
	p := newPlanModeStorePersistence(store, mem)

	// Toggle must succeed via the fallback lane, not error.
	if err := p.SetPlanMode("sess-new", true); err != nil {
		t.Fatalf("SetPlanMode on missing row: %v; want fallback success", err)
	}
	if on, err := p.GetPlanMode("sess-new"); err != nil || !on {
		t.Fatalf("GetPlanMode on missing row = %v, %v; want true via memory fallback", on, err)
	}
	if on, _ := mem.GetPlanMode("sess-new"); !on {
		t.Fatal("fallback lane did not record the toggle")
	}

	// Other sessions with rows stay on the durable lane.
	durableRow, err := store.Create("sess-durable")
	if err != nil {
		t.Fatalf("create durable-lane row: %v", err)
	}
	if err := p.SetPlanMode(durableRow.ID, true); err != nil {
		t.Fatalf("SetPlanMode durable lane: %v", err)
	}
	// The write landed on the durable row itself (the adapter keeps the
	// memory lane in sync for fallback reads; the STORE row is the source
	// of truth a restarted process reads).
	if row := store.Get(durableRow.ID); row == nil || !row.PlanMode {
		t.Fatalf("durable-lane write did not reach the session row (PlanMode=%v)", row)
	}
}

// TestWirePlanMode_MemoryFallbackWhenNoStore pins the wiring degradation: a
// Components without a session store still constructs the controller (the
// memory adapter), never a nil persistence seam.
func TestWirePlanMode_MemoryFallbackWhenNoStore(t *testing.T) {
	c := &Components{ToolRegistry: tools.NewRegistry(slog.New(slog.DiscardHandler))}
	wirePlanMode(c, slog.New(slog.DiscardHandler))
	if c.PlanModeController == nil {
		t.Fatal("wirePlanMode without a session store left the controller nil (must degrade to the memory adapter)")
	}
	// The memory fallback toggles within the process.
	if err := c.PlanModeController.SetPlanMode("sess-x", true); err != nil {
		t.Fatalf("SetPlanMode: %v", err)
	}
	if !c.PlanModeController.PlanMode("sess-x") {
		t.Fatal("plan mode did not toggle via the memory fallback adapter")
	}
}

// TestPlanModeController_StoreBackedRestore pins the restart-restore shape
// end to end through the REAL controller: a controller wired with the store
// adapter reads a session's persisted plan_mode=true (written before the
// "restart") from the store alone — the fresh process has no in-memory view.
func TestPlanModeController_StoreBackedRestore(t *testing.T) {
	store := newPlanModeTestStore()
	// Pre-restart write: the session row exists and entered plan mode.
	row, err := store.Create("sess-restore")
	if err != nil {
		t.Fatalf("seed row: %v", err)
	}
	if err := store.SetPlanMode(row.ID, true); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// "Restart": a fresh controller over the same store (empty memory).
	restored := builtin.NewPlanModeController(newPlanModeStorePersistence(store, newPlanModeMemoryPersistence()))
	if !restored.PlanMode(row.ID) {
		t.Fatal("fresh controller did not restore plan mode from the session store after restart")
	}
	// Unrelated session defaults OFF.
	if restored.PlanMode("session-never-created") {
		t.Fatal("plan mode restored for a session that never enabled it")
	}
}
