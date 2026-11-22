package services

// 01-safety TEST GATE T12 (00T-gate.md): plans typed-nil panic (F5c).
//
// F-NEW-1: GET /api/v1/plans panics when the evolver sink is disabled.
// daemon.go nilSafeEvolverPlanStore returns a CONCRETE *plan.SQLiteStore;
// a nil concrete boxed into the plan.PlanStore interface field is NOT
// == nil, so SetEvolverSink's plain !=nil guard stores it and List/Get
// dereference the nil store (s.db.QueryContext on a nil receiver).
//
// 07-replan-storm-plans-panic contract: SetEvolverSink + List/Get guards
// reject typed-nil interface values (reflect-safe); List returns the
// shared-store plans, Get falls back cleanly, no panic.
//
// RED today: List panics on the typed-nil fallback store.

import (
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/plan"
)

// planConfigForGate is the default plans config (same shape the sink
// fallback tests use: config.Config{}.Plans).
func planConfigForGate() config.PlansConfig { return config.Config{}.Plans }

// TestPlansTypedNilList pins the GET /plans shape: a real shared store
// plus a typed-nil sink store (evolver disabled) must List without
// panic and return the shared plans.
func TestPlansTypedNilList(t *testing.T) {
	dir := t.TempDir()
	sharedStore, err := plan.NewSQLiteStore(filepath.Join(dir, "shared.db"), slog.Default())
	if err != nil {
		t.Fatalf("shared store: %v", err)
	}
	t.Cleanup(func() {
		if cerr := sharedStore.Close(); cerr != nil {
			t.Logf("close shared store: %v", cerr)
		}
	})
	ctx := context.Background()
	sharedPlan := &plan.Plan{ID: "plan-typednil-shared", Title: "shared plan", State: plan.StatePendingApproval}
	if err := sharedStore.CreatePlan(ctx, sharedPlan); err != nil {
		t.Fatalf("seed shared plan: %v", err)
	}

	sharedMgr := plan.NewPlanManager(sharedStore, nil, planConfigForGate(), stubServiceTaskCreator{}, slog.Default())
	svc := NewPlanService(sharedMgr, sharedStore)

	// The daemon wiring shape: nilSafeEvolverPlanStore returns a nil
	// CONCRETE *SQLiteStore, boxed into the interface parameter.
	var nilSink *plan.SQLiteStore
	svc.SetEvolverSink(nil, nilSink)

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("CURRENT DEFECT: List panicked on the typed-nil sink store (%v); GET /api/v1/plans crashes the request when the evolver sink is disabled (F-NEW-1)", r)
		}
	}()
	plans, err := svc.List(ctx, "", 50)
	if err != nil {
		t.Fatalf("List with typed-nil sink: %v", err)
	}
	if len(plans) != 1 || plans[0].ID != "plan-typednil-shared" {
		t.Errorf("List returned %+v, want the shared-store plan (sink disabled must not hide shared plans)", plans)
	}
}

// TestPlansTypedNilGet pins the Get fallback: a miss in the shared store
// with a typed-nil sink must surface ErrNotFound, never panic.
func TestPlansTypedNilGet(t *testing.T) {
	dir := t.TempDir()
	sharedStore, err := plan.NewSQLiteStore(filepath.Join(dir, "shared.db"), slog.Default())
	if err != nil {
		t.Fatalf("shared store: %v", err)
	}
	t.Cleanup(func() {
		if cerr := sharedStore.Close(); cerr != nil {
			t.Logf("close shared store: %v", cerr)
		}
	})
	sharedMgr := plan.NewPlanManager(sharedStore, nil, planConfigForGate(), stubServiceTaskCreator{}, slog.Default())
	svc := NewPlanService(sharedMgr, sharedStore)
	var nilSink *plan.SQLiteStore
	svc.SetEvolverSink(nil, nilSink)

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("CURRENT DEFECT: Get panicked on the typed-nil sink store (%v); the fallback guard `fallbackStore != nil` passes a boxed nil (F-NEW-1)", r)
		}
	}()
	_, err = svc.Get(context.Background(), "plan-does-not-exist")
	if err == nil {
		t.Fatal("Get(missing) returned nil error, want not-found")
	}
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("Get(missing) error = %v, want ErrNotFound (clean miss, no typed-nil deref)", err)
	}
}

// TestPlansTypedNilSinkIgnored pins the setter guard itself: a typed-nil
// store must be IGNORED by SetEvolverSink (the reflect-safe nil check),
// leaving the service exactly as if no sink was wired.
func TestPlansTypedNilSinkIgnored(t *testing.T) {
	dir := t.TempDir()
	sharedStore, err := plan.NewSQLiteStore(filepath.Join(dir, "shared.db"), slog.Default())
	if err != nil {
		t.Fatalf("shared store: %v", err)
	}
	t.Cleanup(func() {
		if cerr := sharedStore.Close(); cerr != nil {
			t.Logf("close shared store: %v", cerr)
		}
	})
	sharedMgr := plan.NewPlanManager(sharedStore, nil, planConfigForGate(), stubServiceTaskCreator{}, slog.Default())
	svc := NewPlanService(sharedMgr, sharedStore)

	var nilSink *plan.SQLiteStore
	var nilMgr *plan.PlanManager
	svc.SetEvolverSink(nilMgr, nilSink)

	if svc.fallbackStore != nil {
		t.Errorf("CURRENT DEFECT: SetEvolverSink stored a typed-nil fallback store (plain !=nil guard); the reflect-safe guard must ignore boxed nils")
	}
	if svc.fallbackManager != nil {
		t.Errorf("SetEvolverSink stored a typed-nil fallback manager; boxed nils must be ignored")
	}
}
