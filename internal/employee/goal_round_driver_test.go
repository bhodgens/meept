// Package employee — goal_round_driver_test.go exercises the C4c autonomous
// round driver: admission fence (ValidGoalRound), auto-block (BlockGoal), the
// OnRoundSettled advance-then-block loop over a real GoalStore, and the
// armed/blocked harmonization the leaf mandates.

package employee

import (
	"context"
	"database/sql"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite" // sqlite driver registration
)

// newDriverTestManager builds a Manager over a temp GoalStore with the
// employee FK anchor seeded, so goals can be created and Read back.
func newDriverTestManager(t *testing.T, employeeID string) *Manager {
	t.Helper()
	path := filepath.Join(t.TempDir(), "goals.db")
	store, err := NewGoalStore(path, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("open goal store: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open seed handle: %v", err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.Exec(`INSERT INTO bot_definitions (id, data, created_at, updated_at) VALUES (?, '{}', ?, ?)`, employeeID, now, now); err != nil {
		t.Fatalf("seed employee FK anchor: %v", err)
	}
	db.Close()
	return NewManagerWithStores(nil, nil, nil, store, nil, slog.New(slog.DiscardHandler))
}

// TestValidGoalRound pins the admission fence (leaf T1): exact revision +
// sequential round accepted; anything else (stale revision, skipped or
// replayed round) rejected.
func TestValidGoalRound(t *testing.T) {
	// goal at revision 2, roundsStarted 1 → round=2 rev=2 valid.
	g := &Goal{Revision: 2, RoundsStarted: 1}
	if !ValidGoalRound(g, 2, 2) {
		t.Error("ValidGoalRound(round=2, rev=2) on rev2/started1 must be valid")
	}
	if ValidGoalRound(g, 3, 2) {
		t.Error("non-sequential round (3 on started 1) must be rejected")
	}
	if ValidGoalRound(g, 2, 1) {
		t.Error("stale revision (1, want 2) must be rejected")
	}
	if ValidGoalRound(nil, 2, 2) {
		t.Error("ValidGoalRound on a nil goal must be false")
	}
}

// TestGoalRoundDriver_ArmedBlockedHarmony is the leaf's (c) harmonization
// gate: the Goal-level armed/blocked/consecutive primitives and the driver's
// explicit-arm registry behave together correctly.
func TestGoalRoundDriver_ArmedBlockedHarmony(t *testing.T) {
	d := NewGoalRoundDriver(nil, slog.New(slog.DiscardHandler))

	g := &Goal{ID: "probe_goal", MaxRounds: 2, State: GoalActive}
	if g.IsArmed() {
		t.Error("fresh goal must be disarmed")
	}
	if g.IsBlockedAfterNConsecutive() {
		t.Error("roundsStarted 0 must not be past the 3-round consecutive threshold")
	}
	g.Activate()
	if !g.IsArmed() {
		t.Error("Activate must arm the goal")
	}
	if g.RoundsCap() != 2 {
		t.Errorf("RoundsCap with MaxRounds=2 = %d, want 2", g.RoundsCap())
	}
	g.RoundsStarted = 4
	if !g.IsBlockedAfterNConsecutive() {
		t.Error("roundsStarted 4 >= threshold 3 must report blocked-after-N")
	}
	g.Disarm()
	if g.IsArmed() {
		t.Error("Disarm must clear the live arming flag")
	}

	// Driver registers goals it arms and treats them as drivable.
	d.Arm(g)
	if !d.isArmed(g) {
		t.Error("driver.Arm must make the goal drivable to isArmed")
	}
	d.disarm(g.ID)
	if d.isArmed(g) {
		t.Error("driver.disarm must clear the explicit arming")
	}
}

// TestBlockGoal pins auto-block: BlockGoal flips the goal to GoalBlocked,
// writes the durable BlockedReason, disarms it, and persists (revision
// advances).
func TestBlockGoal(t *testing.T) {
	mgr := newDriverTestManager(t, "emp_blk")
	g, err := mgr.CreateGoal(context.Background(), GoalSpec{
		EmployeeID: "emp_blk", Title: "t", Mandate: "m", Source: "user", MaxRounds: 2,
	})
	if err != nil {
		t.Fatalf("create goal: %v", err)
	}
	g.Activate()

	if err := BlockGoal(context.Background(), mgr, g, RoundLimitBlockCode, "Goal reached its configured limit of 2 rounds."); err != nil {
		t.Fatalf("BlockGoal: %v", err)
	}
	if g.State != GoalBlocked {
		t.Errorf("BlockGoal state = %v, want GoalBlocked", g.State)
	}
	if g.BlockedReason == nil || g.BlockedReason.Code != RoundLimitBlockCode || g.BlockedReason.Message == "" {
		t.Errorf("BlockGoal blocked reason = %+v, want code round-limit", g.BlockedReason)
	}
	if g.IsArmed() {
		t.Error("blocked goal must be disarmed")
	}
	// Durable: a reload sees the blocked state.
	reloaded, err := mgr.GetGoal(context.Background(), g.ID)
	if err != nil {
		t.Fatalf("reload goal: %v", err)
	}
	if reloaded.State != GoalBlocked || reloaded.BlockedReason == nil || reloaded.BlockedReason.Code != RoundLimitBlockCode {
		t.Errorf("reloaded blocked goal = %+v", reloaded)
	}
}

// TestOnRoundSettled_AdvanceThenAutoBlock drives the full driver loop over a
// real store: a MaxRounds=2 goal advances twice then auto-blocks at the cap
// with code round-limit and zero further rounds reserved.
func TestOnRoundSettled_AdvanceThenAutoBlock(t *testing.T) {
	ctx := context.Background()
	mgr := newDriverTestManager(t, "emp_rounds")
	g, err := mgr.CreateGoal(ctx, GoalSpec{
		EmployeeID: "emp_rounds", Title: "rounds", Mandate: "m", Source: "user", MaxRounds: 2,
	})
	if err != nil {
		t.Fatalf("create goal: %v", err)
	}
	g.Activate()

	d := NewGoalRoundDriver(mgr, slog.New(slog.DiscardHandler))

	// Advance 1 and 2 (rounds 0→1, 1→2).
	for i := 0; i < 2; i++ {
		outcome, err := d.OnRoundSettled(ctx, "emp_rounds")
		if err != nil {
			t.Fatalf("OnRoundSettled advance %d: %v", i, err)
		}
		if outcome != GoalRoundAdvanced {
			t.Fatalf("advance %d outcome = %q, want advanced", i, outcome)
		}
	}
	// Third settlement: roundsStarted(2) >= cap(2) → auto-block.
	outcome, err := d.OnRoundSettled(ctx, "emp_rounds")
	if err != nil {
		t.Fatalf("OnRoundSettled final: %v", err)
	}
	if outcome != GoalRoundBlocked {
		t.Fatalf("final outcome = %q, want blocked", outcome)
	}

	reloaded, err := mgr.GetGoal(ctx, g.ID)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if reloaded.RoundsStarted != 2 {
		t.Errorf("rounds_started = %d, want 2 (capped)", reloaded.RoundsStarted)
	}
	if reloaded.State != GoalBlocked {
		t.Errorf("state = %v, want blocked", reloaded.State)
	}
	if reloaded.BlockedReason == nil || reloaded.BlockedReason.Code != RoundLimitBlockCode {
		t.Errorf("blocked reason = %+v, want code round-limit", reloaded.BlockedReason)
	}

	// A further advance on the now-blocked goal is idle (no drivable goal).
	outcome, err = d.OnRoundSettled(ctx, "emp_rounds")
	if err != nil || outcome != GoalRoundIdle {
		t.Errorf("post-block outcome = %q, err=%v; want idle/nil", outcome, err)
	}
}

// TestOnRoundSettled_NoGoalIdle pins that a settled employee with no active
// goal produces the idle outcome with no error.
func TestOnRoundSettled_NoGoalIdle(t *testing.T) {
	mgr := newDriverTestManager(t, "emp_idle")
	d := NewGoalRoundDriver(mgr, slog.New(slog.DiscardHandler))
	outcome, err := d.OnRoundSettled(context.Background(), "emp_idle")
	if err != nil || outcome != GoalRoundIdle {
		t.Fatalf("no-goal outcome = %q, err=%v; want idle/nil", outcome, err)
	}
}
