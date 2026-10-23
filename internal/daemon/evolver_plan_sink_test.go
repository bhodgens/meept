package daemon

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/plan"
	"github.com/caimlas/meept/internal/skills/lifecycle"
)

// TestEvolverPlanSink_CreatePlanLandsInSinkNotRepo is the core leaf-01
// invariant: constructing the evolver's PlanManager via the daemon wiring
// with a fixture HOME and a CWD pointed at a temp "repo", creating a plan
// writes under the sink, and the temp repo's docs/plans gains NOTHING. The
// sink directory is created on demand (lazily at first write).
func TestEvolverPlanSink_CreatePlanLandsInSinkNotRepo(t *testing.T) {
	fixtureHome := t.TempDir()
	fakeRepo := t.TempDir()
	sink := filepath.Join(fixtureHome, ".meept", "plans", "evolver")

	// Pre-normalized config (normalization itself is covered by the config
	// package tests); the daemon wiring must consume it as-is.
	cfg := config.DefaultConfig()
	cfg.Skills.Evolver.Enabled = true
	cfg.Skills.Evolver.PlanDir = sink

	evolverMgr, closeFn := buildEvolverPlanSinkFixture(t, cfg, fixtureHome)
	defer closeFn()

	// The sink must NOT exist before the first plan write (lazy creation).
	if _, err := os.Stat(sink); !os.IsNotExist(err) {
		t.Fatalf("sink %q should not exist before first write (stat err: %v)", sink, err)
	}

	// Run the actual evolver park path with auto_apply=false semantics: the
	// evolver-dedicated plan manager creates the plan, and the evolver
	// stamps provenance — mirroring evolver.go's CreatePlan call site.
	created, err := evolverMgr.CreatePlan(context.Background(),
		"Skill evolution: archive fake-skill", "test", "", "", "")
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	lifecycle.StampEvolverPlan(created, "evo-archive-fake-skill-000001", "archive")

	// Plan file landed under the sink (directory was created on demand).
	if !strings.HasPrefix(created.FilePath, sink+string(filepath.Separator)) {
		t.Fatalf("plan file %q not under sink %q", created.FilePath, sink)
	}
	if _, err := os.Stat(created.FilePath); err != nil {
		t.Fatalf("plan file missing after create: %v", err)
	}

	// The fake repo's docs/plans gains NOTHING.
	repoPlans := filepath.Join(fakeRepo, "docs", "plans")
	if _, err := os.Stat(repoPlans); !os.IsNotExist(err) {
		t.Fatalf("fake repo docs/plans should not exist (stat err: %v)", err)
	}

	// Provenance round-trips through the durable file check.
	if !lifecycle.IsEvolverPlanFile(created.FilePath) {
		t.Fatal("stamped plan file not recognized as evolver plan")
	}
}

// TestEvolverPlanSink_HumanPlansUnaffected verifies the other half of the
// invariant: a PlanManager built with default (repo-relative) storage — the
// way human plan flows construct it — still resolves docs/plans under the
// given project path, and is NOT redirected to the evolver sink.
func TestEvolverPlanSink_HumanPlansUnaffected(t *testing.T) {
	fixtureHome := t.TempDir()
	fakeRepo := t.TempDir()
	sink := filepath.Join(fixtureHome, ".meept", "plans", "evolver")

	cfg := config.DefaultConfig()
	cfg.Skills.Evolver.Enabled = true
	cfg.Skills.Evolver.PlanDir = sink

	logger := slog.Default()
	dir := t.TempDir()
	store, err := plan.NewSQLiteStore(filepath.Join(dir, "test.db"), logger)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	// Shared manager: default storage, no ExternalPath (human plan flow).
	humanCfg := cfg.Plans
	humanCfg.Storage.ExternalPath = ""
	humanMgr := plan.NewPlanManager(store, nil, humanCfg, nil, logger)

	created, err := humanMgr.CreatePlan(context.Background(), "Human plan", "desc", "proj-1", fakeRepo, "")
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	wantDir := filepath.Join(fakeRepo, "docs", "plans")
	if !strings.HasPrefix(created.FilePath, wantDir+string(filepath.Separator)) {
		t.Fatalf("human plan file %q not under repo docs/plans %q", created.FilePath, wantDir)
	}
	// The sink must not have been touched by the human flow.
	if _, err := os.Stat(sink); !os.IsNotExist(err) {
		t.Fatalf("sink %q must not be created by human plan flow (stat err: %v)", sink, err)
	}
}

// TestEvolverPlanSink_MissingPlanDirRejected verifies the wiring refuses to
// construct the evolver plan manager when the config carries a relative
// plan_dir (normalization rejects it; the wiring surfaces the error).
func TestEvolverPlanSink_MissingPlanDirRejected(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Skills.Evolver.Enabled = true
	cfg.Skills.Evolver.PlanDir = "relative/sink"

	logger := slog.Default()
	dir := t.TempDir()
	store, err := plan.NewSQLiteStore(filepath.Join(dir, "test.db"), logger)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	if _, err := (&Components{}).newEvolverPlanManager(store, cfg, logger); err == nil {
		t.Fatal("newEvolverPlanManager with relative plan_dir must fail, got nil error")
	}
}

// TestEvolverPlanSink_NeverAutoApproves pins the item-3 guard (decision brief
// 2026-10-22): the evolver sink ALWAYS requires human approval regardless of
// the global plans.approval.require_approval. The 922-plan executing backlog
// came from a brief window where the global flag was false, so SubmitPlan
// auto-approved machine proposals and synthesized zero-phase tasks that could
// never complete. Build the sink with RequireApproval=false globally and prove
// a submitted evolver plan still lands in pending_approval, never approved.
func TestEvolverPlanSink_NeverAutoApproves(t *testing.T) {
	fixtureHome := t.TempDir()
	sink := filepath.Join(fixtureHome, ".meept", "plans", "evolver")

	cfg := config.DefaultConfig()
	cfg.Skills.Evolver.Enabled = true
	cfg.Skills.Evolver.PlanDir = sink
	// Global config says approval NOT required - the sink must override it.
	cfg.Plans.Approval.RequireApproval = false

	evolverMgr, closeFn := buildEvolverPlanSinkFixture(t, cfg, fixtureHome)
	defer closeFn()

	created, err := evolverMgr.CreatePlan(context.Background(),
		"Skill evolution: archive fake-skill", "test", "", "", "")
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	if err := evolverMgr.SubmitPlan(context.Background(), created.ID); err != nil {
		t.Fatalf("SubmitPlan: %v", err)
	}

	got, err := evolverMgr.GetPlan(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("GetPlan: %v", err)
	}
	if got.State != plan.StatePendingApproval {
		t.Fatalf("evolver sink plan state = %s, want %s (sink auto-approved despite the guard)",
			got.State, plan.StatePendingApproval)
	}
}

// buildEvolverPlanSinkFixture constructs the evolver-dedicated PlanManager
// through newEvolverPlanManager — the actual daemon wiring function under
// test — backed by a temp SQLite store, with HOME pinned to the fixture.
func buildEvolverPlanSinkFixture(t *testing.T, cfg *config.Config, fixtureHome string) (*plan.PlanManager, func()) {
	t.Helper()

	// Point the process at the fixture HOME for the duration of the test so
	// any ~-expansion resolves inside the fixture, never the developer home.
	t.Setenv("HOME", fixtureHome)
	t.Setenv("USERPROFILE", fixtureHome) // windows-compatible env name

	logger := slog.Default()
	dir := t.TempDir()
	store, err := plan.NewSQLiteStore(filepath.Join(dir, "test.db"), logger)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	mgr, err := (&Components{}).newEvolverPlanManager(store, cfg, logger)
	if err != nil {
		t.Fatalf("newEvolverPlanManager: %v", err)
	}
	return mgr, func() { _ = store.Close() }
}
