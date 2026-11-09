package employee

// Human-approval goal gate park/resume (audit HITL-1).
//
// Coverage:
//   - an approval gate parks the Reflect turn through the injected seam
//     (GateKind "goal") instead of completing the goal;
//   - the park seam unwired → the round degrades to at_risk (never a
//     silent completion);
//   - ResumeGoalGateApproval re-runs Reflect with the approval short-
//     circuit: the gate passes and the recorded result's health verdict
//     lands;
//   - approval gates survive goal-store persistence (Type is the
//     discriminator; empty Command does not drop the gate).

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/bot"
	"github.com/caimlas/meept/internal/llm"
)

// captureGatePark is a test double of the daemon's GateParker.ParkGate
// wrapper: records GateParkData and reports parked.
type captureGatePark struct {
	parked []agent.GateParkData
}

func (c *captureGatePark) park(data agent.GateParkData) bool {
	c.parked = append(c.parked, data)
	return true
}

// healthyReflectContent is the reflect LLM answer used so the approval
// gate branch is reached (Reflect only asks the gate when health would be
// healthy).
const healthyReflectContent = `{"health":"healthy","reasoning":"ok"}`

// newApprovalGateLoop builds a loop over a real goal store whose active
// goal carries an approval-only gate, with the injected park seam.
func newApprovalGateLoop(t *testing.T) (*GoalLoop, *GoalStore, *captureGatePark, *Goal) {
	t.Helper()
	store, err := NewGoalStore(filepath.Join(t.TempDir(), "goals.db"), nil)
	if err != nil {
		t.Fatalf("goal store: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	// Satisfy the employee_goals FK (bot_definitions) with a seed row.
	ctx := context.Background()
	if _, err := store.db.ExecContext(ctx,
		`INSERT INTO bot_definitions (id, data, created_at, updated_at) VALUES ('emp-gate-1', '{}', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatalf("seed bot_definitions: %v", err)
	}
	goal := &Goal{
		EmployeeID: "emp-gate-1",
		Title:      "ship the thing",
		Mandate:    "ship it",
		State:      GoalActive,
		Source:     SourceUser,
		Gate:       &GateConfig{Type: GateTypeApproval},
	}
	if err := store.Create(ctx, goal); err != nil {
		t.Fatalf("create goal: %v", err)
	}

	reflector := newStubReflector()
	// Both the reflect call (and any assess re-entry) answer healthy.
	reflector.default_ = &llm.Response{Content: healthyReflectContent}

	loop := NewGoalLoop("emp-gate-1", testTier2Constitution(), store, nil).
		WithReflector(reflector)
	cap := &captureGatePark{}
	loop.SetGoalGateParker(cap.park)
	return loop, store, cap, goal
}

func TestReflect_ApprovalGateParksTurn(t *testing.T) {
	loop, _, cap, goal := newApprovalGateLoop(t)

	result := &bot.BotExecutionResult{
		BotID:   "emp-gate-1",
		Success: true,
		Output:  "done",
	}
	health, err := loop.Reflect(context.Background(), PlanRef{ID: "plan-g1", Prompt: "p"}, result)
	if err != nil {
		t.Fatalf("Reflect error on approval park: %v", err)
	}
	if health != GoalUnknown {
		t.Fatalf("health = %v, want GoalUnknown while parked", health)
	}
	if len(cap.parked) != 1 {
		t.Fatalf("parked gate rows = %d, want 1", len(cap.parked))
	}
	data := cap.parked[0]
	if data.GateKind != "goal" {
		t.Fatalf("gate_kind = %q, want goal", data.GateKind)
	}
	if data.GateID != goal.ID {
		t.Fatalf("gate_id = %q, want %q", data.GateID, goal.ID)
	}
	if planID, _ := data.TurnReentry["plan_id"].(string); planID != "plan-g1" {
		t.Fatalf("turn_reentry plan_id = %v, want plan-g1", data.TurnReentry["plan_id"])
	}
}

func TestReflect_ApprovalGateUnwiredDegradesToAtRisk(t *testing.T) {
	loop, _, cap, _ := newApprovalGateLoop(t)
	// Refuse the park (the daemon's checkpointing-off shape: the wrapper
	// returns false). SetGoalGateParker(nil) is a typed-nil no-op, so the
	// unwired path is exercised by a refusing fn.
	loop.SetGoalGateParker(func(agent.GateParkData) bool { return false })

	result := &bot.BotExecutionResult{BotID: "emp-gate-1", Success: true}
	health, err := loop.Reflect(context.Background(), PlanRef{ID: "plan-g2"}, result)
	if err != nil {
		t.Fatalf("Reflect error: %v", err)
	}
	if health != GoalAtRisk {
		t.Fatalf("health = %v, want GoalAtRisk when the park seam is unwired", health)
	}
	if len(cap.parked) != 0 {
		t.Fatalf("park seam refused but rows were recorded (%d)", len(cap.parked))
	}
}

func TestResumeGoalGateApproval_ReentersReflectAndPassesGate(t *testing.T) {
	loop, _, cap, _ := newApprovalGateLoop(t)

	// Park first (the parked round).
	result := &bot.BotExecutionResult{BotID: "emp-gate-1", Success: true, Output: "done"}
	if _, err := loop.Reflect(context.Background(), PlanRef{ID: "plan-g3", Prompt: "p"}, result); err != nil {
		t.Fatalf("parking reflect error: %v", err)
	}
	if len(cap.parked) != 1 {
		t.Fatalf("gate did not park")
	}

	// Approval resume: the daemon drains the row (out of scope here) and
	// re-enters with the persisted re-entry payload.
	loop.ResumeGoalGateApproval(context.Background(), map[string]any{
		"plan_id": "plan-g3",
		"prompt":  "p",
		"result":  result,
	})
	// No second park on the approved re-entry.
	if len(cap.parked) != 1 {
		t.Fatalf("approved re-entry parked again (%d rows)", len(cap.parked))
	}
	// The completion verdict landed: the recorded healthy result advances
	// the success streak.
	if loop.ConsecutiveFailures() != 0 {
		t.Fatalf("failures = %d, want 0 after approved completion", loop.ConsecutiveFailures())
	}
}

func TestGoalStore_ApprovalGatePersistsRoundTrip(t *testing.T) {
	_, store, _, goal := newApprovalGateLoop(t)

	got, err := store.Get(context.Background(), goal.ID)
	if err != nil {
		t.Fatalf("get goal: %v", err)
	}
	if got.Gate == nil {
		t.Fatalf("approval gate lost in storage round-trip")
	}
	if !got.Gate.IsApproval() {
		t.Fatalf("gate type = %q, want approval", got.Gate.Type)
	}
}

func TestGoalStore_ApprovalOnlyGateSurvivesUpdate(t *testing.T) {
	_, store, _, goal := newApprovalGateLoop(t)

	g, err := store.Get(context.Background(), goal.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	g.Gate = &GateConfig{Type: GateTypeApproval}
	if err := store.Update(context.Background(), g); err != nil {
		t.Fatalf("update: %v", err)
	}
	g2, err := store.Get(context.Background(), goal.ID)
	if err != nil {
		t.Fatalf("re-get: %v", err)
	}
	if g2.Gate == nil || !g2.Gate.IsApproval() {
		t.Fatalf("approval-only gate dropped by Update/scan")
	}
}

func TestGateConfig_ApprovalHelpers(t *testing.T) {
	if !(GateConfig{Type: GateTypeApproval}).IsApproval() {
		t.Fatalf("IsApproval false for approval type")
	}
	if (GateConfig{Command: "go test"}).IsApproval() {
		t.Fatalf("IsApproval true for plain command gate")
	}
	if got := (GateConfig{Type: GateTypeApproval, Command: "rm -rf /"}).EffectiveCommand(); got != "" {
		t.Fatalf("EffectiveCommand = %q for approval gate, want empty", got)
	}
	if got := (GateConfig{Command: "go test"}).EffectiveCommand(); got != "go test" {
		t.Fatalf("EffectiveCommand = %q, want go test", got)
	}
}

func TestGoalGate_ParkDataRoundTripThroughSharedFormat(t *testing.T) {
	// The goal kind rides the ONE checkpoint format (agent.GateParkData),
	// not a side channel.
	data := agent.GateParkData{
		GateKind:  "goal",
		GateID:    "goal_x",
		SessionID: "session-1",
		TurnReentry: map[string]any{
			"plan_id": "plan-9",
		},
		RequestedAt: time.Now().UTC(),
	}
	raw, err := json.Marshal(data)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	rec := agent.ParkedTurnRecord{TurnPayload: raw}
	decoded, key, err := agent.DecodeGatePark(rec)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if decoded.GateKind != "goal" || decoded.GateID != "goal_x" {
		t.Fatalf("round trip mismatch: %+v", decoded)
	}
	if key != "goal|goal_x" {
		t.Fatalf("persistence key = %q, want goal|goal_x", key)
	}
}
