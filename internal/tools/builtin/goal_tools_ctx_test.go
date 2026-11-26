// Package builtin — goal_tools_ctx_test.go pins H8/A6-1: the goal tools'
// autonomous-round authority is carried PER CONTEXT (WithGoalAutonomous),
// never in a process-global. Two concurrent rounds — one driver-stamped
// (autonomous) and one plain (direct human) — must get DIFFERENT verdicts,
// and neither round's lifecycle may flip the other's verdict mid-flight.
package builtin

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
)

// TestGoalAuthorityIsPerContext pins the core H8 isolation: with one
// "driver round" ctx active (autonomous), a concurrently running plain
// trigger keeps direct-human verdicts (create/edit/pause/resume ALLOWED,
// self-block threshold not applied), and the stamped ctx round is REFUSED
// the human-only actions. The OLD process-global implementation cannot
// produce this split: one atomic.Bool serves both callers, so whichever
// verdict it holds is applied to every concurrent turn (the bad case:
// while a driver round ran, a plain/cron turn was refused OR — after the
// first round finished and flipped the global off — the still-running
// driver round passed the direct-human gate and could edit/pause/resume).
func TestGoalAuthorityIsPerContext(t *testing.T) {
	m := newFakeGoalManager()
	// Seed a goal owned by the "driver" employee for update actions.
	g, err := m.CreateGoal(context.Background(), GoalCreateSpec{Owner: "emp_driver", Objective: "orig", MaxRounds: 5})
	if err != nil {
		t.Fatalf("seed goal: %v", err)
	}

	createPlain := NewGoalCreateTool(m, goalOwnerStatic("emp_plain"))
	createDriver := NewGoalCreateTool(m, goalOwnerStatic("emp_driver"))
	updateDriver := NewGoalUpdateTool(m, goalOwnerStatic("emp_driver"))
	updatePlain := NewGoalUpdateTool(m, goalOwnerStatic("emp_plain"))

	// Run both "rounds" concurrently over the shared store.
	var (
		wg           sync.WaitGroup
		plainAllowed bool
		driverDenied bool
		// The driver round is mid-flight (complete allowed) while a plain
		// turn's create is separately adjudicated.
		driverCompleteOK bool
		plainBlockOK     bool
	)
	// Stamped ctx = the driver round's authority (autonomous: human-only
	// actions refused). Plain ctx = direct human (fail-safe default:
	// allowed).
	driverCtx := autonomousCtx()
	plainCtx := context.Background()

	wg.Add(2)
	go func() {
		defer wg.Done()
		res, _ := createDriver.Execute(driverCtx, map[string]any{"objective": "driver-created"})
		driverDenied = errors.Is(toErr(res), ErrGoalToolDirectHumanRequired)
		// complete IS legal autonomously.
		res, _ = updateDriver.Execute(driverCtx, map[string]any{
			"goal_id": g.ID, "revision": float64(g.Revision), "action": "complete"})
		driverCompleteOK = res.(map[string]any)["goal"] != nil && toErr(res) == nil
	}()
	go func() {
		defer wg.Done()
		res, _ := createPlain.Execute(plainCtx, map[string]any{"objective": "human-created"})
		plainAllowed = toErr(res) == nil
		// A direct human may block below the threshold (operator authority)
		// even while the driver round runs.
		seed := &GoalRecord{ID: "goal_lowround", Objective: "o", Phase: string(goalPhaseActive), RoundsStarted: 0, Revision: 1}
		m.mu.Lock()
		m.goals[seed.ID] = cloneGoal(seed)
		m.mu.Unlock()
		res, _ = updatePlain.Execute(plainCtx, map[string]any{
			"goal_id": seed.ID, "revision": float64(1), "action": "blocked", "blocked_reason": "human says so"})
		plainBlockOK = toErr(res) == nil
	}()
	wg.Wait()

	if !driverDenied {
		t.Error("driver-stamped (autonomous) round must be REFUSED goal_create; it was allowed (H8 leak)")
	}
	if !plainAllowed {
		t.Error("plain concurrent turn must keep the direct-human fail-safe verdict (allowed); the driver round's autonomy leaked onto it (H8)")
	}
	if !driverCompleteOK {
		t.Error("driver round must be allowed goal_update complete (autonomous authority)")
	}
	if !plainBlockOK {
		t.Error("direct human may block at any round even while a driver round is active (H8 spurious-refusal regression)")
	}
	// The retired-goal side effect of the driver complete must not have
	// touched the plain turn's created goal (isolation sanity).
	if seen := listGoalIDs(t, m); !hasPrefixCount(seen, "goal_", 1) || len(seen) < 2 {
		// plain created one goal; driver's create was denied.
		t.Errorf("unexpected goal set after split-verdict run: %v", seen)
	}
}

// TestSetGoalAutonomousRoundIsNoOp pins the H8 retirement: the old
// process-global seam no longer moves any verdict. Setting it true must NOT
// flip a plain ctx round into autonomous refusal, and setting it false must
// NOT release a stamped ctx round into human authority. The fail-safe
// default (no stamp = direct human) stands.
func TestSetGoalAutonomousRoundIsNoOp(t *testing.T) {
	m := newFakeGoalManager()
	tool := NewGoalCreateTool(m, goalOwnerStatic("emp_1"))

	SetGoalAutonomousRound(true) // old global knob: must be inert
	res, _ := tool.Execute(context.Background(), map[string]any{"objective": "plain after global-true"})
	if toErr(res) != nil {
		t.Errorf("retired global flag must not gate a plain round: %s", resultText(res))
	}

	SetGoalAutonomousRound(false) // even cleared, the ctx stamp decides
	res, _ = tool.Execute(autonomousCtx(), map[string]any{"objective": "stamped after global-false"})
	if !errors.Is(toErr(res), ErrGoalToolDirectHumanRequired) {
		t.Errorf("stamped ctx must still be refused; global=false leaked human authority: %s", resultText(res))
	}
}

// TestGoalAutonomousRoundMidFlightIsolation pins failure direction 2 of
// H8: the FIRST round to finish must not flip the other off. Simulated
// old-shape teardown: one stamped round "finishes" (its ctx simply stops
// being used); the other stamped round, still running, keeps its
// autonomous verdict — and a plain ctx keeps its direct-human verdict —
// with no shared mutable state in between.
func TestGoalAutonomousRoundMidFlightIsolation(t *testing.T) {
	m := newFakeGoalManager()
	g1, _ := m.CreateGoal(context.Background(), GoalCreateSpec{Owner: "emp_a", Objective: "a", MaxRounds: 5})
	g2, _ := m.CreateGoal(context.Background(), GoalCreateSpec{Owner: "emp_b", Objective: "b", MaxRounds: 5})

	// Round A "settles first": the old code ran defer MarkAutonomousRound(false)
	// here, clobbering round B mid-flight. With ctx carriage there is no
	// global to clobber.
	resA, _ := NewGoalCreateTool(m, goalOwnerStatic("emp_a")).Execute(autonomousCtx(), map[string]any{"objective": "a2"})
	if toErr(resA) == nil || !strings.Contains(resultText(resA), "denied") {
		t.Fatal("round A create must be denied (autonomous)")
	}
	// Round B still running: its edit stays denied...
	resB, _ := NewGoalUpdateTool(m, goalOwnerStatic("emp_b")).Execute(autonomousCtx(), map[string]any{
		"goal_id": g2.ID, "revision": float64(g2.Revision), "action": "edit", "objective": "B edited"})
	if !errors.Is(toErr(resB), ErrGoalToolDirectHumanRequired) {
		t.Errorf("round B (still running, stamped) must stay refused after round A settled: %s", resultText(resB))
	}
	// ...and B's self-block below threshold stays gated by the threshold,
	// not released by A's teardown.
	low := &GoalRecord{ID: "goal_low", Objective: "o", Phase: string(goalPhaseActive), RoundsStarted: 1, Revision: 1}
	m.mu.Lock()
	m.goals[low.ID] = cloneGoal(low)
	m.mu.Unlock()
	res, _ := NewGoalUpdateTool(m, goalOwnerStatic("emp_b")).Execute(autonomousCtx(), map[string]any{
		"goal_id": low.ID, "revision": float64(1), "action": "blocked", "blocked_reason": "early"})
	if !errors.Is(toErr(res), ErrGoalToolBlockThreshold) {
		t.Errorf("stamped round's sub-threshold self-block must stay gated: %s", resultText(res))
	}
	// A plain concurrent turn is untouched by both: edit allowed,
	// sub-threshold block allowed (direct human).
	res, _ = NewGoalUpdateTool(m, goalOwnerStatic("emp_c")).Execute(context.Background(), map[string]any{
		"goal_id": g1.ID, "revision": float64(g1.Revision), "action": "edit", "objective": "human edited"})
	if toErr(res) != nil {
		t.Errorf("plain turn edit refused while driver rounds ran (H8 spurious refusal): %s", resultText(res))
	}
}

func listGoalIDs(t *testing.T, m *fakeGoalManager) []string {
	t.Helper()
	m.mu.Lock()
	defer m.mu.Unlock()
	ids := make([]string, 0, len(m.goals))
	for id := range m.goals {
		ids = append(ids, id)
	}
	return ids
}

func hasPrefixCount(ids []string, prefix string, min int) bool {
	n := 0
	for _, id := range ids {
		if strings.HasPrefix(id, prefix) {
			n++
		}
	}
	return n >= min
}
