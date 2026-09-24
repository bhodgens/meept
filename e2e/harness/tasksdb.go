package harness

import (
	"database/sql"
	"fmt"
	"testing"
	"time"

	_ "modernc.org/sqlite" // pure-Go sqlite driver (module dep of the daemon)
)

// TaskRow mirrors the columns of tasks.db's tasks table the harness
// asserts on. The daemon's own store defines the schema
// (internal/task/store.go migrate).
type TaskRow struct {
	ID            string
	Name          string
	State         string
	TotalJobs     int
	CompletedJobs int
	FailedJobs    int
}

// StepRow mirrors one row of task_steps (internal/task/step.go migrate).
type StepRow struct {
	ID          string
	TaskID      string
	Description string
	State       string
	Result      string
	Sequence    int
}

// openTasksDB opens tasks.db read-only with WAL + busy timeout, mirroring
// the daemon's DSN (internal/task/store.go) so a concurrent writer never
// blocks the reader.
func openTasksDB(t testing.TB, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&mode=ro")
	if err != nil {
		t.Fatalf("harness: open tasks.db: %v", err)
	}
	db.SetMaxOpenConns(1)
	if err := db.Ping(); err != nil {
		db.Close()
		t.Fatalf("harness: ping tasks.db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// Tasks lists all tasks (id, name, state, job counters).
func Tasks(t testing.TB, dbPath string) []TaskRow {
	t.Helper()
	db := openTasksDB(t, dbPath)
	rows, err := db.Query(`SELECT id, name, COALESCE(state,''), COALESCE(total_jobs,0), COALESCE(completed_jobs,0), COALESCE(failed_jobs,0) FROM tasks ORDER BY created_at`)
	if err != nil {
		t.Fatalf("harness: query tasks: %v", err)
	}
	defer rows.Close()
	var out []TaskRow
	for rows.Next() {
		var r TaskRow
		if err := rows.Scan(&r.ID, &r.Name, &r.State, &r.TotalJobs, &r.CompletedJobs, &r.FailedJobs); err != nil {
			t.Fatalf("harness: scan task: %v", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("harness: iterate tasks: %v", err)
	}
	return out
}

// Steps lists the steps of one task.
func Steps(t testing.TB, dbPath, taskID string) []StepRow {
	t.Helper()
	db := openTasksDB(t, dbPath)
	rows, err := db.Query(`SELECT id, task_id, description, COALESCE(state,''), COALESCE(result,''), COALESCE(sequence,0) FROM task_steps WHERE task_id = ? ORDER BY sequence`, taskID)
	if err != nil {
		t.Fatalf("harness: query steps: %v", err)
	}
	defer rows.Close()
	var out []StepRow
	for rows.Next() {
		var r StepRow
		if err := rows.Scan(&r.ID, &r.TaskID, &r.Description, &r.State, &r.Result, &r.Sequence); err != nil {
			t.Fatalf("harness: scan step: %v", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("harness: iterate steps: %v", err)
	}
	return out
}

// WaitTaskCompleted polls tasks.db until the task's state is "completed"
// (or fails the test on timeout). Returns the final task row.
func WaitTaskCompleted(t testing.TB, dbPath, taskID string, timeout time.Duration) TaskRow {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var last string
	for time.Now().Before(deadline) {
		for _, row := range Tasks(t, dbPath) {
			if row.ID != taskID {
				continue
			}
			last = row.State
			if row.State == "completed" {
				return row
			}
			if row.State == "failed" {
				steps := Steps(t, dbPath, taskID)
				t.Fatalf("harness: task %s failed; steps: %+v", taskID, steps)
			}
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Fatalf("harness: task %s not completed within %s (last state %q)", taskID, timeout, last)
	return TaskRow{}
}

// WaitForStepState polls tasks.db until at least one step of the task is
// in the given state.
func WaitForStepState(t testing.TB, dbPath, taskID, state string, timeout time.Duration) StepRow {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		for _, step := range Steps(t, dbPath, taskID) {
			if step.State == state {
				return step
			}
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Fatalf("harness: no step of task %s reached state %q within %s; steps: %+v",
		taskID, state, timeout, Steps(t, dbPath, taskID))
	return StepRow{}
}

// CountStepsByState returns state -> count for one task's steps.
func CountStepsByState(t testing.TB, dbPath, taskID string) map[string]int {
	t.Helper()
	counts := map[string]int{}
	for _, step := range Steps(t, dbPath, taskID) {
		counts[step.State]++
	}
	return counts
}

// FormatSteps renders step rows for failure messages.
func FormatSteps(steps []StepRow) string {
	out := ""
	for _, s := range steps {
		out += fmt.Sprintf("  [%s] %s (seq %d): %.120s\n", s.State, s.ID, s.Sequence, s.Result)
	}
	return out
}
