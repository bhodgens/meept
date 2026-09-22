package agent

// Feature-specific validation for the session-state quickplan upgrade
// (owner decision 2026-09-20: enable only after feature-specific tests
// validate it). These exercise the FULL maybeUpgradeSessionQuickplan path
// with a REAL plan store seeded with an approved plan — the wiring the
// predicate unit tests skip. The live half runs on the scratch rig.

import (
	"context"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/plan"
)

// buildSessionUpgradeHarness seeds a plan store with an approved plan for
// sessionID and returns a dispatcher with planManager wired and the knob ON.
func buildSessionUpgradeHarness(t *testing.T, sessionID string, seedPlan bool) *Dispatcher {
	t.Helper()
	logger := slog.New(slog.DiscardHandler)
	store, err := plan.NewSQLiteStore(filepath.Join(t.TempDir(), "plans.db"), logger)
	if err != nil {
		t.Fatalf("plan store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	pm := plan.NewPlanManager(store, nil, config.PlansConfig{}, nil, logger)
	ctx := context.Background()
	if seedPlan {
		p, err := pm.CreatePlan(ctx, "t", "test plan", "", "", sessionID)
		if err != nil {
			t.Fatalf("plan create: %v", err)
		}
		// Drive the state transition through the store: SubmitPlan
		// auto-approves only when RequireApproval is false, and approval
		// runs Synthesize which needs a TaskCreator this bare fixture does
		// not wire (same exclusion as the worker's SessionEvidence tests).
		if err := store.SetPlanState(ctx, p.ID, plan.StatePendingApproval); err != nil {
			t.Fatalf("plan submit: %v", err)
		}
		if err := store.SetPlanState(ctx, p.ID, plan.StateApproved); err != nil {
			t.Fatalf("plan approve: %v", err)
		}
	}
	d := &Dispatcher{stats: newTestStats(), logger: logger}
	d.planManager = pm
	d.classifierConfig = classifierGateConfig{SessionStateUpgrade: true}
	return d
}

// TestSessionStateUpgrade_Validation_FullPathApprovedPlan drives the real
// upgrade path: boundary-lane code verdict + cue input + approved-plan
// evidence + knob ON -> quickplan with the session-state method recorded.
func TestSessionStateUpgrade_Validation_FullPathApprovedPlan(t *testing.T) {
	d := buildSessionUpgradeHarness(t, "sess-upg", true)
	intent := &Intent{
		Type:       string(IntentCode),
		Confidence: 0.9,
		AgentType:  "coder",
		Method:     "llm",
	}
	got := d.maybeUpgradeSessionQuickplan(context.Background(), intent,
		cueInput, "sess-upg", true)
	if got.Type != string(IntentQuickPlan) {
		t.Fatalf("upgrade with approved-plan evidence: got %q (method %q), want quickplan", got.Type, got.Method)
	}
	if got.Method != "quickplan_session_upgrade" {
		t.Errorf("method = %q, want quickplan_session_upgrade", got.Method)
	}
	if !got.RequiresPlanning {
		t.Error("upgraded quickplan must require planning")
	}
}

// TestSessionStateUpgrade_Validation_NoPlanPassesThrough pins the negative
// half through the same full path: same input, same knob, NO plan evidence —
// the boundary-lane verdict passes through unchanged.
func TestSessionStateUpgrade_Validation_NoPlanPassesThrough(t *testing.T) {
	d := buildSessionUpgradeHarness(t, "sess-no-plan", false)
	intent := &Intent{
		Type:       string(IntentCode),
		Confidence: 0.9,
		AgentType:  "coder",
		Method:     "llm",
	}
	got := d.maybeUpgradeSessionQuickplan(context.Background(), intent,
		cueInput, "sess-no-plan", true)
	if got.Type != string(IntentCode) {
		t.Fatalf("no-evidence session must not upgrade: got %q (method %q)", got.Type, got.Method)
	}
}

// TestSessionStateUpgrade_Validation_KnobOffInert pins default-off through
// the full path with evidence present: knob off = original intent untouched.
func TestSessionStateUpgrade_Validation_KnobOffInert(t *testing.T) {
	d := buildSessionUpgradeHarness(t, "sess-upg-knoboff", true)
	intent := &Intent{
		Type:       string(IntentDebug),
		Confidence: 0.9,
		AgentType:  "debugger",
		Method:     "llm",
	}
	got := d.maybeUpgradeSessionQuickplan(context.Background(), intent,
		cueInput, "sess-upg-knoboff", false)
	if got.Type != string(IntentDebug) || got.Method != "llm" {
		t.Fatalf("knob off with evidence must be inert: got %q (method %q)", got.Type, got.Method)
	}
}
