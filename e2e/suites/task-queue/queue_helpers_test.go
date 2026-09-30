//go:build e2e

// Queue-store read helpers for the task-queue suite: read-only access to the
// daemon's queue.db (same schema as internal/queue/store.go) so assertions
// read persisted state directly, independent of the RPC surface.
package taskqueue

import (
	"database/sql"
	"testing"

	"github.com/caimlas/meept/e2e/harness"
	_ "modernc.org/sqlite" // pure-Go sqlite driver (module dep of the daemon)
)

// openQueueDB opens the daemon's queue.db read-only with WAL + busy
// timeout, mirroring the daemon's DSN so a concurrent writer never blocks
// the reader.
func openQueueDB(s *harness.Stack) (*sql.DB, error) {
	db, err := sql.Open("sqlite", queueDBPath(s)+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&mode=ro")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

// queryQueueJob fetches one job row by id.
func queryQueueJob(db *sql.DB, jobID string) (*queueJobRow, error) {
	var row queueJobRow
	err := db.QueryRow(`SELECT id, COALESCE(state,''), COALESCE(updated_at,'') FROM jobs WHERE id = ?`, jobID).
		Scan(&row.ID, &row.State, &row.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

var _ = testing.TB(nil) // keep testing import for helper symmetry
