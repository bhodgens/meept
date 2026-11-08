// Package builtin — goal_tools_test.go exercises the model-facing goal
// tools (contract C4b, leaf docs/plans/20261102-dsh-convergence/02-goals-
// plans/02-model-goal-tools.md): goal_get / goal_create / goal_update with
// the authority gate. Uses a fake goalManager for the backing store and a
// process-local resolution seam so no daemon is required.
package builtin

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/caimlas/meept/internal/toolprov"
	"github.com/caimlas/meept/internal/tools"
)

// fakeGoalManager is an in-memory goalManager.
type fakeGoalManager struct {
	mu    sync.Mutex
	goals map[string]*GoalRecord
	next  int
}

func newFakeGoalManager() *fakeGoalManager {
	return &fakeGoalManager{goals: map[string]*GoalRecord{}}
}

func (f *fakeGoalManager) CreateGoal(_ context.Context, spec GoalCreateSpec) (*GoalRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.next++
	g := &GoalRecord{
		ID:        fmt.Sprintf("goal_%d", f.next),
		Objective: spec.Objective,
		Phase:     string(goalPhaseActive),
		MaxRounds: spec.MaxRounds,
		Revision:  1,
		Armed:     true,
	}
	f.goals[g.ID] = cloneGoal(g)
	return cloneGoal(g), nil
}

func (f *fakeGoalManager) GetGoal(_ context.Context, id string) (*GoalRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	g, ok := f.goals[id]
	if !ok {
		return nil, ErrGoalToolNotFound
	}
	return cloneGoal(g), nil
}

func (f *fakeGoalManager) ListActive(_ context.Context, _ string) ([]*GoalRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []*GoalRecord
	for _, g := range f.goals {
		if g.Phase != string(goalPhaseRetired) {
			out = append(out, cloneGoal(g))
		}
	}
	return out, nil
}

func (f *fakeGoalManager) UpdateGoal(_ context.Context, g *GoalRecord) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.goals[g.ID]; !ok {
		return ErrGoalToolNotFound
	}
	g.Revision++ // simulate the store's CAS bump
	f.goals[g.ID] = cloneGoal(g)
	return nil
}

func cloneGoal(g *GoalRecord) *GoalRecord {
	c := *g
	if g.BlockedReason != nil {
		r := *g.BlockedReason
		c.BlockedReason = &r
	}
	return &c
}

func ownerOf(id string) []string { return []string{id} }

func goalOwnerStatic(id string) goalOwner {
	return func(context.Context) string { return id }
}

// setAutonomous restores the round knob after each mutation.
func setAutonomous(t *testing.T, v bool) {
	t.Helper()
	SetGoalAutonomousRound(v)
	t.Cleanup(func() { SetGoalAutonomousRound(false) })
}

func TestGoalTools_RegisteredThroughProviderSeam(t *testing.T) {
	// The "goal" provider must be present in the init-populated factories.
	if _, ok := toolprov.Factory("goal"); !ok {
		t.Fatal("goal provider missing from toolprov factories")
	}
	// Unbound (default): RegisterAll fails closed, nothing arms.
	if _, err := toolprov.RegisterAll(tools.NewRegistry(nil), []string{"goal"}); err == nil {
		t.Fatal("RegisterAll(goal) with an unbound factory succeeded; default must fail closed")
	}

	// Bind a manager and walk the provider end-to-end into a fresh registry.
	SetGoalProvider(newFakeGoalManager(), goalOwnerStatic("emp_1"))
	reg := tools.NewRegistry(nil)
	if _, err := toolprov.RegisterAll(reg, []string{"goal"}); err != nil {
		t.Fatalf("RegisterAll(goal) with a bound manager: %v", err)
	}
	for _, name := range []string{"goal_get", "goal_create", "goal_update"} {
		if reg.Get(name) == nil {
			t.Errorf("tool %q missing from the registry after the goal provider walk", name)
		}
	}
	ResetGoalProviderForTest()

	// Typed-nil guard: a nil manager (bare or boxed) must leave the factory
	// unbound so the provider fails closed, never arming against a nil store.
	var f *fakeGoalManager // typed nil
	SetGoalProvider(f, nil)
	if _, err := toolprov.RegisterAll(tools.NewRegistry(nil), []string{"goal"}); err == nil {
		t.Fatal("RegisterAll(goal) with a typed-nil manager succeeded; must fail closed")
	}
	SetGoalProvider(nil, nil)
	if _, err := toolprov.RegisterAll(tools.NewRegistry(nil), []string{"goal"}); err == nil {
		t.Fatal("RegisterAll(goal) with a nil manager succeeded; must fail closed")
	}
	ResetGoalProviderForTest()
}

func TestGoalGet_NoGoalReturnsNull(t *testing.T) {
	tool := NewGoalGetTool(newFakeGoalManager(), goalOwnerStatic("emp_1"))
	got, err := tool.Execute(context.Background(), nil)
	if err != nil {
		t.Fatalf("goal_get errored: %v", err)
	}
	if !tool.IsReadOnly(nil) || !tool.IsConcurrencySafe(nil) {
		t.Error("goal_get must be read-only and concurrency-safe")
	}
	m, ok := got.(map[string]any)
	if !ok || m["goal"] != nil {
		t.Errorf("goal_get with no goal = %#v, want {\"goal\": null}", got)
	}
}

func TestGoalGet_ReturnsCompactValue(t *testing.T) {
	m := newFakeGoalManager()
	g, _ := m.CreateGoal(context.Background(), GoalCreateSpec{Owner: "emp_1", Objective: "keep tests green", MaxRounds: 5})
	tool := NewGoalGetTool(m, goalOwnerStatic("emp_1"))
	got, err := tool.Execute(context.Background(), nil)
	if err != nil {
		t.Fatalf("goal_get errored: %v", err)
	}
	vv, ok := got.(map[string]any)["goal"].(goalValue)
	if !ok {
		t.Fatalf("goal_get goal field = %#v, want goalValue", got)
	}
	if vv.ID != g.ID || vv.Objective != "keep tests green" || vv.Phase != "active" {
		t.Errorf("goal_get compact value mismatch: %+v", vv)
	}
	if vv.Revision != 1 || vv.RoundsStarted != 0 || vv.MaxRounds != 5 {
		t.Errorf("goal_get autonomy fields mismatch: %+v", vv)
	}
	if !vv.Activation {
		t.Error("goal_get activation must be true for an armed goal")
	}
}

func TestGoalCreate_RequiresDirectHuman(t *testing.T) {
	tool := NewGoalCreateTool(newFakeGoalManager(), goalOwnerStatic("emp_1"))

	// Autonomous round (no direct human): refused with an authority message.
	setAutonomous(t, true)
	res, err := tool.Execute(context.Background(), map[string]any{"objective": "probe objective"})
	if err != nil {
		t.Fatalf("goal_create errored: %v", err)
	}
	msg := strings.ToLower(resultText(res))
	if !strings.Contains(msg, "authority") && !strings.Contains(msg, "denied") &&
		!strings.Contains(msg, "requires") && !strings.Contains(msg, "permission") {
		t.Errorf("goal_create not refused without direct human authority: %s", resultText(res))
	}
	if !errors.Is(toErr(res), ErrGoalToolDirectHumanRequired) {
		t.Errorf("goal_create refusal must wrap ErrGoalToolDirectHumanRequired, got %T", toErr(res))
	}
}

func TestGoalCreate_DirectHumanPersists(t *testing.T) {
	setAutonomous(t, false)
	m := newFakeGoalManager()
	tool := NewGoalCreateTool(m, goalOwnerStatic("emp_1"))
	res, err := tool.Execute(context.Background(), map[string]any{"objective": "ship the convergence", "max_rounds": float64(4)})
	if err != nil {
		t.Fatalf("goal_create errored: %v", err)
	}
	vv, ok := res.(map[string]any)["goal"].(goalValue)
	if !ok {
		t.Fatalf("goal_create result = %#v, want {"+"goal: goalValue}", res)
	}
	if vv.Objective != "ship the convergence" || vv.MaxRounds != 4 || vv.Revision != 1 {
		t.Errorf("goal_create persisted value mismatch: %+v", vv)
	}
	// Durable: goal_get sees it.
	dup := NewGoalGetTool(m, goalOwnerStatic("emp_1"))
	if seen, _ := dup.Execute(context.Background(), nil); seen.(map[string]any)["goal"] == nil {
		t.Error("created goal not visible to a later goal_get")
	}
}

func TestGoalUpdate_DirectHumanActionsAndCAS(t *testing.T) {
	setAutonomous(t, false)
	m := newFakeGoalManager()
	g, _ := m.CreateGoal(context.Background(), GoalCreateSpec{Owner: "emp_1", Objective: "orig", MaxRounds: 3})
	tool := NewGoalUpdateTool(m, goalOwnerStatic("emp_1"))

	// edit bumps the objective and revision.
	res, _ := tool.Execute(context.Background(), map[string]any{
		"goal_id": g.ID, "revision": float64(1), "action": "edit", "objective": "edited"})
	vv := res.(map[string]any)["goal"].(goalValue)
	if vv.Objective != "edited" || vv.Revision != 2 {
		t.Errorf("edit result mismatch: %+v", vv)
	}

	// Stale revision is refused.
	res = ctxArgs(tool.Execute, context.Background(), map[string]any{
		"goal_id": g.ID, "revision": float64(1), "action": "edit", "objective": "again"})
	if !errors.Is(toErr(res), ErrGoalToolStaleRevision) {
		t.Errorf("stale revision not gated, got %T %s", toErr(res), resultText(res))
	}

	// pause (requires direct human) -> paused, disarmed.
	res, _ = tool.Execute(context.Background(), map[string]any{
		"goal_id": g.ID, "revision": float64(2), "action": "pause"})
	if vv := res.(map[string]any)["goal"].(goalValue); vv.Phase != "paused" || vv.Activation {
		t.Errorf("pause result mismatch: %+v", vv)
	}

	// resume -> active, armed.
	res, _ = tool.Execute(context.Background(), map[string]any{
		"goal_id": g.ID, "revision": float64(3), "action": "resume"})
	if vv := res.(map[string]any)["goal"].(goalValue); vv.Phase != "active" || !vv.Activation {
		t.Errorf("resume result mismatch: %+v", vv)
	}

	// edit/pause/resume refused during an autonomous round.
	setAutonomous(t, true)
	res = ctxArgs(tool.Execute, context.Background(), map[string]any{
		"goal_id": g.ID, "revision": float64(4), "action": "pause"})
	if !errors.Is(toErr(res), ErrGoalToolDirectHumanRequired) {
		t.Errorf("pause in autonomous round not gated: %T %s", toErr(res), resultText(res))
	}
	setAutonomous(t, false)
}

func TestGoalUpdate_CompleteAllowedInAutonomous(t *testing.T) {
	setAutonomous(t, true)
	m := newFakeGoalManager()
	g, _ := m.CreateGoal(context.Background(), GoalCreateSpec{Owner: "emp_1", Objective: "done work", MaxRounds: 3})
	tool := NewGoalUpdateTool(m, goalOwnerStatic("emp_1"))
	res, err := tool.Execute(context.Background(), map[string]any{
		"goal_id": g.ID, "revision": float64(1), "action": "complete"})
	if err != nil {
		t.Fatalf("goal_update complete errored: %v", err)
	}
	if vv := res.(map[string]any)["goal"].(goalValue); vv.Phase != "retired" || vv.Activation {
		t.Errorf("complete result mismatch: %+v", vv)
	}
	// Retired goals drop out of goal_get.
	dup := NewGoalGetTool(m, goalOwnerStatic("emp_1"))
	if seen, _ := dup.Execute(context.Background(), nil); seen.(map[string]any)["goal"] != nil {
		t.Error("retired goal still returned by goal_get")
	}
}

func TestGoalUpdate_BlockedThreshold(t *testing.T) {
	m := newFakeGoalManager()
	// Goal with roundsStarted=1 (below threshold 3).
	g := &GoalRecord{ID: "goal_a", Objective: "blocker", Phase: string(goalPhaseActive), RoundsStarted: 1, Revision: 1, Armed: true}
	m.goals[g.ID] = cloneGoal(g)
	tool := NewGoalUpdateTool(m, goalOwnerStatic("emp_1"))

	// Empty reason always refused.
	res := ctxArgs(tool.Execute, context.Background(), map[string]any{
		"goal_id": g.ID, "revision": float64(1), "action": "blocked", "blocked_reason": " "})
	if !errors.Is(toErr(res), ErrGoalToolBlockedReasonRequired) {
		t.Errorf("blocked with empty reason not gated: %T %s", toErr(res), resultText(res))
	}

	// Autonomous self-block before threshold: rejected.
	setAutonomous(t, true)
	res = ctxArgs(tool.Execute, context.Background(), map[string]any{
		"goal_id": g.ID, "revision": float64(1), "action": "blocked", "blocked_reason": "early"})
	if !errors.Is(toErr(res), ErrGoalToolBlockThreshold) {
		t.Errorf("autonomous block before threshold not gated: %T %s", toErr(res), resultText(res))
	}

	// At threshold (roundsStarted=3): autonomous block succeeds.
	g = &GoalRecord{ID: "goal_b", Objective: "at cap", Phase: string(goalPhaseActive), RoundsStarted: 3, Revision: 1, Armed: true}
	m.goals[g.ID] = cloneGoal(g)
	res, _ = tool.Execute(context.Background(), map[string]any{
		"goal_id": g.ID, "revision": float64(1), "action": "blocked", "blocked_reason": "reached cap"})
	vv, ok := res.(map[string]any)["goal"].(goalValue)
	if !ok || vv.Phase != "blocked" || vv.BlockedReason == nil || *vv.BlockedReason != "reached cap" {
		t.Errorf("blocked at threshold result mismatch: %+v", res)
	}
	setAutonomous(t, false)

	// Direct human may block at any round (operator authority).
	g = &GoalRecord{ID: "goal_c", Objective: "operator", Phase: string(goalPhaseActive), RoundsStarted: 0, Revision: 1, Armed: true}
	m.goals[g.ID] = cloneGoal(g)
	res, _ = tool.Execute(context.Background(), map[string]any{
		"goal_id": g.ID, "revision": float64(1), "action": "blocked", "blocked_reason": "operator call"})
	if vv := res.(map[string]any)["goal"].(goalValue); vv.Phase != "blocked" {
		t.Errorf("direct-human block at round 0 failed: %s", resultText(res))
	}
}

func TestGoalUpdate_UnknownGoalOrAction(t *testing.T) {
	m := newFakeGoalManager()
	tool := NewGoalUpdateTool(m, goalOwnerStatic("emp_1"))
	res := ctxArgs(tool.Execute, context.Background(), map[string]any{
		"goal_id": "missing", "revision": float64(1), "action": "edit", "objective": "x"})
	if !errors.Is(toErr(res), ErrGoalToolNotFound) {
		t.Errorf("unknown goal not surfaced: %T %s", toErr(res), resultText(res))
	}
	g, _ := m.CreateGoal(context.Background(), GoalCreateSpec{Owner: "emp_1", Objective: "o", MaxRounds: 2})
	res = ctxArgs(tool.Execute, context.Background(), map[string]any{
		"goal_id": g.ID, "revision": float64(1), "action": "bogus"})
	if strings.Contains(strings.ToLower(resultText(res)), "unknown action") == false {
		t.Errorf("unknown action not rejected: %s", resultText(res))
	}
}

// resultText extracts the error text from a tool result; "" when success.
func resultText(res any) string {
	switch r := res.(type) {
	case *tools.ToolResult:
		return r.Error
	case tools.ToolResult:
		return r.Error
	default:
		return fmt.Sprintf("%v", res)
	}
}

// toErr extracts the typed error from an error result, nil otherwise.
func toErr(res any) error {
	switch r := res.(type) {
	case *tools.ToolResult:
		return r.Err
	case tools.ToolResult:
		return r.Err
	default:
		return nil
	}
}

// ctxArgs adapts an Execute with args to a plain return for assertion reuse.
func ctxArgs(fn func(context.Context, map[string]any) (any, error), ctx context.Context, args map[string]any) any {
	res, _ := fn(ctx, args)
	return res
}
