package daemon

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/plan"
	"github.com/caimlas/meept/internal/skills/lifecycle"
	"github.com/caimlas/meept/internal/task"
)

// ---------------------------------------------------------------------------
// Approval-wiring fixtures (leaf 03 Task 2)
// ---------------------------------------------------------------------------

// stubWiringTaskCreator satisfies plan.TaskCreator for the wiring tests so
// ApprovePlan's trailing Synthesize succeeds the way it does in production
// (which wires newTaskCreatorAdapter over task.Registry). It records the
// tasks created without exercising the real task registry.
type stubWiringTaskCreator struct{}

func (stubWiringTaskCreator) CreateTask(context.Context, string, string) (*task.Task, error) {
	return task.NewTask("wiring-stub", "wiring stub task"), nil
}

func (stubWiringTaskCreator) CreateTaskStep(_ context.Context, taskID, description string, sequence int) (*task.TaskStep, error) {
	return task.NewTaskStep(taskID, description, sequence), nil
}

func (stubWiringTaskCreator) UpdateTaskStep(context.Context, *task.TaskStep) error { return nil }

func (stubWiringTaskCreator) LinkSession(context.Context, string, string) error { return nil }

func (stubWiringTaskCreator) SetTaskJobCount(context.Context, string, int) error { return nil }

func (stubWiringTaskCreator) ScheduleSteps(context.Context, string) error { return nil }

// writeTierFixtureSkillForWiring creates <tierDir>/<name>/SKILL.md with valid
// frontmatter (daemon-package twin of the lifecycle package's fixture
// helper; helpers are not shared across packages).
func writeTierFixtureSkillForWiring(t *testing.T, tierDir, name string) string {
	t.Helper()
	dir := filepath.Join(tierDir, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create fixture skill dir: %v", err)
	}
	content := "---\nname: " + name + "\ndescription: approval wiring fixture skill\n---\n\nbody of " + name + "\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatalf("write fixture SKILL.md: %v", err)
	}
	return content
}

// buildApprovalWiringComponents constructs the minimal component set the
// approval bridge needs, through the REAL wiring functions: the evolver plan
// sink manager (newEvolverPlanManager) and the evolver itself, with a live
// message bus so ApprovePlan's plan.approved event reaches the bridge.
func buildApprovalWiringComponents(t *testing.T, fixtureHome string) (*Components, *plan.PlanManager) {
	t.Helper()

	sink := filepath.Join(fixtureHome, ".meept", "plans", "evolver")
	cfg := config.DefaultConfig()
	cfg.Skills.Evolver.Enabled = true
	cfg.Skills.Evolver.PlanDir = sink
	// The real gate: SubmitPlan parks the plan in pending_approval and only
	// an explicit ApprovePlan call proceeds. (With RequireApproval=false the
	// auto-approve would trigger Synthesize, which needs a task creator.)
	cfg.Plans.Approval.RequireApproval = true

	msgBus := bus.New(nil, slog.Default())
	c := &Components{
		Config: cfg,
		Logger: slog.Default(),
	}
	c.ctx, c.cancel = context.WithCancel(context.Background())
	c.msgBus = msgBus
	t.Cleanup(c.cancel)

	// Evolver-dedicated plan manager (the manager evolver plans are parked
	// in), built through the production wiring function with a stub task
	// creator injected — ApprovePlan's trailing Synthesize then succeeds the
	// way it does in the daemon (which wires the real task-registry adapter).
	sinkStore, err := plan.NewSQLiteStore(filepath.Join(t.TempDir(), "sink.db"), slog.Default())
	if err != nil {
		t.Fatalf("sink store: %v", err)
	}
	t.Cleanup(func() { _ = sinkStore.Close() })
	sinkMgr, err := c.newEvolverPlanManagerWithCreator(
		sinkStore, cfg, slog.Default(), stubWiringTaskCreator{})
	if err != nil {
		t.Fatalf("newEvolverPlanManagerWithCreator: %v", err)
	}
	c.EvolverPlanManager = sinkMgr

	// Tier fixture: the skill under archive lives in the claude tier so the
	// actuator exercises leaf-02 resolution.
	claudeTier := filepath.Join(fixtureHome, ".claude", "skills")
	writeTierFixtureSkillForWiring(t, claudeTier, "approval-wired-skill")

	skillsDir := filepath.Join(t.TempDir(), "skills")
	c.SkillEvolver = lifecycle.NewEvolver(
		nil, nil, lifecycle.NewWriter(skillsDir, slog.Default()),
		nil, nil, lifecycle.NewVerifier(nil, slog.Default()),
		nil, sinkMgr, cfg.Skills.Evolver, slog.Default(),
	)
	return c, sinkMgr
}

// createSubmittedEvolverPlan creates an evolver-provenance plan through the
// sink manager, stamps provenance, and submits it to pending_approval — the
// exact lifecycle a real evolver plan goes through before a human approves.
func createSubmittedEvolverPlan(t *testing.T, mgr *plan.PlanManager, proposalID, action string) *plan.Plan {
	t.Helper()
	created, err := mgr.CreatePlan(context.Background(),
		"Skill evolution: archive approval-wired-skill",
		"Approval wiring fixture", "", "", "")
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	lifecycle.StampEvolverPlan(created, proposalID, action)
	if err := mgr.SubmitPlan(context.Background(), created.ID); err != nil {
		t.Fatalf("SubmitPlan: %v", err)
	}
	return created
}

// ---------------------------------------------------------------------------
// TestApprovalWiring: approved evolver plans reach the actuator
// ---------------------------------------------------------------------------

// TestApprovalWiring_EvolverPlanApprovalTriggersActuator is the core leaf-03
// wiring invariant: approving an evolver-provenance plan through the REAL
// approval path (PlanManager.ApprovePlan → plan.approved event → bridge →
// ApplyApprovedPlan) applies the skill action. Asserted via the applied side
// effect: the archived skill is gone from its tier and the plan file carries
// the durable applied marker.
func TestApprovalWiring_EvolverPlanApprovalTriggersActuator(t *testing.T) {
	fixtureHome := t.TempDir()
	t.Setenv("HOME", fixtureHome)
	t.Setenv("USERPROFILE", fixtureHome)

	c, mgr := buildApprovalWiringComponents(t, fixtureHome)

	if _, err := c.wireEvolverApprovalBridge(); err != nil {
		t.Fatalf("wireEvolverApprovalBridge: %v", err)
	}

	created := createSubmittedEvolverPlan(t, mgr,
		"evo-archive-approval-wired-skill-000042", "archive")

	if err := mgr.ApprovePlan(context.Background(), created.ID, "sess-1", "reviewer"); err != nil {
		t.Fatalf("ApprovePlan: %v", err)
	}

	// The approval MUST succeed even though synthesis fails (no task
	// creator) — an actuator-seam test must not depend on task wiring.
	if err := waitForCondition(2*time.Second, func() bool {
		data, err := os.ReadFile(created.FilePath) //nolint:gosec // fixture path
		return err == nil && strings.Contains(string(data), "\n- applied: ")
	}); err != nil {
		t.Fatalf("applied marker never landed on the plan file: %v", err)
	}

	// Skill archived out of its tier by the actuator (leaf 02 semantics).
	if _, err := os.Stat(filepath.Join(fixtureHome, ".claude", "skills", "approval-wired-skill")); !os.IsNotExist(err) {
		t.Errorf("skill still present in tier after approval (err=%v)", err)
	}
}

// TestApprovalWiring_HumanPlanApprovalUnaffected: a human-authored plan
// (no provenance) goes through plain approval — no actuator runs, so no
// applied marker is ever written onto it.
func TestApprovalWiring_HumanPlanApprovalUnaffected(t *testing.T) {
	fixtureHome := t.TempDir()
	t.Setenv("HOME", fixtureHome)
	t.Setenv("USERPROFILE", fixtureHome)

	c, mgr := buildApprovalWiringComponents(t, fixtureHome)

	if _, err := c.wireEvolverApprovalBridge(); err != nil {
		t.Fatalf("wireEvolverApprovalBridge: %v", err)
	}

	created, err := mgr.CreatePlan(context.Background(),
		"Human-authored wiring fixture", "no evolver provenance", "", "", "")
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	if err := mgr.SubmitPlan(context.Background(), created.ID); err != nil {
		t.Fatalf("SubmitPlan: %v", err)
	}
	if err := mgr.ApprovePlan(context.Background(), created.ID, "sess-1", "reviewer"); err != nil {
		t.Fatalf("ApprovePlan: %v", err)
	}

	// Give a (wrongly-wired) bridge a chance to misfire, then assert the
	// plan file is untouched.
	time.Sleep(100 * time.Millisecond)
	data, err := os.ReadFile(created.FilePath) //nolint:gosec // fixture path
	if err != nil {
		t.Fatalf("read human plan file: %v", err)
	}
	if strings.Contains(string(data), "\n- applied: ") {
		t.Errorf("human plan must never be applied by the actuator:\n%s", data)
	}
}

// TestApprovalWiring_ActuatorFailureDoesNotCorruptApprovalState: the actuator
// errors (archive target exists in no tier), but the approval still succeeds
// — signoff recorded, plan left approved. Failure must be visible on the
// plan's application marker absence + via logs, not by corrupting the gate.
func TestApprovalWiring_ActuatorFailureDoesNotCorruptApprovalState(t *testing.T) {
	fixtureHome := t.TempDir()
	t.Setenv("HOME", fixtureHome)
	t.Setenv("USERPROFILE", fixtureHome)

	c, mgr := buildApprovalWiringComponents(t, fixtureHome)

	// Remove the tier fixture skill so the archive actuation fails.
	if err := os.RemoveAll(filepath.Join(fixtureHome, ".claude", "skills", "approval-wired-skill")); err != nil {
		t.Fatalf("remove tier fixture skill: %v", err)
	}

	if _, err := c.wireEvolverApprovalBridge(); err != nil {
		t.Fatalf("wireEvolverApprovalBridge: %v", err)
	}

	created := createSubmittedEvolverPlan(t, mgr,
		"evo-archive-approval-wired-skill-000099", "archive")

	if err := mgr.ApprovePlan(context.Background(), created.ID, "sess-1", "reviewer"); err != nil {
		t.Fatalf("ApprovePlan must succeed even when the actuator fails: %v", err)
	}

	// Signoff recorded despite actuator failure. The plan advanced past
	// approval into execution (Synthesize succeeded via the stub task
	// creator) — the point is that approval was NOT rolled back or corrupted
	// by the actuator's error.
	p, err := mgr.GetPlan(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("GetPlan: %v", err)
	}
	if p.State != plan.StateApproved && p.State != plan.StateExecuting {
		t.Errorf("plan state = %q, want approved-or-executing (approval corrupted by actuator failure)", p.State)
	}
	if p.State == plan.StateApproved && p.ApprovedBy != "reviewer" {
		t.Errorf("plan approved_by = %q, want %q", p.ApprovedBy, "reviewer")
	}
	// The failed application never wrote the applied marker.
	data, err := os.ReadFile(created.FilePath) //nolint:gosec // fixture path
	if err != nil {
		t.Fatalf("read plan file: %v", err)
	}
	if strings.Contains(string(data), "\n- applied: ") {
		t.Errorf("failed application must not record an applied marker")
	}
}

// waitForCondition polls cond every 10ms until it returns true or the
// timeout elapses. The bridge is asynchronous (bus subscriber goroutine), so
// assertions poll rather than sleep.
func waitForCondition(timeout time.Duration, cond func() bool) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return nil
		}
		time.Sleep(10 * time.Millisecond)
	}
	return wiringTimeout{}
}

// wiringTimeoutError is the sentinel waitForCondition returns on timeout.
type wiringTimeout struct{}

func (wiringTimeout) Error() string { return "condition not met within timeout" }
