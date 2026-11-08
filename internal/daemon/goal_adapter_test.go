package daemon

// goal_adapter_test.go pins the *employee.Manager → builtin.GoalManager goal
// adapter (goal_adapter.go): the fail-closed nil guard AND a live round-trip
// (create → get → update) through a real employee.Manager + GoalStore so the
// field/semantics mapping is verified end-to-end, not just the types.

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite" // sqlite driver registration

	"github.com/caimlas/meept/internal/employee"
	"github.com/caimlas/meept/internal/tools/builtin"
)

// TestGoalAdapterNilGuard pins the typed-nil posture: a typed-nil adapter and
// an adapter wrapping a nil manager both fail closed with
// errGoalStoreUnavailable instead of panicking on a nil deref.
func TestGoalAdapterNilGuard(t *testing.T) {
	ctx := context.Background()

	var typedNil *goalEmployeeManagerAdapter
	if _, err := typedNil.CreateGoal(ctx, builtin.GoalCreateSpec{Owner: "emp_1", Objective: "x"}); !errors.Is(err, errGoalStoreUnavailable) {
		t.Fatalf("typed-nil CreateGoal err=%v, want errGoalStoreUnavailable", err)
	}

	wrapped := &goalEmployeeManagerAdapter{m: nil}
	if _, err := wrapped.ListActive(ctx, "emp_1"); !errors.Is(err, errGoalStoreUnavailable) {
		t.Fatalf("nil-manager ListActive err=%v, want errGoalStoreUnavailable", err)
	}
	if err := wrapped.UpdateGoal(ctx, &builtin.GoalRecord{ID: "g"}); !errors.Is(err, errGoalStoreUnavailable) {
		t.Fatalf("nil-manager UpdateGoal err=%v, want errGoalStoreUnavailable", err)
	}
}

// TestGoalAdapterCreateGetUpdateRoundTrip drives the adapter against a live
// employee.Manager + GoalStore (temp DB), proving the field mapping lands in
// the store and reads back faithfully, and that the CAS revision advances.
func TestGoalAdapterCreateGetUpdateRoundTrip(t *testing.T) {
	ctx := context.Background()
	goalsDBPath := filepath.Join(t.TempDir(), "goals.db")
	store, err := employee.NewGoalStore(goalsDBPath, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("open goal store: %v", err)
	}
	defer store.Close()

	// Seed the FK anchor (employee_goals.employee_id → bot_definitions.id)
	// into the SAME database the goal store opened. On the daemon's
	// shared-DB layout this row comes from hiring; here we mirror that so
	// CreateGoal passes the FK constraint.
	db, err := sql.Open("sqlite", goalsDBPath)
	if err != nil {
		t.Fatalf("open seed handle: %v", err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.Exec(`INSERT INTO bot_definitions (id, data, created_at, updated_at) VALUES ('emp_1','{}',?,?)`, now, now); err != nil {
		t.Fatalf("seed employee FK anchor: %v", err)
	}
	db.Close()

	mgr := employee.NewManagerWithStores(nil, nil, nil, store, nil, slog.New(slog.DiscardHandler))
	adapter := &goalEmployeeManagerAdapter{m: mgr}

	// CreateGoal: mapping + armed-on-create + round cap persisted at rev 1.
	rec, err := adapter.CreateGoal(ctx, builtin.GoalCreateSpec{Owner: "emp_1", Objective: "keep the build green", MaxRounds: 3})
	if err != nil {
		t.Fatalf("CreateGoal: %v", err)
	}
	if rec.ID == "" || rec.Objective != "keep the build green" || rec.Phase != "active" {
		t.Fatalf("CreateGoal record mismatch: %+v", rec)
	}
	if rec.MaxRounds != 3 || rec.Revision != 1 {
		t.Fatalf("CreateGoal autonomy fields mismatch: maxRounds=%d revision=%d, want 3/1", rec.MaxRounds, rec.Revision)
	}
	if !rec.Armed {
		t.Error("CreateGoal record not armed (contract arms-active-on-create)")
	}

	// GetGoal round-trip: fields survive the store reload.
	got, err := adapter.GetGoal(ctx, rec.ID)
	if err != nil {
		t.Fatalf("GetGoal: %v", err)
	}
	if got == nil || got.Objective != rec.Objective || got.MaxRounds != 3 || got.Revision != 1 {
		t.Fatalf("GetGoal did not reflect created goal: %+v", got)
	}

	// UpdateGoal: mutate to blocked + disarm, persist, re-read.
	got.Phase = "blocked"
	reason := "reached the probe cap"
	got.BlockedReason = &reason
	got.Armed = false
	if err := adapter.UpdateGoal(ctx, got); err != nil {
		t.Fatalf("UpdateGoal: %v", err)
	}
	after, err := adapter.GetGoal(ctx, rec.ID)
	if err != nil {
		t.Fatalf("GetGoal after update: %v", err)
	}
	if after.Phase != "blocked" {
		t.Fatalf("after update phase = %q, want blocked", after.Phase)
	}
	if after.Armed {
		t.Error("blocked goal still reports armed")
	}
	if after.BlockedReason == nil || !strings.Contains(*after.BlockedReason, "probe cap") {
		t.Fatalf("blocked reason not persisted round-trip: %+v", after.BlockedReason)
	}
	if after.Revision != 2 {
		t.Fatalf("revision after one update = %d, want 2 (CAS advance)", after.Revision)
	}
}
