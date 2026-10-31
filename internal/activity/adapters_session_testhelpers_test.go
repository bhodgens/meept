package activity

import (
	"github.com/caimlas/meept/internal/session"
	_ "modernc.org/sqlite"
)

// newSessionSQLiteStore opens a real session store under the given path
// (sandbox pattern from e2e/suites/audit-chain). Returns the concrete
// *session.SQLiteStore, which satisfies the SessionReader seam
// structurally via the adapter.
func newSessionSQLiteStore(dbPath string) (*session.SQLiteStore, error) {
	return session.NewSQLiteStore(dbPath, nil)
}

// SessionMsg is a test-local alias for session.Message minus the fields the
// aggregator never reads.
type SessionMsg = session.Message
