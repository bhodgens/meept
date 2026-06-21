package session

// migrateThreadsTable creates the session_threads table if it doesn't exist.
// This is called from SQLiteStore.migrate().
func (s *SQLiteStore) migrateThreadsTable() error {
	schema := `
	CREATE TABLE IF NOT EXISTS session_threads (
		id              TEXT PRIMARY KEY,
		session_id      TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
		topic_label     TEXT NOT NULL DEFAULT 'general',
		conversation_id TEXT NOT NULL,
		created_at      TEXT NOT NULL,
		last_activity   TEXT NOT NULL,
		summary         TEXT DEFAULT '',
		is_active       INTEGER DEFAULT 0,
		UNIQUE(session_id, topic_label)
	);
	`
	if _, err := s.db.Exec(schema); err != nil {
		return err
	}

	// Create indexes
	s.migrationCreateIndex("CREATE INDEX IF NOT EXISTS idx_session_threads_session ON session_threads(session_id)", "idx_session_threads_session")
	s.migrationCreateIndex("CREATE INDEX IF NOT EXISTS idx_session_threads_active ON session_threads(session_id, is_active)", "idx_session_threads_active")

	return nil
}
