package agent

import (
	"database/sql"
	"log/slog"
	"testing"

	"github.com/caimlas/meept/internal/task"
	_ "modernc.org/sqlite"
)

// newInMemoryStepStore creates a real StepStore backed by in-memory SQLite
// for orchestrator tests. The tasks table is pre-created so FK constraints pass.
func newInMemoryStepStore(t *testing.T) *task.StepStore {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	// Create tasks table first (FK target).
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS tasks (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		description TEXT,
		state TEXT DEFAULT 'pending',
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	)`)
	if err != nil {
		t.Fatalf("create tasks table: %v", err)
	}

	// Insert a default test task.
	_, err = db.Exec(`INSERT INTO tasks (id, name, description, state, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)`,
		"task-x", "test", "test", "pending", "2026-01-01T00:00:00Z", "2026-01-01T00:00:00Z")
	if err != nil {
		t.Fatalf("insert task: %v", err)
	}

	logger := slog.New(slog.NewTextHandler(&discardWriter{}, &slog.HandlerOptions{Level: slog.LevelDebug}))
	store, err := task.NewStepStore(db, logger)
	if err != nil {
		t.Fatalf("new step store: %v", err)
	}
	return store
}
