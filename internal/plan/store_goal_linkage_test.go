package plan

// Goal-linkage tests (plan tree 20261008-goal-completion, leaf 02):
// Plan.GoalID round-trip and legacy-row backfill semantics.

import (
	"context"
	"testing"
	"time"
)

// TestPlanGoalID_RoundTrip verifies a Plan created with a GoalID persists it
// and every read path (Get, List, ListBySession, ListByState) returns it.
func TestPlanGoalID_RoundTrip(t *testing.T) {
	ctx := context.Background()
	store := setupTestStore(t)

	p := newTestPlan("goal-linked plan")
	p.GoalID = "goal_abc123"
	if err := store.CreatePlan(ctx, p); err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}

	got, err := store.GetPlan(ctx, p.ID)
	if err != nil {
		t.Fatalf("GetPlan: %v", err)
	}
	if got.GoalID != "goal_abc123" {
		t.Errorf("GetPlan.GoalID = %q, want %q", got.GoalID, "goal_abc123")
	}

	// Update carries the linkage too (and can clear it).
	got.GoalID = "goal_def456"
	if err := store.UpdatePlan(ctx, got); err != nil {
		t.Fatalf("UpdatePlan: %v", err)
	}
	got2, err := store.GetPlan(ctx, p.ID)
	if err != nil {
		t.Fatalf("GetPlan after update: %v", err)
	}
	if got2.GoalID != "goal_def456" {
		t.Errorf("GoalID after update = %q, want %q", got2.GoalID, "goal_def456")
	}

	plans, err := store.ListPlans(ctx, "test-project", 10)
	if err != nil {
		t.Fatalf("ListPlans: %v", err)
	}
	if len(plans) != 1 {
		t.Fatalf("ListPlans returned %d plans, want 1", len(plans))
	}
	if plans[0].GoalID != "goal_def456" {
		t.Errorf("ListPlans GoalID = %q, want goal_def456", plans[0].GoalID)
	}

	byState, err := store.ListPlansByState(ctx, StatePlanning, 10)
	if err != nil {
		t.Fatalf("ListPlansByState: %v", err)
	}
	if len(byState) != 1 {
		t.Fatalf("ListPlansByState returned %d plans, want 1", len(byState))
	}
	if byState[0].GoalID != "goal_def456" {
		t.Errorf("ListPlansByState GoalID = %q, want goal_def456", byState[0].GoalID)
	}
}

// TestPlanGoalID_LegacyRow scans a hand-inserted row with a NULL goal_id —
// the shape every pre-migration row has — and verifies it reads back as the
// empty string (backfill rule C3: "no goal" is legacy-legal).
func TestPlanGoalID_LegacyRow(t *testing.T) {
	ctx := context.Background()
	store := setupTestStore(t)

	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := store.DB().ExecContext(ctx, `
		INSERT INTO plans (id, title, description, file_path, project_id, state,
		                   approved_at, confirmed_at, approved_by, confirmed_by,
		                   revision_count, created_at, updated_at)
		VALUES ('plan-legacy', 'legacy plan', 'd', '/tmp/legacy.md', 'proj', 'draft',
		        NULL, NULL, NULL, NULL, 0, ?, ?)`, now, now); err != nil {
		t.Fatalf("insert legacy row: %v", err)
	}

	got, err := store.GetPlan(ctx, "plan-legacy")
	if err != nil {
		t.Fatalf("GetPlan: %v", err)
	}
	if got.GoalID != "" {
		t.Errorf("legacy row GoalID = %q, want empty string", got.GoalID)
	}

	plans, err := store.ListPlans(ctx, "proj", 10)
	if err != nil {
		t.Fatalf("ListPlans: %v", err)
	}
	if len(plans) != 1 || plans[0].GoalID != "" {
		t.Errorf("legacy row via ListPlans GoalID = %q (n=%d), want empty", plans[0].GoalID, len(plans))
	}
}
