package session

import (
	"database/sql"
	_ "modernc.org/sqlite" // driver registration for raw test opens
)

// openRawSQLite opens a database WITHOUT the session store's migrations,
// for hand-seeding legacy schemas in tests.
func openRawSQLite(dbPath string) (*sql.DB, error) {
	return sql.Open("sqlite", dbPath)
}
