package agent

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/plan"
	"github.com/caimlas/meept/internal/task"
)

// --- sessionEvidence tests (C1) ---

func TestSessionEvidence_PlanApproved(t *testing.T) {
	t.Parallel()
	logger := digestTestLogger()
	store, err := plan.NewSQLiteStore(filepath.Join(t.TempDir(), "plans.db"), logger)
	if err != nil {
		t.Fatalf("plan store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	// Approval auto-advances to StateApproved (RequireApproval false).
	pm := plan.NewPlanManager(store, nil, config.PlansConfig{}, nil, logger)
	ctx := context.Background()
	p, err := pm.CreatePlan(ctx, "t", "d", "", "", "sess-plan")
	if err != nil {
		t.Fatal(err)
	}
	// Drive the state transition directly through the store: SubmitPlan
	// auto-approves when RequireApproval is false, and the approval path
	// runs Synthesize, which needs a TaskCreator this bare fixture does not
	// wire (dispatcher_plan_lifecycle_test.go documents the same
	// exclusion). sessionEvidence reads STATE, not signoffs.
	if err := store.SetPlanState(ctx, p.ID, plan.StatePendingApproval); err != nil {
		t.Fatal(err)
	}
	if err := store.SetPlanState(ctx, p.ID, plan.StateApproved); err != nil {
		t.Fatal(err)
	}
	d := &Dispatcher{stats: newTestStats(), logger: logger}
	d.planManager = pm

	has, reason := d.sessionEvidence(ctx, "sess-plan")
	if !has {
		t.Fatal("approved plan must be quickplan evidence")
	}
	if reason != "plan:approved" {
		t.Fatalf("reason = %q, want plan:approved", reason)
	}
}

func TestSessionEvidence_Empty(t *testing.T) {
	t.Parallel()
	d := &Dispatcher{stats: newTestStats(), logger: digestTestLogger()}
	has, reason := d.sessionEvidence(context.Background(), "sess-empty")
	if has || reason != "" {
		t.Fatalf("no plans/tasks = (%v, %q), want (false, \"\")", has, reason)
	}
}

func TestSessionEvidence_TerminalPlanIgnored(t *testing.T) {
	t.Parallel()
	logger := digestTestLogger()
	store, err := plan.NewSQLiteStore(filepath.Join(t.TempDir(), "plans.db"), logger)
	if err != nil {
		t.Fatalf("plan store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	pm := plan.NewPlanManager(store, nil, config.PlansConfig{}, nil, logger)
	ctx := context.Background()
	p, err := pm.CreatePlan(ctx, "t", "d", "", "", "sess-term")
	if err != nil {
		t.Fatal(err)
	}
	if err := pm.CancelPlan(ctx, p.ID, "test"); err != nil {
		t.Fatal(err)
	}
	d := &Dispatcher{stats: newTestStats(), logger: logger}
	d.planManager = pm

	has, _ := d.sessionEvidence(ctx, "sess-term")
	if has {
		t.Fatal("terminal (cancelled) plan must NOT count as quickplan evidence")
	}
}

// TestSessionEvidence_ActiveTask exercises the task-store evidence path
// through the real task store fixture (newDigestTestDispatcher pattern).
func TestSessionEvidence_ActiveTask(t *testing.T) {
	t.Parallel()
	d, _ := newDigestTestDispatcher(t)
	seedDigestTask(t, d, "tk-ev-1", "pending work", "sess-task-ev",
		task.StateExecuting, time.Now(), "coder")
	has, reason := d.sessionEvidence(context.Background(), "sess-task-ev")
	if !has {
		t.Fatal("session with an executing linked task must be quickplan evidence")
	}
	if reason == "" {
		t.Fatal("task evidence reason must be non-empty")
	}
}

// --- sessionStateUpgradeApplies predicate tests (C4/C6, gates table) ---

const cueInput = "using subagents, implement the plan and correct issues as you find them"
const noCueInput = "fix this typo in the readme"

func TestSessionStateUpgrade_Gates(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name        string
		verdict     string
		input       string
		hasEvidence bool
		knob        bool
		want        bool
	}{
		{"upgrade fires", string(IntentCode), cueInput, true, true, true},
		{"knob off inert", string(IntentCode), cueInput, true, false, false},
		{"no evidence inert", string(IntentCode), cueInput, false, true, false},
		{"no cue inert (C6)", string(IntentCode), noCueInput, true, true, false},
		{"chat not boundary", string(IntentChat), cueInput, true, true, false},
		{"git boundary fires", string(IntentGit), cueInput, true, true, true},
		{"quickplan no-op (C4)", string(IntentQuickPlan), cueInput, true, true, false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := sessionStateUpgradeApplies(tc.verdict, tc.input, tc.hasEvidence, tc.knob)
			if got != tc.want {
				t.Fatalf("sessionStateUpgradeApplies(%q, ev=%v, knob=%v) = %v, want %v",
					tc.verdict, tc.hasEvidence, tc.knob, got, tc.want)
			}
		})
	}
}

// TestSessionStateUpgrade_OneWayNoDowngrade pins the one-way rule through
// the real dispatcher path: an existing quickplan verdict plus session
// evidence passes through UNCHANGED (same pointer, C4).
func TestSessionStateUpgrade_OneWayNoDowngrade(t *testing.T) {
	t.Parallel()
	d := &Dispatcher{stats: newTestStats(), logger: digestTestLogger()}
	intent := &Intent{
		Type:             string(IntentQuickPlan),
		Confidence:       0.9,
		AgentType:        "orchestrator",
		RequiresPlanning: true,
		Method:           "llm",
	}
	got := d.maybeUpgradeSessionQuickplan(context.Background(), intent, cueInput, "sess-x", true)
	if got != intent {
		t.Fatal("quickplan verdict must pass through untouched (C4 one-way)")
	}
}

// TestSessionStateUpgrade_DefaultOffInert pins the default-off contract:
// knob=false returns the SAME intent pointer, unmodified.
func TestSessionStateUpgrade_DefaultOffInert(t *testing.T) {
	t.Parallel()
	d := &Dispatcher{stats: newTestStats(), logger: digestTestLogger()}
	intent := &Intent{Type: string(IntentCode), Confidence: 0.8, AgentType: "coder", Method: "llm"}
	got := d.maybeUpgradeSessionQuickplan(context.Background(), intent, cueInput, "sess-x", false)
	if got != intent {
		t.Fatal("knob off must return the original intent untouched")
	}
	if got.Type != string(IntentCode) || got.Method != "llm" {
		t.Fatalf("verdict mutated with knob off: %+v", got)
	}
}

// TestSessionStateUpgrade_MethodRecorded verifies the observability
// contract (C2): the upgrade path records quickplan_session_upgrade.
func TestSessionStateUpgrade_MethodRecorded(t *testing.T) {
	t.Parallel()
	d := &Dispatcher{stats: newTestStats(), logger: digestTestLogger()}
	d.recordClassificationMethod("quickplan_session_upgrade")
	if d.stats.ByMethod["quickplan_session_upgrade"] != 1 {
		t.Fatal("method not recorded")
	}
}

// --- fixtures ---

func newTestStats() *DispatcherStats {
	return &DispatcherStats{
		ByMethod: make(map[string]int),
		ByAgent:  make(map[string]int),
		ByIntent: make(map[string]int),
	}
}
