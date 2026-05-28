package plan

import (
	"context"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/caimlas/meept/internal/config"
)

// setupTestManager creates a PlanManager backed by a temporary SQLite store
// with nil bus and nil taskCreator.
func setupTestManager(t *testing.T) *PlanManager {
	t.Helper()
	logger := slog.Default()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	store, err := NewSQLiteStore(dbPath, logger)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	cfg := config.PlansConfig{
		Mode: "threshold",
		Threshold: config.PlansThresholdConfig{
			MinSteps:           3,
			ComplexityKeywords: []string{"refactor", "migrate", "implement"},
			AlwaysPlanIntents:  []string{"plan", "implement", "build"},
		},
		Storage: config.PlansStorageConfig{
			DefaultPath:      "docs/plans",
			FilenameTemplate: "{{slug}}.md",
		},
		Approval: config.PlansApprovalConfig{
			RequireApproval: true,
			AllowRevision:   true,
			MaxRevisions:    3,
		},
		Confirmation: config.PlansConfirmationConfig{
			RequireSignoff: true,
		},
	}

	return NewPlanManager(store, nil, cfg, nil, logger)
}

// setupTestManagerWithDir is like setupTestManager but returns the temp dir
// so tests can inspect files on disk.
func setupTestManagerWithDir(t *testing.T) (*PlanManager, string) {
	t.Helper()
	logger := slog.Default()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	store, err := NewSQLiteStore(dbPath, logger)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	cfg := config.PlansConfig{
		Mode: "threshold",
		Threshold: config.PlansThresholdConfig{
			MinSteps:           3,
			ComplexityKeywords: []string{"refactor", "migrate", "implement"},
			AlwaysPlanIntents:  []string{"plan", "implement", "build"},
		},
		Storage: config.PlansStorageConfig{
			DefaultPath:      "docs/plans",
			FilenameTemplate: "{{slug}}.md",
		},
		Approval: config.PlansApprovalConfig{
			RequireApproval: true,
			AllowRevision:   true,
			MaxRevisions:    3,
		},
		Confirmation: config.PlansConfirmationConfig{
			RequireSignoff: true,
		},
	}

	return NewPlanManager(store, nil, cfg, nil, logger), dir
}

// --------------------------------------------------------------------------
// Tests
// --------------------------------------------------------------------------

func TestManagerCreatePlan(t *testing.T) {
	mgr, dir := setupTestManagerWithDir(t)
	ctx := context.Background()

	plan, err := mgr.CreatePlan(ctx, "Refactor Auth System", "Refactor the auth module", "proj-1", dir, "sess-001")
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}

	// State should be draft after creation (planning -> draft transition).
	if plan.State != StateDraft {
		t.Errorf("state after create: got %q, want %q", plan.State, StateDraft)
	}

	// Plan file should exist on disk.
	if plan.FilePath == "" {
		t.Fatal("FilePath should not be empty")
	}

	// Verify the file path follows expected pattern.
	expectedSlug := "refactor-auth-system.md"
	gotBase := filepath.Base(plan.FilePath)
	if gotBase != expectedSlug {
		t.Errorf("file basename: got %q, want %q", gotBase, expectedSlug)
	}

	// Verify plan is retrievable from store by ID.
	got, err := mgr.store.GetPlan(ctx, plan.ID)
	if err != nil {
		t.Fatalf("GetPlan: %v", err)
	}
	if got.State != StateDraft {
		t.Errorf("stored state: got %q, want %q", got.State, StateDraft)
	}

	// Verify session is linked.
	plans, err := mgr.store.GetPlansForSession(ctx, "sess-001")
	if err != nil {
		t.Fatalf("GetPlansForSession: %v", err)
	}
	if len(plans) != 1 {
		t.Fatalf("plans for session: got %d, want 1", len(plans))
	}
	if plans[0].ID != plan.ID {
		t.Errorf("linked plan ID: got %q, want %q", plans[0].ID, plan.ID)
	}
}

func TestManagerApproveFlow(t *testing.T) {
	mgr, dir := setupTestManagerWithDir(t)
	ctx := context.Background()

	// Create a plan.
	plan, err := mgr.CreatePlan(ctx, "Migrate Database", "Migrate from Postgres to SQLite", "proj-1", dir, "sess-001")
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	if plan.State != StateDraft {
		t.Fatalf("state after create: got %q, want %q", plan.State, StateDraft)
	}

	// Submit the plan (draft -> pending_approval).
	if err := mgr.SubmitPlan(ctx, plan.ID); err != nil {
		t.Fatalf("SubmitPlan: %v", err)
	}
	got, err := mgr.store.GetPlan(ctx, plan.ID)
	if err != nil {
		t.Fatalf("GetPlan after submit: %v", err)
	}
	if got.State != StatePendingApproval {
		t.Errorf("state after submit: got %q, want %q", got.State, StatePendingApproval)
	}

	// Approve the plan. Synthesize will fail because taskCreator is nil,
	// but the plan should already be in StateApproved in the store.
	err = mgr.ApprovePlan(ctx, plan.ID, "sess-001", "reviewer")
	if err == nil {
		// If no error, synthesis somehow succeeded — verify state.
		got, _ = mgr.store.GetPlan(ctx, plan.ID)
		if got.State != StateExecuting {
			t.Errorf("state after approve+synthesize: got %q, want %q", got.State, StateExecuting)
		}
	} else {
		// Synthesis failed (expected with nil taskCreator). Check that state
		// was at least set to approved before the synthesis attempt.
		got, getErr := mgr.store.GetPlan(ctx, plan.ID)
		if getErr != nil {
			t.Fatalf("GetPlan after approve: %v", getErr)
		}
		if got.State != StateApproved {
			t.Errorf("state after approve (synthesis failed): got %q, want %q", got.State, StateApproved)
		}
	}
}

func TestManagerRejectPlan(t *testing.T) {
	mgr, dir := setupTestManagerWithDir(t)
	ctx := context.Background()

	// Create -> Submit -> Reject.
	plan, err := mgr.CreatePlan(ctx, "Test Plan", "desc", "proj-1", dir, "sess-001")
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	if err := mgr.SubmitPlan(ctx, plan.ID); err != nil {
		t.Fatalf("SubmitPlan: %v", err)
	}
	if err := mgr.RejectPlan(ctx, plan.ID, "sess-001", "reviewer", "not good enough"); err != nil {
		t.Fatalf("RejectPlan: %v", err)
	}

	got, err := mgr.store.GetPlan(ctx, plan.ID)
	if err != nil {
		t.Fatalf("GetPlan after reject: %v", err)
	}
	if got.State != StateCancelled {
		t.Errorf("state after reject: got %q, want %q", got.State, StateCancelled)
	}

	// Verify a rejection signoff was recorded.
	signoffs, err := mgr.store.GetSignoffs(ctx, plan.ID)
	if err != nil {
		t.Fatalf("GetSignoffs: %v", err)
	}
	found := false
	for _, so := range signoffs {
		if so.Action == "rejected" {
			found = true
			if so.By != "reviewer" {
				t.Errorf("reject signoff by: got %q, want %q", so.By, "reviewer")
			}
			break
		}
	}
	if !found {
		t.Error("expected a 'rejected' signoff to be recorded")
	}
}

func TestManagerRevisePlan(t *testing.T) {
	mgr, dir := setupTestManagerWithDir(t)
	ctx := context.Background()

	// Create -> Submit -> Revise.
	plan, err := mgr.CreatePlan(ctx, "Test Plan", "desc", "proj-1", dir, "sess-001")
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	if err := mgr.SubmitPlan(ctx, plan.ID); err != nil {
		t.Fatalf("SubmitPlan: %v", err)
	}
	if err := mgr.RevisePlan(ctx, plan.ID, "sess-001", "needs more detail"); err != nil {
		t.Fatalf("RevisePlan: %v", err)
	}

	got, err := mgr.store.GetPlan(ctx, plan.ID)
	if err != nil {
		t.Fatalf("GetPlan after revise: %v", err)
	}

	// State should be back to planning.
	if got.State != StatePlanning {
		t.Errorf("state after revise: got %q, want %q", got.State, StatePlanning)
	}

	// Revision count should be 1.
	if got.RevisionCount != 1 {
		t.Errorf("revision count after 1 revise: got %d, want 1", got.RevisionCount)
	}
}

func TestManagerMaxRevisions(t *testing.T) {
	mgr, dir := setupTestManagerWithDir(t)
	ctx := context.Background()

	plan, err := mgr.CreatePlan(ctx, "Test Plan", "desc", "proj-1", dir, "sess-001")
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	if err := mgr.SubmitPlan(ctx, plan.ID); err != nil {
		t.Fatalf("SubmitPlan: %v", err)
	}

	// Revise 3 times (up to max revisions = 3).
	// After each revise, state goes to planning. Simulate the re-work cycle:
	// planning -> draft (via store) -> pending_approval (via SubmitPlan).
	for i := 0; i < 3; i++ {
		if err := mgr.RevisePlan(ctx, plan.ID, "sess-001", "revision feedback"); err != nil {
			t.Fatalf("RevisePlan(%d): %v", i+1, err)
		}
		// Simulate re-work: set to draft, then submit.
		if err := mgr.store.SetPlanState(ctx, plan.ID, StateDraft); err != nil {
			t.Fatalf("SetPlanState(draft) after revise %d: %v", i+1, err)
		}
		if err := mgr.SubmitPlan(ctx, plan.ID); err != nil {
			t.Fatalf("SubmitPlan after revise %d: %v", i+1, err)
		}
	}

	// The 4th revision should fail.
	err = mgr.RevisePlan(ctx, plan.ID, "sess-001", "one more try")
	if err == nil {
		t.Error("expected error on 4th revision (exceeds max), got nil")
	}

	// Verify the plan is still in a valid state (should be pending_approval).
	got, getErr := mgr.store.GetPlan(ctx, plan.ID)
	if getErr != nil {
		t.Fatalf("GetPlan after failed revise: %v", getErr)
	}
	if got.State != StatePendingApproval {
		t.Errorf("state after failed revise: got %q, want %q", got.State, StatePendingApproval)
	}
}

func TestManagerConfirmPlan(t *testing.T) {
	mgr, dir := setupTestManagerWithDir(t)
	ctx := context.Background()

	// Create a plan.
	plan, err := mgr.CreatePlan(ctx, "Test Plan", "desc", "proj-1", dir, "sess-001")
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}

	// Manually set state to completed via store (simulating plan execution finish).
	if err := mgr.store.SetPlanState(ctx, plan.ID, StateCompleted); err != nil {
		t.Fatalf("SetPlanState(completed): %v", err)
	}

	// Confirm the plan.
	if err := mgr.ConfirmPlan(ctx, plan.ID, "sess-001", "manager"); err != nil {
		t.Fatalf("ConfirmPlan: %v", err)
	}

	got, err := mgr.store.GetPlan(ctx, plan.ID)
	if err != nil {
		t.Fatalf("GetPlan after confirm: %v", err)
	}
	if got.State != StateConfirmed {
		t.Errorf("state after confirm: got %q, want %q", got.State, StateConfirmed)
	}
	if got.ConfirmedBy != "manager" {
		t.Errorf("confirmed_by: got %q, want %q", got.ConfirmedBy, "manager")
	}
	if got.ConfirmedAt == nil {
		t.Error("confirmed_at should not be nil")
	}

	// Verify a confirmation signoff was recorded.
	signoffs, err := mgr.store.GetSignoffs(ctx, plan.ID)
	if err != nil {
		t.Fatalf("GetSignoffs: %v", err)
	}
	found := false
	for _, so := range signoffs {
		if so.Action == "confirmed" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected a 'confirmed' signoff to be recorded")
	}
}

func TestShouldCreatePlanThreshold(t *testing.T) {
	mgr := setupTestManager(t)

	tests := []struct {
		name      string
		intent    string
		stepCount int
		want      bool
	}{
		{"chat intent with enough steps returns true", "chat", 5, true},
		{"implement is always-plan intent", "implement", 2, true},
		{"code with enough steps", "code", 4, true},
		{"code below threshold", "code", 2, false},
		{"build is always-plan intent", "build", 1, true},
		{"refactor keyword match", "refactor module", 1, true},
		{"plan is always-plan intent", "plan", 0, true},
		{"unknown intent below threshold", "search", 1, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mgr.ShouldCreatePlan(tt.intent, tt.stepCount)
			if got != tt.want {
				t.Errorf("ShouldCreatePlan(%q, %d) = %v, want %v", tt.intent, tt.stepCount, got, tt.want)
			}
		})
	}
}

func TestShouldCreatePlanAlways(t *testing.T) {
	mgr := setupTestManager(t)
	mgr.config.Mode = "always"

	tests := []struct {
		name      string
		intent    string
		stepCount int
	}{
		{"chat always", "chat", 0},
		{"code always", "code", 1},
		{"empty always", "", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mgr.ShouldCreatePlan(tt.intent, tt.stepCount)
			if !got {
				t.Errorf("ShouldCreatePlan(%q, %d) in 'always' mode = false, want true", tt.intent, tt.stepCount)
			}
		})
	}
}

func TestShouldCreatePlanOff(t *testing.T) {
	mgr := setupTestManager(t)
	mgr.config.Mode = "off"

	tests := []struct {
		name      string
		intent    string
		stepCount int
	}{
		{"implement off", "implement", 10},
		{"code off", "code", 100},
		{"refactor off", "refactor", 50},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mgr.ShouldCreatePlan(tt.intent, tt.stepCount)
			if got {
				t.Errorf("ShouldCreatePlan(%q, %d) in 'off' mode = true, want false", tt.intent, tt.stepCount)
			}
		})
	}
}

func TestResolvePlanDir(t *testing.T) {
	mgr := setupTestManager(t)

	// With empty external_path, should return projectPath + default_path.
	got := mgr.resolvePlanDir("/home/user/project")
	want := "/home/user/project/docs/plans"
	if got != want {
		t.Errorf("resolvePlanDir with empty external_path: got %q, want %q", got, want)
	}

	// With external_path set, should return the expanded external_path.
	mgr.config.Storage.ExternalPath = "/tmp/external-plans"
	got = mgr.resolvePlanDir("/home/user/project")
	want = "/tmp/external-plans"
	if got != want {
		t.Errorf("resolvePlanDir with external_path: got %q, want %q", got, want)
	}

	// With env var in external_path.
	mgr.config.Storage.ExternalPath = "$TMPDIR/plans"
	got = mgr.resolvePlanDir("/home/user/project")
	// Result should contain the expanded TMPDIR, not the literal string.
	if got == "$TMPDIR/plans" {
		t.Error("resolvePlanDir should expand env vars in external_path")
	}
}
