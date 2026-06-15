package queue

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite" //nolint:revive // blank import for side effects
)

// newTestStore creates a Store backed by a temp-file SQLite database for
// migration and dead-letter tests. The file persists across open/close cycles
// so we can simulate upgrading an old database.
func newTestStore(t *testing.T, dbPath string) *Store {
	t.Helper()
	if dbPath == "" {
		dbPath = filepath.Join(t.TempDir(), "queue.db")
	}
	store, err := NewStore(dbPath, nil)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

// TestDeadLetter_DueAtPreservedOnDeadLetter verifies that when a job with a
// non-nil due_at is moved to the dead-letter queue (via repeated failures),
// the due_at column is preserved in the dead_letter row.
func TestDeadLetter_DueAtPreservedOnDeadLetter(t *testing.T) {
	store := newTestStore(t, "")

	// Create a job with a due_at timestamp.
	dueAt := time.Now().UTC().Add(1 * time.Hour)
	job := mustNewJob(t, JobTypeOneOff, map[string]string{"prompt": "test"})
	job.WithDueAt(dueAt)
	job.WithMaxRetries(0) // fail immediately -> dead letter on first Fail

	if err := store.Insert(job); err != nil {
		t.Fatalf("Insert failed: %v", err)
	}

	// Fail the job so it moves to dead letter.
	if err := store.Fail(job.ID, "test failure"); err != nil {
		t.Fatalf("Fail failed: %v", err)
	}

	// Verify the dead_letter row has due_at set.
	var dlDueAt sql.NullString
	err := store.db.QueryRow(
		`SELECT due_at FROM dead_letter WHERE id = ?`, job.ID,
	).Scan(&dlDueAt)
	if err != nil {
		t.Fatalf("failed to query dead_letter due_at: %v", err)
	}
	if !dlDueAt.Valid {
		t.Fatal("expected due_at to be preserved in dead_letter, got NULL")
	}
	gotDue, err := time.Parse(time.RFC3339, dlDueAt.String)
	if err != nil {
		t.Fatalf("failed to parse dead_letter due_at %q: %v", dlDueAt.String, err)
	}
	// Compare with one-second precision (SQLite stores RFC3339 strings).
	if gotDue.Sub(dueAt).Abs() > time.Second {
		t.Errorf("due_at mismatch: expected ~%s, got %s", dueAt.Format(time.RFC3339), gotDue.Format(time.RFC3339))
	}
}

// TestDeadLetter_DueAtRestoredOnRecover verifies that RecoverFromDeadLetter
// restores the preserved due_at back into the active jobs table.
func TestDeadLetter_DueAtRestoredOnRecover(t *testing.T) {
	store := newTestStore(t, "")

	dueAt := time.Now().UTC().Add(2 * time.Hour)
	job := mustNewJob(t, JobTypeOneOff, map[string]string{"prompt": "recover-me"})
	job.WithDueAt(dueAt)
	job.WithMaxRetries(0)

	if err := store.Insert(job); err != nil {
		t.Fatalf("Insert failed: %v", err)
	}
	if err := store.Fail(job.ID, "boom"); err != nil {
		t.Fatalf("Fail failed: %v", err)
	}

	// Recover from dead letter.
	recovered, err := store.RecoverFromDeadLetter(job.ID)
	if err != nil {
		t.Fatalf("RecoverFromDeadLetter failed: %v", err)
	}
	if recovered == nil {
		t.Fatal("expected recovered job, got nil")
	}

	// The recovered job should have the original due_at restored.
	if recovered.DueAt == nil {
		t.Fatal("expected recovered job to have due_at restored, got nil")
	}
	if recovered.DueAt.Sub(dueAt).Abs() > time.Second {
		t.Errorf("recovered due_at mismatch: expected ~%s, got %s",
			dueAt.Format(time.RFC3339), recovered.DueAt.Format(time.RFC3339))
	}
}

// TestDeadLetter_MigrationWithPreExistingRows simulates upgrading a database
// that was created before the due_at column was added to the dead_letter
// table. It creates a dead_letter row without the due_at column, then runs
// the migration, and verifies the column is added without losing data.
func TestDeadLetter_MigrationWithPreExistingRows(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "migration_queue.db")

	// 1. Open a raw DB and create the OLD dead_letter schema (no due_at).
	rawDB, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("failed to open raw db: %v", err)
	}

	oldSchema := `
		CREATE TABLE IF NOT EXISTS jobs (
			id            TEXT PRIMARY KEY,
			task_id       TEXT,
			agent_id      TEXT,
			type          TEXT NOT NULL,
			priority      INTEGER DEFAULT 2,
			state         TEXT DEFAULT 'pending',
			payload       TEXT NOT NULL,
			required_caps TEXT DEFAULT '[]',
			max_retries   INTEGER DEFAULT 3,
			retry_count   INTEGER DEFAULT 0,
			claimed_by    TEXT,
			result        TEXT,
			error         TEXT,
			created_at    TEXT NOT NULL,
			updated_at    TEXT NOT NULL,
			due_at        TEXT
		);
		CREATE TABLE IF NOT EXISTS dead_letter (
			id            TEXT PRIMARY KEY,
			task_id       TEXT,
			agent_id      TEXT,
			type          TEXT NOT NULL,
			priority      INTEGER,
			payload       TEXT NOT NULL,
			required_caps TEXT,
			max_retries   INTEGER,
			retry_count   INTEGER,
			error         TEXT,
			created_at    TEXT NOT NULL,
			died_at       TEXT NOT NULL
		);
	`
	if _, err := rawDB.Exec(oldSchema); err != nil {
		t.Fatalf("failed to create old schema: %v", err)
	}

	// 2. Insert a pre-existing dead_letter row WITHOUT due_at.
	now := time.Now().UTC().Format(time.RFC3339)
	_, err = rawDB.Exec(`
		INSERT INTO dead_letter (id, task_id, agent_id, type, priority, payload,
		                         required_caps, max_retries, retry_count, error,
		                         created_at, died_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"pre-existing-dl-1", "", "", "one_off", 2,
		`{"prompt":"old"}`, "[]", 3, 3, "exhausted",
		now, now,
	)
	if err != nil {
		t.Fatalf("failed to insert pre-existing dead_letter row: %v", err)
	}
	rawDB.Close()

	// 3. Now open via NewStore which runs the full migration.
	store, err := NewStore(dbPath, nil)
	if err != nil {
		t.Fatalf("NewStore migration failed on pre-existing db: %v", err)
	}
	defer store.Close()

	// 4. Verify due_at column exists on dead_letter.
	var colName string
	err = store.db.QueryRow(`
		SELECT name FROM pragma_table_info('dead_letter')
		WHERE name = 'due_at'`).Scan(&colName)
	if err != nil {
		t.Fatalf("due_at column not found on dead_letter after migration: %v", err)
	}

	// 5. Verify the pre-existing row is intact.
	var rowCount int
	err = store.db.QueryRow(
		`SELECT COUNT(*) FROM dead_letter WHERE id = 'pre-existing-dl-1'`,
	).Scan(&rowCount)
	if err != nil {
		t.Fatalf("failed to count pre-existing row: %v", err)
	}
	if rowCount != 1 {
		t.Errorf("expected 1 pre-existing dead_letter row, got %d", rowCount)
	}

	// 6. Verify the pre-existing row's due_at is NULL (it was added after).
	var dlDueAt sql.NullString
	err = store.db.QueryRow(
		`SELECT due_at FROM dead_letter WHERE id = 'pre-existing-dl-1'`,
	).Scan(&dlDueAt)
	if err != nil {
		t.Fatalf("failed to query pre-existing due_at: %v", err)
	}
	if dlDueAt.Valid {
		t.Errorf("expected NULL due_at for pre-existing row, got %q", dlDueAt.String)
	}

	// 7. Verify we can still recover the pre-existing row (with NULL due_at).
	recovered, err := store.RecoverFromDeadLetter("pre-existing-dl-1")
	if err != nil {
		t.Fatalf("RecoverFromDeadLetter failed for pre-existing row: %v", err)
	}
	if recovered == nil {
		t.Fatal("expected recovered job, got nil")
	}
	if recovered.DueAt != nil {
		t.Errorf("expected nil DueAt for pre-existing row, got %v", recovered.DueAt)
	}
}

// TestDeadLetter_NoDueAtPreservedOnDeadLetter verifies that jobs without a
// due_at result in a NULL due_at in the dead_letter table (no spurious data).
func TestDeadLetter_NoDueAtPreservedOnDeadLetter(t *testing.T) {
	store := newTestStore(t, "")

	// Create a job WITHOUT a due_at.
	job := mustNewJob(t, JobTypeOneOff, map[string]string{"prompt": "no-due"})
	job.WithMaxRetries(0)

	if err := store.Insert(job); err != nil {
		t.Fatalf("Insert failed: %v", err)
	}
	if err := store.Fail(job.ID, "fail"); err != nil {
		t.Fatalf("Fail failed: %v", err)
	}

	var dlDueAt sql.NullString
	err := store.db.QueryRow(
		`SELECT due_at FROM dead_letter WHERE id = ?`, job.ID,
	).Scan(&dlDueAt)
	if err != nil {
		t.Fatalf("failed to query dead_letter due_at: %v", err)
	}
	if dlDueAt.Valid {
		t.Errorf("expected NULL due_at in dead_letter for job without due_at, got %q", dlDueAt.String)
	}
}

// mustNewJob is a test helper that creates a job or fails the test.
func mustNewJob(t *testing.T, jobType JobType, payload any) *Job {
	t.Helper()
	job, err := NewJob(jobType, payload)
	if err != nil {
		t.Fatalf("NewJob failed: %v", err)
	}
	return job
}
