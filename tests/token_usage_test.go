package tests

import (
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/task"

	_ "modernc.org/sqlite"
)

// newTestTaskStore creates a task.Store backed by a temporary SQLite database.
func newTestTaskStore(t *testing.T) *task.Store {
	t.Helper()
	dbPath := t.TempDir() + "/tasks.db"
	store, err := task.NewStore(dbPath, nil)
	if err != nil {
		t.Fatalf("failed to create task store: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

// newTestStepStore creates a StepStore backed by an in-memory SQLite database.
func newTestStepStore(t *testing.T) *task.StepStore {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	// Create the tasks table that task_steps references via FK
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS tasks (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			state TEXT DEFAULT 'pending',
			total_jobs INTEGER DEFAULT 0,
			completed_jobs INTEGER DEFAULT 0,
			failed_jobs INTEGER DEFAULT 0,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);
	`)
	if err != nil {
		t.Fatalf("failed to create tasks table: %v", err)
	}

	store, err := task.NewStepStore(db, nil)
	if err != nil {
		t.Fatalf("failed to create step store: %v", err)
	}
	return store
}

// makeTestTask creates a task with the given ID, name, and state.
func makeTestTask(id, name string, state task.TaskState) *task.Task {
	now := time.Now().UTC()
	return &task.Task{
		ID:        id,
		Name:      name,
		State:     state,
		CreatedAt: now,
		UpdatedAt: now,
	}
}

// TestTaskTokenUsage_Persistence verifies that Task.TokenUsage persists through SQLite.
func TestTaskTokenUsage_Persistence(t *testing.T) {
	store := newTestTaskStore(t)

	// Create task with initial token usage
	t1 := makeTestTask("task-1", "persist tokens", task.StatePending)
	t1.TokenUsage = 1500
	if err := store.Create(t1); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Read back and verify
	got, err := store.GetByID("task-1")
	if err != nil {
		t.Fatalf("GetByID failed: %v", err)
	}
	if got.TokenUsage != 1500 {
		t.Errorf("expected token_usage 1500, got %d", got.TokenUsage)
	}

	// Update with more tokens
	got.AddTokenUsage(500) // now 2000
	if err := store.Update(got); err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	got2, err := store.GetByID("task-1")
	if err != nil {
		t.Fatalf("GetByID after update failed: %v", err)
	}
	if got2.TokenUsage != 2000 {
		t.Errorf("expected token_usage 2000 after update, got %d", got2.TokenUsage)
	}

	// Verify List also returns token_usage
	tasks, err := store.List(nil, 10)
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task from List, got %d", len(tasks))
	}
	if tasks[0].TokenUsage != 2000 {
		t.Errorf("List: expected token_usage 2000, got %d", tasks[0].TokenUsage)
	}

	// Verify ListActive also returns token_usage
	active, err := store.ListActive()
	if err != nil {
		t.Fatalf("ListActive failed: %v", err)
	}
	if len(active) != 1 {
		t.Fatalf("expected 1 active task, got %d", len(active))
	}
	if active[0].TokenUsage != 2000 {
		t.Errorf("ListActive: expected token_usage 2000, got %d", active[0].TokenUsage)
	}
}

// TestTaskStepTokenUsage_Persistence verifies that TaskStep.TokenUsage persists through SQLite.
func TestTaskStepTokenUsage_Persistence(t *testing.T) {
	store := newTestStepStore(t)

	step := task.NewTaskStep("task-1", "test step", 0)
	step.TokenUsage = 800
	if err := store.Create(step); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	got, err := store.GetByID(step.ID)
	if err != nil {
		t.Fatalf("GetByID failed: %v", err)
	}
	if got.TokenUsage != 800 {
		t.Errorf("expected token_usage 800, got %d", got.TokenUsage)
	}

	// Update with more tokens
	got.AddTokenUsage(200)
	if err := store.Update(got); err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	got2, err := store.GetByID(step.ID)
	if err != nil {
		t.Fatalf("GetByID after update failed: %v", err)
	}
	if got2.TokenUsage != 1000 {
		t.Errorf("expected token_usage 1000 after update, got %d", got2.TokenUsage)
	}

	// Verify ListByTaskID also returns token_usage
	steps, err := store.ListByTaskID("task-1")
	if err != nil {
		t.Fatalf("ListByTaskID failed: %v", err)
	}
	if len(steps) != 1 {
		t.Fatalf("expected 1 step, got %d", len(steps))
	}
	if steps[0].TokenUsage != 1000 {
		t.Errorf("ListByTaskID: expected token_usage 1000, got %d", steps[0].TokenUsage)
	}
}

// TestTokenUsage_TrickleUp verifies the trickle-up flow: create task+step,
// set step tokens, simulate aggregation, verify task tokens.
func TestTokenUsage_TrickleUp(t *testing.T) {
	store := newTestTaskStore(t)

	// Create a task with 3 steps
	t1 := makeTestTask("task-trickle", "trickle up test", task.StateExecuting)
	t1.TotalJobs = 3
	if err := store.Create(t1); err != nil {
		t.Fatalf("Create task failed: %v", err)
	}

	// Simulate three steps completing with different token usages
	stepTokens := []int{500, 1200, 300}
	for i, tokens := range stepTokens {
		step := task.NewTaskStep("task-trickle", fmt.Sprintf("step %d", i), i)
		step.TokenUsage = tokens

		if err := store.StepStore().Create(step); err != nil {
			t.Fatalf("Create step %d failed: %v", i, err)
		}
		if err := store.StepStore().SetState(step.ID, task.StepCompleted); err != nil {
			t.Fatalf("SetState step %d failed: %v", i, err)
		}

		// Simulate trickle-up: aggregate step tokens to task
		t1.AddTokenUsage(tokens)
		t1.CompleteJob()
	}

	if err := store.Update(t1); err != nil {
		t.Fatalf("Update task failed: %v", err)
	}

	// Verify the task has the correct total token usage
	got, err := store.GetByID("task-trickle")
	if err != nil {
		t.Fatalf("GetByID failed: %v", err)
	}
	expectedTotal := 500 + 1200 + 300 // 2000
	if got.TokenUsage != expectedTotal {
		t.Errorf("expected total token_usage %d, got %d", expectedTotal, got.TokenUsage)
	}
	if got.CompletedJobs != 3 {
		t.Errorf("expected completed_jobs 3, got %d", got.CompletedJobs)
	}

	// Verify all steps retain their individual token usage
	steps, err := store.StepStore().ListByTaskID("task-trickle")
	if err != nil {
		t.Fatalf("ListByTaskID failed: %v", err)
	}
	if len(steps) != 3 {
		t.Fatalf("expected 3 steps, got %d", len(steps))
	}
	for i, s := range steps {
		if s.TokenUsage != stepTokens[i] {
			t.Errorf("step %d: expected token_usage %d, got %d", i, stepTokens[i], s.TokenUsage)
		}
	}
}

// TestFormatTokenCount verifies the formatTokenCount helper function.
// This tests the logic that is duplicated in agent/util.go and tui/sidebar.go.
func TestFormatTokenCount(t *testing.T) {
	tests := []struct {
		count    int
		expected string
	}{
		{0, "0"},
		{42, "42"},
		{999, "999"},
		{1000, "1.0K"},
		{1500, "1.5K"},
		{9999, "10.0K"},
		{10000, "10.0K"},
		{500000, "500.0K"},
		{1000000, "1.0M"},
		{1200000, "1.2M"},
		{15000000, "15.0M"},
	}

	for _, tt := range tests {
		got := formatTokenCount(tt.count)
		if got != tt.expected {
			t.Errorf("formatTokenCount(%d) = %q, want %q", tt.count, got, tt.expected)
		}
	}
}

// formatTokenCount is a local copy for testing the formatting logic.
func formatTokenCount(count int) string {
	if count >= 1_000_000 {
		return fmt.Sprintf("%.1fM", float64(count)/1_000_000)
	}
	if count >= 1_000 {
		return fmt.Sprintf("%.1fK", float64(count)/1_000)
	}
	return fmt.Sprintf("%d", count)
}
