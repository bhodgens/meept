package task

// Goal-linkage tests (plan tree 20261008-goal-completion, leaf 02):
// Task.GoalID round-trip and legacy-row backfill semantics.

import (
	"testing"
	"time"
)

// TestTaskGoalID_RoundTrip verifies a Task created with a GoalID persists it
// and every read path (GetByID, List, ListActive, GetTasksForSession)
// returns it.
func TestTaskGoalID_RoundTrip(t *testing.T) {
	s := newTestStore(t)

	tk := makeTask("t-goal", "goal-linked task", StatePending)
	tk.GoalID = "goal_xyz789"
	if err := s.Create(tk); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := s.GetByID("t-goal")
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.GoalID != "goal_xyz789" {
		t.Errorf("GetByID.GoalID = %q, want %q", got.GoalID, "goal_xyz789")
	}

	// Update carries the linkage (and can clear it).
	got.GoalID = "goal_000"
	if err := s.Update(got); err != nil {
		t.Fatalf("Update: %v", err)
	}
	got2, err := s.GetByID("t-goal")
	if err != nil {
		t.Fatalf("GetByID after update: %v", err)
	}
	if got2.GoalID != "goal_000" {
		t.Errorf("GoalID after update = %q, want %q", got2.GoalID, "goal_000")
	}

	tasks, err := s.List(nil, 10)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(tasks) != 1 || tasks[0].GoalID != "goal_000" {
		t.Errorf("List GoalID = %q (n=%d), want goal_000", tasks[0].GoalID, len(tasks))
	}

	active, err := s.ListActive()
	if err != nil {
		t.Fatalf("ListActive: %v", err)
	}
	if len(active) != 1 || active[0].GoalID != "goal_000" {
		t.Errorf("ListActive GoalID = %q (n=%d), want goal_000", active[0].GoalID, len(active))
	}

	if err := s.LinkSession("t-goal", "sess-1"); err != nil {
		t.Fatalf("LinkSession: %v", err)
	}
	bySession, err := s.GetTasksForSession("sess-1")
	if err != nil {
		t.Fatalf("GetTasksForSession: %v", err)
	}
	if len(bySession) != 1 || bySession[0].GoalID != "goal_000" {
		t.Errorf("GetTasksForSession GoalID = %q (n=%d), want goal_000", bySession[0].GoalID, len(bySession))
	}
}

// TestTaskGoalID_UpdateWithoutCounters keeps the no-counter write path
// goal-linked too.
func TestTaskGoalID_UpdateWithoutCounters(t *testing.T) {
	s := newTestStore(t)

	tk := makeTask("t-goal-2", "no-counter task", StatePending)
	tk.GoalID = "goal_keep"
	if err := s.Create(tk); err != nil {
		t.Fatalf("Create: %v", err)
	}

	fresh, err := s.GetByID("t-goal-2")
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if fresh.GoalID != "goal_keep" {
		t.Fatalf("GoalID = %q, want goal_keep", fresh.GoalID)
	}
	if err := s.UpdateWithoutCounters(fresh); err != nil {
		t.Fatalf("UpdateWithoutCounters: %v", err)
	}
	got, err := s.GetByID("t-goal-2")
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.GoalID != "goal_keep" {
		t.Errorf("GoalID after UpdateWithoutCounters = %q, want goal_keep", got.GoalID)
	}
}

// TestTaskGoalID_LegacyRow scans a hand-inserted row with a NULL goal_id —
// the shape every pre-migration row has — and verifies it reads back as the
// empty string (backfill rule C3: "no goal" is legacy-legal).
func TestTaskGoalID_LegacyRow(t *testing.T) {
	s := newTestStore(t)

	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := s.DB().Exec(`
		INSERT INTO tasks (id, name, description, project_dir, workspace_dir, state,
		                   git_repo, memvid_zone, metadata, total_jobs, completed_jobs,
		                   failed_jobs, created_at, updated_at)
		VALUES ('t-legacy', 'legacy task', 'd', NULL, NULL, 'pending',
		        NULL, NULL, '{}', 0, 0, 0, ?, ?)`, now, now); err != nil {
		t.Fatalf("insert legacy row: %v", err)
	}

	got, err := s.GetByID("t-legacy")
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.GoalID != "" {
		t.Errorf("legacy row GoalID = %q, want empty string", got.GoalID)
	}

	tasks, err := s.List(nil, 10)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(tasks) != 1 || tasks[0].GoalID != "" {
		t.Errorf("legacy row via List GoalID = %q (n=%d), want empty", tasks[0].GoalID, len(tasks))
	}
}
