package daemon

// 01-safety TEST GATE T12 (00T-gate.md): nilSafeEvolverPlanStore typed-nil
// wiring (F5c, F-NEW-1).
//
// internal/daemon/AGENTS.md typed-nil rule: a nil-dependency test must go
// through the REAL wiring function, not a hand-built struct. The daemon
// boxes nilSafeEvolverPlanStore's CONCRETE *plan.SQLiteStore return into
// services.Config.FallbackPlanStore (a plan.PlanStore interface field):
// when the evolver sink is disabled the boxed nil is non-nil, defeats
// every !=nil guard downstream, and GET /api/v1/plans panics.
//
// Contract: nilSafeEvolverPlanStore must return a TRUE interface nil when
// the concrete store is nil (return type plan.PlanStore, concrete-nil
// check at the wiring seam).
//
// RED today: the function returns *plan.SQLiteStore; the boxed value is
// non-nil (compile-time shape pin below fails to build against the
// concrete return type).

import (
	"testing"

	"github.com/caimlas/meept/internal/plan"
)

// TestNilSafeEvolverStoreInterfaceNil pins the wiring seam through the
// REAL function: with Components carrying no evolver store (sink
// disabled -- the default-config daemon shape), the value the service
// registry receives must be a TRUE nil interface.
func TestNilSafeEvolverStoreInterfaceNil(t *testing.T) {
	c := &Components{} // EvolverPlanStore nil: evolver sink disabled

	// The contract shape: the wiring helper returns the INTERFACE type
	// so a nil concrete cannot box into a non-nil interface. Today the
	// function returns *plan.SQLiteStore and this assignment does not
	// compile (RED: cannot use ... as plan.PlanStore value in variable
	// declaration -- the concrete return type IS the defect).
	var storeIF plan.PlanStore = nilSafeEvolverPlanStore(c)
	if storeIF != nil {
		t.Fatalf("CURRENT DEFECT: nilSafeEvolverPlanStore boxed a nil concrete into a non-nil interface (%T); the !=nil guards downstream pass and GET /plans derefs nil (F-NEW-1)", storeIF)
	}

	// Nil Components must stay nil-safe too.
	var nilComponentsIF plan.PlanStore = nilSafeEvolverPlanStore(nil)
	if nilComponentsIF != nil {
		t.Fatalf("nilSafeEvolverPlanStore(nil) = %v, want true nil", nilComponentsIF)
	}
}

// TestNilSafeEvolverStoreLivePassThrough pins the positive path: a REAL
// evolver store passes through the wiring seam unchanged (the fix must
// not break the enabled-sink case).
func TestNilSafeEvolverStoreLivePassThrough(t *testing.T) {
	store, err := plan.NewSQLiteStore(t.TempDir()+"/sink.db", nil)
	if err != nil {
		t.Fatalf("sink store: %v", err)
	}
	t.Cleanup(func() {
		if cerr := store.Close(); cerr != nil {
			t.Logf("close sink store: %v", cerr)
		}
	})
	c := &Components{EvolverPlanStore: store}

	var storeIF plan.PlanStore = nilSafeEvolverPlanStore(c)
	if storeIF == nil {
		t.Fatal("a live evolver store must pass through the wiring seam, not be nil-ed out")
	}
	got, ok := storeIF.(*plan.SQLiteStore)
	if !ok || got != store {
		t.Fatalf("pass-through changed the store identity: %T %v", storeIF, storeIF)
	}
}
