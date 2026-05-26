package session

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite" // sqlite3 driver registration
)

// SQLiteStore implements Store using SQLite for persistence.
type SQLiteStore struct {
	db     *sql.DB
	mu     sync.RWMutex
	logger *slog.Logger
}

// NewSQLiteStore creates a new SQLite-backed session store.
func NewSQLiteStore(dbPath string, logger *slog.Logger) (*SQLiteStore, error) {
	if logger == nil {
		logger = slog.Default()
	}

	db, err := sql.Open("sqlite", dbPath+"?_journal_mode=WAL&_busy_timeout=5000&_foreign_keys=ON")
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	store := &SQLiteStore{
		db:     db,
		logger: logger,
	}

	if err := store.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to migrate database: %w", err)
	}

	logger.Info("SQLite session store initialized", "path", dbPath)
	return store, nil
}

func (s *SQLiteStore) migrate() error {
	schema := `
	CREATE TABLE IF NOT EXISTS sessions (
		id              TEXT PRIMARY KEY,
		name            TEXT NOT NULL,
		conversation_id TEXT UNIQUE NOT NULL,
		created_at      TEXT NOT NULL,
		last_activity   TEXT NOT NULL,
		attached_clients TEXT DEFAULT '[]',
		worker_ids      TEXT DEFAULT '[]'
	);

	CREATE INDEX IF NOT EXISTS idx_sessions_last_activity ON sessions(last_activity DESC);
	CREATE INDEX IF NOT EXISTS idx_sessions_conversation_id ON sessions(conversation_id);

	CREATE TABLE IF NOT EXISTS session_messages (
		id          INTEGER PRIMARY KEY AUTOINCREMENT,
		session_id  TEXT NOT NULL,
		role        TEXT NOT NULL,
		content     TEXT NOT NULL,
		timestamp   TEXT NOT NULL,
		FOREIGN KEY (session_id) REFERENCES sessions(id) ON DELETE CASCADE
	);

	CREATE INDEX IF NOT EXISTS idx_session_messages_session ON session_messages(session_id, id);
	`

	if _, err := s.db.Exec(schema); err != nil {
		return err
	}

	// Add description column if not present (migration for existing databases)
	s.migrationAddColumn("ALTER TABLE sessions ADD COLUMN description TEXT DEFAULT ''", "description")

	// Add leaf_message_id column to sessions
	s.migrationAddColumn("ALTER TABLE sessions ADD COLUMN leaf_message_id INTEGER", "leaf_message_id")

	// Add tree-structure columns to session_messages
	s.migrationAddColumn("ALTER TABLE session_messages ADD COLUMN parent_id INTEGER REFERENCES session_messages(id)", "parent_id")
	s.migrationAddColumn("ALTER TABLE session_messages ADD COLUMN entry_type TEXT DEFAULT 'message'", "entry_type")
	s.migrationAddColumn("ALTER TABLE session_messages ADD COLUMN branch_id TEXT DEFAULT 'main'", "branch_id")
	s.migrationAddColumn("ALTER TABLE session_messages ADD COLUMN model TEXT DEFAULT ''", "model")
	s.migrationAddColumn("ALTER TABLE session_messages ADD COLUMN name TEXT DEFAULT ''", "name")
	s.migrationAddColumn("ALTER TABLE session_messages ADD COLUMN tool_call_id TEXT DEFAULT ''", "tool_call_id")

	// Create session_tool_calls table
	toolCallsSchema := `
	CREATE TABLE IF NOT EXISTS session_tool_calls (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		message_id INTEGER NOT NULL REFERENCES session_messages(id) ON DELETE CASCADE,
		tool_name TEXT NOT NULL,
		tool_call_id TEXT NOT NULL,
		arguments TEXT NOT NULL,
		result TEXT,
		seq INTEGER NOT NULL,
		UNIQUE(message_id, seq)
	);

	CREATE INDEX IF NOT EXISTS idx_session_tool_calls_message_id ON session_tool_calls(message_id);
	`
	if _, err := s.db.Exec(toolCallsSchema); err != nil {
		return fmt.Errorf("failed to create session_tool_calls table: %w", err)
	}

	// Add indexes for tree queries
	s.migrationCreateIndex("CREATE INDEX IF NOT EXISTS idx_session_messages_session_parent ON session_messages(session_id, parent_id)", "idx_session_messages_session_parent")
	s.migrationCreateIndex("CREATE INDEX IF NOT EXISTS idx_session_messages_session_branch ON session_messages(session_id, branch_id)", "idx_session_messages_session_branch")

	// Backfill parent_id for messages created before the column existed.
	// Orders messages by id ASC within each session and chains them so each
	// message's parent_id points to the previous message in that session.
	if err := s.migrationBackfillParentID(); err != nil {
		return fmt.Errorf("failed to backfill parent_id: %w", err)
	}

	return nil
}

// migrationAddColumn runs an ALTER TABLE ADD COLUMN, ignoring "duplicate column" errors.
func (s *SQLiteStore) migrationAddColumn(stmt, columnName string) {
	_, err := s.db.Exec(stmt)
	if err != nil {
		// SQLite returns different error messages for duplicate column across versions
		msg := err.Error()
		if !strings.Contains(msg, "duplicate column name") {
			s.logger.Debug("Column migration note", "column", columnName, "info", msg)
		}
	}
}

// migrationCreateIndex runs a CREATE INDEX IF NOT EXISTS, logging non-fatal errors.
func (s *SQLiteStore) migrationCreateIndex(stmt, indexName string) {
	if _, err := s.db.Exec(stmt); err != nil {
		s.logger.Debug("Index creation note", "index", indexName, "info", err.Error())
	}
}

// migrationBackfillParentID populates parent_id for messages that were created
// before the column existed. For each session it chains messages in insertion
// order (id ASC) so each message points to the previous one. The first message
// in every session keeps parent_id = NULL. Only rows where parent_id IS NULL
// are touched, making the migration idempotent and safe to re-run.
func (s *SQLiteStore) migrationBackfillParentID() error {
	const batchSize = 500

	// Count how many messages still need a parent_id.
	var needBackfill int
	if err := s.db.QueryRow(
		`SELECT COUNT(*) FROM session_messages WHERE parent_id IS NULL`,
	).Scan(&needBackfill); err != nil {
		return fmt.Errorf("failed to count messages needing backfill: %w", err)
	}
	if needBackfill == 0 {
		s.logger.Debug("parent_id backfill not needed")
		return nil
	}

	s.logger.Info("backfilling parent_id for historical messages", "count", needBackfill)

	// Retrieve distinct sessions that have at least one NULL parent_id message.
	sessionRows, err := s.db.Query(
		`SELECT DISTINCT session_id FROM session_messages WHERE parent_id IS NULL`,
	)
	if err != nil {
		return fmt.Errorf("failed to query sessions for backfill: %w", err)
	}
	defer sessionRows.Close()

	var sessionIDs []string
	for sessionRows.Next() {
		var sid string
		if err := sessionRows.Scan(&sid); err != nil {
			return fmt.Errorf("failed to scan session id for backfill: %w", err)
		}
		sessionIDs = append(sessionIDs, sid)
	}
	if err := sessionRows.Err(); err != nil {
		return fmt.Errorf("failed iterating sessions for backfill: %w", err)
	}

	var totalUpdated int
	for _, sid := range sessionIDs {
		updated, err := s.backfillSession(sid, batchSize)
		if err != nil {
			return err
		}
		totalUpdated += updated
	}

	s.logger.Info("parent_id backfill complete", "total_updated", totalUpdated)
	return nil
}

// backfillSession chains messages within a single session in batches.
// For each message with NULL parent_id, it sets parent_id to the id of the
// immediately preceding message (by id ASC) in the session — regardless of
// whether that predecessor already has a parent_id. The very first message
// in the session keeps parent_id = NULL.
func (s *SQLiteStore) backfillSession(sessionID string, batchSize int) (int, error) {
	var totalUpdated int

	for {
		// Fetch a batch of NULL-parent messages with the id of their
		// immediately preceding sibling (LAG). This correctly handles
		// mixed scenarios where some messages already have parent_id set.
		rows, err := s.db.Query(`
			SELECT id, prev_id FROM (
				SELECT id,
				       LAG(id) OVER (ORDER BY id) AS prev_id,
				       parent_id,
				       ROW_NUMBER() OVER (ORDER BY id) AS rn
				FROM session_messages
				WHERE session_id = ?
			) sub
			WHERE parent_id IS NULL
			ORDER BY id ASC
			LIMIT ?`, sessionID, batchSize)
		if err != nil {
			return totalUpdated, fmt.Errorf("failed to query messages for backfill: %w", err)
		}

		type backfillRow struct {
			id     int64
			prevID sql.NullInt64
		}
		var batch []backfillRow
		for rows.Next() {
			var r backfillRow
			if err := rows.Scan(&r.id, &r.prevID); err != nil {
				return totalUpdated, fmt.Errorf("failed to scan message for backfill: %w", err)
			}
			batch = append(batch, r)
		}
		rows.Close() //nolint:sqlclosecheck // manual close in loop; defer would accumulate
		if err := rows.Err(); err != nil {
			return totalUpdated, fmt.Errorf("failed iterating messages for backfill: %w", err)
		}

		if len(batch) == 0 {
			break
		}

		for _, r := range batch {
			if !r.prevID.Valid {
				// This is the first message in the session; keep parent_id NULL.
				continue
			}
			result, err := s.db.Exec(
				`UPDATE session_messages SET parent_id = ? WHERE id = ? AND parent_id IS NULL`,
				r.prevID.Int64, r.id,
			)
			if err != nil {
				return totalUpdated, fmt.Errorf("failed to update parent_id for message %d: %w", r.id, err)
			}
			n, _ := result.RowsAffected()
			totalUpdated += int(n)
		}

		// If we got fewer than batchSize rows, we're done with this session.
		if len(batch) < batchSize {
			break
		}
	}

	return totalUpdated, nil
}

// Create creates a new session with the given name.
func (s *SQLiteStore) Create(name string) (*Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UTC()
	id := fmt.Sprintf("session-%d", now.UnixNano())
	convID := fmt.Sprintf("conv-%d", now.UnixNano())

	session := &Session{
		ID:              id,
		Name:            name,
		ConversationID:  convID,
		CreatedAt:       now,
		LastActivity:    now,
		AttachedClients: []string{},
		WorkerIDs:       []string{},
	}

	attachedJSON, _ := json.Marshal(session.AttachedClients)
	workersJSON, _ := json.Marshal(session.WorkerIDs)

	_, err := s.db.Exec(`
		INSERT INTO sessions (id, name, conversation_id, created_at, last_activity, attached_clients, worker_ids, description, leaf_message_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, NULL)`,
		session.ID,
		session.Name,
		session.ConversationID,
		session.CreatedAt.Format(time.RFC3339),
		session.LastActivity.Format(time.RFC3339),
		string(attachedJSON),
		string(workersJSON),
		"",
	)

	if err != nil {
		s.logger.Error("Failed to create session", "error", err)
		return nil, fmt.Errorf("failed to create session: %w", err)
	}

	s.logger.Info("Session created", "id", id, "name", name)
	return session, nil
}

// Get retrieves a session by ID.
func (s *SQLiteStore) Get(id string) *Session {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.getByColumn("id", id)
}

// GetByConversationID retrieves a session by its conversation ID.
func (s *SQLiteStore) GetByConversationID(conversationID string) *Session {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.getByColumn("conversation_id", conversationID)
}

// GetMostRecent returns the most recently active session.
func (s *SQLiteStore) GetMostRecent() *Session {
	s.mu.RLock()
	defer s.mu.RUnlock()

	row := s.db.QueryRow(`
		SELECT id, name, conversation_id, created_at, last_activity, attached_clients, worker_ids, description, leaf_message_id
		FROM sessions
		ORDER BY last_activity DESC
		LIMIT 1`)

	return s.scanSession(row)
}

func (s *SQLiteStore) getByColumn(column, value string) *Session {
	//nolint:gosec // column name is hardcoded at call sites, not user input
	query := fmt.Sprintf(`
		SELECT id, name, conversation_id, created_at, last_activity, attached_clients, worker_ids, description, leaf_message_id
		FROM sessions
		WHERE %s = ?`, column)

	row := s.db.QueryRow(query, value)
	return s.scanSession(row)
}

func (s *SQLiteStore) scanSession(row *sql.Row) *Session {
	var (
		id, name, convID          string
		createdAt, lastActivity   string
		attachedJSON, workersJSON string
		description               sql.NullString
		leafMessageID             sql.NullInt64
	)

	err := row.Scan(&id, &name, &convID, &createdAt, &lastActivity, &attachedJSON, &workersJSON, &description, &leafMessageID)
	if err != nil {
		if err != sql.ErrNoRows {
			s.logger.Error("Failed to scan session", "error", err)
		}
		return nil
	}

	session := &Session{
		ID:             id,
		Name:           name,
		ConversationID: convID,
	}

	if description.Valid {
		session.Description = description.String
	}
	if leafMessageID.Valid {
		session.LeafMessageID = &leafMessageID.Int64
	}

	if t, err := time.Parse(time.RFC3339, createdAt); err == nil {
		session.CreatedAt = t
	}
	if t, err := time.Parse(time.RFC3339, lastActivity); err == nil {
		session.LastActivity = t
	}

	if err := json.Unmarshal([]byte(attachedJSON), &session.AttachedClients); err != nil {
		s.logger.Warn("Failed to unmarshal session attached_clients JSON", "id", id, "error", err)
	}
	if err := json.Unmarshal([]byte(workersJSON), &session.WorkerIDs); err != nil {
		s.logger.Warn("Failed to unmarshal session worker_ids JSON", "id", id, "error", err)
	}

	return session
}

// List returns all sessions that have at least one assistant response, ordered by last activity.
func (s *SQLiteStore) List() ([]*Session, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.Query(`
		SELECT s.id, s.name, s.conversation_id, s.created_at, s.last_activity, s.attached_clients, s.worker_ids, s.description, s.leaf_message_id
		FROM sessions s
		WHERE EXISTS (
			SELECT 1 FROM session_messages sm
			WHERE sm.session_id = s.id AND sm.role = 'assistant'
		)
		ORDER BY s.last_activity DESC`)
	if err != nil {
		s.logger.Error("Failed to list sessions", "error", err)
		return nil, fmt.Errorf("failed to list sessions: %w", err)
	}
	defer rows.Close()

	sessions := s.scanSessionRows(rows)
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate sessions: %w", err)
	}
	return sessions, nil
}

func (s *SQLiteStore) scanSessionRows(rows *sql.Rows) []*Session {
	var sessions []*Session
	for rows.Next() {
		var (
			id, name, convID          string
			createdAt, lastActivity   string
			attachedJSON, workersJSON string
			description               sql.NullString
			leafMessageID             sql.NullInt64
		)

		if err := rows.Scan(&id, &name, &convID, &createdAt, &lastActivity, &attachedJSON, &workersJSON, &description, &leafMessageID); err != nil {
			s.logger.Error("Failed to scan session row", "error", err)
			continue
		}

		session := &Session{
			ID:             id,
			Name:           name,
			ConversationID: convID,
		}

		if description.Valid {
			session.Description = description.String
		}
		if leafMessageID.Valid {
			session.LeafMessageID = &leafMessageID.Int64
		}

		if t, err := time.Parse(time.RFC3339, createdAt); err == nil {
			session.CreatedAt = t
		}
		if t, err := time.Parse(time.RFC3339, lastActivity); err == nil {
			session.LastActivity = t
		}

		if err := json.Unmarshal([]byte(attachedJSON), &session.AttachedClients); err != nil {
			s.logger.Warn("Failed to unmarshal session attached_clients JSON", "id", id, "error", err)
		}
		if err := json.Unmarshal([]byte(workersJSON), &session.WorkerIDs); err != nil {
			s.logger.Warn("Failed to unmarshal session worker_ids JSON", "id", id, "error", err)
		}

		sessions = append(sessions, session)
	}

	return sessions
}

// Delete removes a session by ID.
func (s *SQLiteStore) Delete(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	result, err := s.db.Exec("DELETE FROM sessions WHERE id = ?", id)
	if err != nil {
		s.logger.Error("Failed to delete session", "id", id, "error", err)
		return false
	}

	rows, err := result.RowsAffected()
	if err != nil {
		s.logger.Warn("Failed to read RowsAffected after Delete", "id", id, "error", err)
		return false
	}
	if rows > 0 {
		s.logger.Info("Session deleted", "id", id)
		return true
	}
	return false
}

// Attach adds a client to a session.
func (s *SQLiteStore) Attach(sessionID, clientID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	session := s.getByColumnUnsafe("id", sessionID)
	if session == nil {
		return fmt.Errorf("session not found: %s", sessionID)
	}

	// Check if already attached
	if slices.Contains(session.AttachedClients, clientID) {
		return nil
	}

	session.AttachedClients = append(session.AttachedClients, clientID)
	return s.updateSession(session)
}

// Detach removes a client from a session.
func (s *SQLiteStore) Detach(sessionID, clientID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	session := s.getByColumnUnsafe("id", sessionID)
	if session == nil {
		return fmt.Errorf("session not found: %s", sessionID)
	}

	for i, c := range session.AttachedClients {
		if c == clientID {
			session.AttachedClients = append(session.AttachedClients[:i], session.AttachedClients[i+1:]...)
			return s.updateSession(session)
		}
	}

	return nil
}

// UpdateActivity updates the last activity timestamp for a session.
func (s *SQLiteStore) UpdateActivity(sessionID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.Exec("UPDATE sessions SET last_activity = ? WHERE id = ?", now, sessionID)
	if err != nil {
		s.logger.Error("Failed to update session activity", "id", sessionID, "error", err)
		return fmt.Errorf("failed to update session activity: %w", err)
	}
	return nil
}

// AddWorker adds a worker ID to a session.
func (s *SQLiteStore) AddWorker(sessionID, workerID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	session := s.getByColumnUnsafe("id", sessionID)
	if session == nil {
		return fmt.Errorf("session not found: %s", sessionID)
	}

	// Check if already present
	if slices.Contains(session.WorkerIDs, workerID) {
		return nil
	}

	session.WorkerIDs = append(session.WorkerIDs, workerID)
	return s.updateSession(session)
}

// RemoveWorker removes a worker ID from a session.
func (s *SQLiteStore) RemoveWorker(sessionID, workerID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	session := s.getByColumnUnsafe("id", sessionID)
	if session == nil {
		return fmt.Errorf("session not found: %s", sessionID)
	}

	for i, w := range session.WorkerIDs {
		if w == workerID {
			session.WorkerIDs = append(session.WorkerIDs[:i], session.WorkerIDs[i+1:]...)
			return s.updateSession(session)
		}
	}

	return nil
}

// SaveMessages batch-inserts messages for a session in a transaction.
func (s *SQLiteStore) SaveMessages(sessionID string, messages []Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	stmt, err := tx.Prepare(`
		INSERT INTO session_messages (session_id, role, content, timestamp, parent_id, entry_type, branch_id, model, name, tool_call_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("failed to prepare statement: %w", err)
	}
	defer stmt.Close()

	for _, msg := range messages {
		var entryType, branchID string
		if msg.EntryType != "" {
			entryType = msg.EntryType
		} else {
			entryType = KeyMessage
		}
		if msg.BranchID != "" {
			branchID = msg.BranchID
		} else {
			branchID = BranchMain
		}

		_, err := stmt.Exec(sessionID, msg.Role, msg.Content, msg.Timestamp.Format(time.RFC3339),
			msg.ParentID, entryType, branchID, msg.Model, msg.Name, msg.ToolCallID)
		if err != nil {
			return fmt.Errorf("failed to insert message: %w", err)
		}
	}

	return tx.Commit()
}

// GetMessages retrieves messages for a session with pagination, ordered by id.
func (s *SQLiteStore) GetMessages(sessionID string, offset, limit int) ([]Message, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.Query(`
		SELECT id, session_id, role, content, timestamp, parent_id, entry_type, branch_id, model, name, tool_call_id
		FROM session_messages
		WHERE session_id = ?
		ORDER BY id
		LIMIT ? OFFSET ?`, sessionID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to query messages: %w", err)
	}
	defer rows.Close()

	var messages []Message
	for rows.Next() {
		var msg Message
		var ts string
		var parentID sql.NullInt64
		var entryType, branchID, model, name, toolCallID sql.NullString
		if err := rows.Scan(&msg.ID, &msg.SessionID, &msg.Role, &msg.Content, &ts, &parentID, &entryType, &branchID, &model, &name, &toolCallID); err != nil {
			return nil, fmt.Errorf("failed to scan message: %w", err)
		}
		if t, err := time.Parse(time.RFC3339, ts); err == nil {
			msg.Timestamp = t
		}
		if parentID.Valid {
			msg.ParentID = &parentID.Int64
		}
		if entryType.Valid {
			msg.EntryType = entryType.String
		}
		if branchID.Valid {
			msg.BranchID = branchID.String
		}
		if model.Valid {
			msg.Model = model.String
		}
		if name.Valid {
			msg.Name = name.String
		}
		if toolCallID.Valid {
			msg.ToolCallID = toolCallID.String
		}
		messages = append(messages, msg)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed iterating session messages: %w", err)
	}

	return messages, nil
}

// GetMessageCount returns the number of messages in a session.
func (s *SQLiteStore) GetMessageCount(sessionID string) (int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var count int
	err := s.db.QueryRow("SELECT COUNT(*) FROM session_messages WHERE session_id = ?", sessionID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count messages: %w", err)
	}
	return count, nil
}

// UpdateDescription updates a session's description.
func (s *SQLiteStore) UpdateDescription(sessionID, description string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	result, err := s.db.Exec("UPDATE sessions SET description = ? WHERE id = ?", description, sessionID)
	if err != nil {
		return fmt.Errorf("failed to update description: %w", err)
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("session not found: %s", sessionID)
	}
	return nil
}

// UpdateName updates a session's name.
func (s *SQLiteStore) UpdateName(sessionID, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	result, err := s.db.Exec("UPDATE sessions SET name = ? WHERE id = ?", name, sessionID)
	if err != nil {
		return fmt.Errorf("failed to update name: %w", err)
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("session not found: %s", sessionID)
	}
	return nil
}

// HasResponses checks if a session has any assistant messages.
func (s *SQLiteStore) HasResponses(sessionID string) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var exists bool
	err := s.db.QueryRow(`
		SELECT EXISTS(
			SELECT 1 FROM session_messages
			WHERE session_id = ? AND role = 'assistant'
		)`, sessionID).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("failed to check responses: %w", err)
	}
	return exists, nil
}

// Close closes the database connection.
func (s *SQLiteStore) Close() error {
	return s.db.Close()
}

//nolint:unparam // column is intentionally generic for reuse across different query types
func (s *SQLiteStore) getByColumnUnsafe(column, value string) *Session {
	//nolint:gosec // column name is hardcoded at call sites, not user input
	query := fmt.Sprintf(`
		SELECT id, name, conversation_id, created_at, last_activity, attached_clients, worker_ids, description, leaf_message_id
		FROM sessions
		WHERE %s = ?`, column)

	row := s.db.QueryRow(query, value)
	return s.scanSession(row)
}

// updateSession updates a session in the database.
// Returns an error if no rows are affected (session was deleted).
func (s *SQLiteStore) updateSession(session *Session) error {
	attachedJSON, _ := json.Marshal(session.AttachedClients)
	workersJSON, _ := json.Marshal(session.WorkerIDs)
	now := time.Now().UTC().Format(time.RFC3339)

	result, err := s.db.Exec(`
		UPDATE sessions
		SET name = ?, attached_clients = ?, worker_ids = ?, last_activity = ?, description = ?, leaf_message_id = ?
		WHERE id = ?`,
		session.Name,
		string(attachedJSON),
		string(workersJSON),
		now,
		session.Description,
		session.LeafMessageID,
		session.ID,
	)

	if err != nil {
		s.logger.Error("Failed to update session", "id", session.ID, "error", err)
		return err
	}

	rows, err := result.RowsAffected()
	if err != nil {
		s.logger.Warn("Failed to read RowsAffected after update", "id", session.ID, "error", err)
		return nil // Update succeeded but couldn't verify
	}
	if rows == 0 {
		return fmt.Errorf("session not found or was deleted: %s", session.ID)
	}

	return nil
}

// --- Tree and branch operations ---

// GetLeafMessageID returns the current leaf message ID for a session.
// Returns 0 if no leaf is set.
func (s *SQLiteStore) GetLeafMessageID(sessionID string) (int64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var leafID sql.NullInt64
	err := s.db.QueryRow(`SELECT leaf_message_id FROM sessions WHERE id = ?`, sessionID).Scan(&leafID)
	if err != nil {
		return 0, fmt.Errorf("failed to get leaf message id: %w", err)
	}
	if !leafID.Valid {
		return 0, nil
	}
	return leafID.Int64, nil
}

// SetLeafMessageID updates the leaf message ID for a session.
func (s *SQLiteStore) SetLeafMessageID(sessionID string, messageID int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	result, err := s.db.Exec(`UPDATE sessions SET leaf_message_id = ? WHERE id = ?`, messageID, sessionID)
	if err != nil {
		return fmt.Errorf("failed to set leaf message id: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("session not found: %s", sessionID)
	}
	return nil
}

// GetMessagePath returns the path from root to the given leaf message ID,
// using a recursive CTE to walk the parent chain.
func (s *SQLiteStore) GetMessagePath(sessionID string, leafID int64) ([]Message, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	query := `
	WITH RECURSIVE path AS (
		SELECT id, session_id, parent_id, role, content, timestamp, entry_type, branch_id, model, name, tool_call_id
		FROM session_messages
		WHERE id = ? AND session_id = ?
		UNION ALL
		SELECT m.id, m.session_id, m.parent_id, m.role, m.content, m.timestamp, m.entry_type, m.branch_id, m.model, m.name, m.tool_call_id
		FROM session_messages m
		INNER JOIN path p ON m.id = p.parent_id
		WHERE m.session_id = ?
	)
	SELECT id, session_id, parent_id, role, content, timestamp, entry_type, branch_id, model, name, tool_call_id
	FROM path
	ORDER BY id`

	rows, err := s.db.Query(query, leafID, sessionID, sessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to query message path: %w", err)
	}
	defer rows.Close()

	var messages []Message
	for rows.Next() {
		var msg Message
		var ts string
		var parentID sql.NullInt64
		var entryType, branchID, model, name, toolCallID sql.NullString
		if err := rows.Scan(&msg.ID, &msg.SessionID, &parentID, &msg.Role, &msg.Content, &ts, &entryType, &branchID, &model, &name, &toolCallID); err != nil {
			return nil, fmt.Errorf("failed to scan message in path: %w", err)
		}
		if t, err := time.Parse(time.RFC3339, ts); err == nil {
			msg.Timestamp = t
		}
		if parentID.Valid {
			msg.ParentID = &parentID.Int64
		}
		if entryType.Valid {
			msg.EntryType = entryType.String
		}
		if branchID.Valid {
			msg.BranchID = branchID.String
		}
		if model.Valid {
			msg.Model = model.String
		}
		if name.Valid {
			msg.Name = name.String
		}
		if toolCallID.Valid {
			msg.ToolCallID = toolCallID.String
		}
		messages = append(messages, msg)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed iterating message path: %w", err)
	}

	return messages, nil
}

// GetMessageBranches returns all branches in a session.
func (s *SQLiteStore) GetMessageBranches(sessionID string) ([]Branch, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Each branch has a branch_id; the leaf is the last message in that branch
	query := `
	SELECT branch_id, COUNT(*) as msg_count, MAX(id) as leaf_id
	FROM session_messages
	WHERE session_id = ?
	GROUP BY branch_id
	ORDER BY MIN(id)`

	rows, err := s.db.Query(query, sessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to query branches: %w", err)
	}
	defer rows.Close()

	var branches []Branch
	for rows.Next() {
		var b Branch
		if err := rows.Scan(&b.ID, &b.MessageCount, &b.LeafID); err != nil {
			return nil, fmt.Errorf("failed to scan branch: %w", err)
		}
		branches = append(branches, b)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed iterating branches: %w", err)
	}

	return branches, nil
}

// GetTree returns all nodes in the session tree for visualization.
func (s *SQLiteStore) GetTree(sessionID string) ([]TreeNode, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Get the current leaf for IsLeaf marking
	var leafID sql.NullInt64
	_ = s.db.QueryRow(`SELECT leaf_message_id FROM sessions WHERE id = ?`, sessionID).Scan(&leafID)

	query := `
	SELECT id, COALESCE(parent_id, 0), role, COALESCE(entry_type, 'message'), COALESCE(branch_id, 'main'),
	       content, timestamp
	FROM session_messages
	WHERE session_id = ?
	ORDER BY id`

	rows, err := s.db.Query(query, sessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to query tree: %w", err)
	}
	defer rows.Close()

	var nodes []TreeNode
	for rows.Next() {
		var n TreeNode
		var ts string
		if err := rows.Scan(&n.ID, &n.ParentID, &n.Role, &n.EntryType, &n.BranchID, &n.Content, &ts); err != nil {
			return nil, fmt.Errorf("failed to scan tree node: %w", err)
		}
		n.Timestamp = ts
		if leafID.Valid && n.ID == leafID.Int64 {
			n.IsLeaf = true
		}
		// Truncate content for tree view
		if len(n.Content) > 200 {
			n.Content = n.Content[:197] + "..."
		}
		nodes = append(nodes, n)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed iterating tree: %w", err)
	}

	return nodes, nil
}

// NavigateToBranch moves the session leaf to a target message.
// Returns the old leaf ID. Validates that both session and target message exist.
func (s *SQLiteStore) NavigateToBranch(sessionID string, targetMessageID int64) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Validate session exists
	session := s.getByColumnUnsafe("id", sessionID)
	if session == nil {
		return 0, fmt.Errorf("session not found: %s", sessionID)
	}

	// Get current leaf
	var oldLeaf sql.NullInt64
	err := s.db.QueryRow(`SELECT leaf_message_id FROM sessions WHERE id = ?`, sessionID).Scan(&oldLeaf)
	if err != nil {
		return 0, fmt.Errorf("failed to get current leaf: %w", err)
	}
	var oldLeafID int64
	if oldLeaf.Valid {
		oldLeafID = oldLeaf.Int64
	}

	// Validate target message exists in this session
	var exists bool
	err = s.db.QueryRow(`SELECT EXISTS(SELECT 1 FROM session_messages WHERE id = ? AND session_id = ?)`,
		targetMessageID, sessionID).Scan(&exists)
	if err != nil {
		return 0, fmt.Errorf("failed to validate target message: %w", err)
	}
	if !exists {
		return 0, fmt.Errorf("target message %d not found in session %s", targetMessageID, sessionID)
	}

	// Update leaf to target
	result, err := s.db.Exec(`UPDATE sessions SET leaf_message_id = ? WHERE id = ?`, targetMessageID, sessionID)
	if err != nil {
		return 0, fmt.Errorf("failed to update leaf: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return 0, fmt.Errorf("session not found: %s", sessionID)
	}

	return oldLeafID, nil
}

// ForkSession creates a new session by copying messages from root to fromMessageID
// from the source session. The new session gets its own IDs for all messages and
// tool calls, with parent_id references remapped accordingly.
func (s *SQLiteStore) ForkSession(sourceSessionID string, fromMessageID int64, newName string) (*Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// 1. Validate source session exists
	source := s.getByColumnUnsafe("id", sourceSessionID)
	if source == nil {
		return nil, fmt.Errorf("source session not found: %s", sourceSessionID)
	}

	// 2. Start transaction
	tx, err := s.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// 3. Create new session
	now := time.Now().UTC()
	newID, err := randomHex(8)
	if err != nil {
		return nil, fmt.Errorf("failed to generate session ID: %w", err)
	}
	newID = "session-" + newID
	newConvID, err := randomHex(8)
	if err != nil {
		return nil, fmt.Errorf("failed to generate conversation ID: %w", err)
	}
	newConvID = "conv-" + newConvID
	if newName == "" {
		newName = "fork of " + source.Name
	}

	attachedJSON, _ := json.Marshal([]string{})
	workersJSON, _ := json.Marshal([]string{})

	_, err = tx.Exec(`
		INSERT INTO sessions (id, name, conversation_id, created_at, last_activity, attached_clients, worker_ids, description, leaf_message_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, '', NULL)`,
		newID, newName, newConvID,
		now.Format(time.RFC3339), now.Format(time.RFC3339),
		string(attachedJSON), string(workersJSON),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create forked session: %w", err)
	}

	// 4. Copy messages from root to fromMessageID.
	// We need to copy the path from root to the target message. Since messages
	// form a tree, we use a recursive CTE to collect all ancestors.
	rows, err := tx.Query(`
		WITH RECURSIVE ancestors AS (
			SELECT id, session_id, role, content, timestamp, parent_id, entry_type, branch_id, model, name, tool_call_id
			FROM session_messages
			WHERE id = ? AND session_id = ?
			UNION ALL
			SELECT m.id, m.session_id, m.role, m.content, m.timestamp, m.parent_id, m.entry_type, m.branch_id, m.model, m.name, m.tool_call_id
			FROM session_messages m
			INNER JOIN ancestors a ON m.id = a.parent_id
			WHERE m.session_id = ?
		)
		SELECT id, role, content, timestamp, parent_id, entry_type, branch_id, model, name, tool_call_id
		FROM ancestors
		ORDER BY id`, fromMessageID, sourceSessionID, sourceSessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to query messages for fork: %w", err)
	}
	defer rows.Close()

	type sourceMsg struct {
		oldID      int64
		oldParent  *int64
		role       string
		content    string
		timestamp  string
		entryType  sql.NullString
		branchID   sql.NullString
		model      sql.NullString
		name       sql.NullString
		toolCallID sql.NullString
	}
	var sourceMsgs []sourceMsg
	for rows.Next() {
		var sm sourceMsg
		var parentID sql.NullInt64
		if err := rows.Scan(&sm.oldID, &sm.role, &sm.content, &sm.timestamp, &parentID,
			&sm.entryType, &sm.branchID, &sm.model, &sm.name, &sm.toolCallID); err != nil {
			return nil, fmt.Errorf("failed to scan source message: %w", err)
		}
		if parentID.Valid {
			sm.oldParent = &parentID.Int64
		}
		sourceMsgs = append(sourceMsgs, sm)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed iterating source messages: %w", err)
	}

	if len(sourceMsgs) == 0 {
		return nil, fmt.Errorf("message %d not found in session %s", fromMessageID, sourceSessionID)
	}

	// 5. Insert copied messages into new session, building old->new ID map
	oldToNew := make(map[int64]int64)
	var newLeafID int64

	insertStmt, err := tx.Prepare(`
		INSERT INTO session_messages (session_id, role, content, timestamp, parent_id, entry_type, branch_id, model, name, tool_call_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return nil, fmt.Errorf("failed to prepare message insert: %w", err)
	}
	defer insertStmt.Close()

	for _, sm := range sourceMsgs {
		// Resolve parent_id: if the old parent was copied, use new ID; otherwise nil (root)
		var newParentID any
		if sm.oldParent != nil {
			if np, ok := oldToNew[*sm.oldParent]; ok {
				newParentID = np
			}
			// If old parent wasn't in the copied set, this becomes a root message
		}

		entryType := KeyMessage
		if sm.entryType.Valid && sm.entryType.String != "" {
			entryType = sm.entryType.String
		}
		branchID := BranchMain
		if sm.branchID.Valid && sm.branchID.String != "" {
			branchID = sm.branchID.String
		}
		model := ""
		if sm.model.Valid {
			model = sm.model.String
		}
		name := ""
		if sm.name.Valid {
			name = sm.name.String
		}
		toolCallID := ""
		if sm.toolCallID.Valid {
			toolCallID = sm.toolCallID.String
		}

		result, err := insertStmt.Exec(newID, sm.role, sm.content, sm.timestamp,
			newParentID, entryType, branchID, model, name, toolCallID)
		if err != nil {
			return nil, fmt.Errorf("failed to insert copied message: %w", err)
		}

		newMsgID, err := result.LastInsertId()
		if err != nil {
			return nil, fmt.Errorf("failed to get new message ID: %w", err)
		}
		oldToNew[sm.oldID] = newMsgID
		newLeafID = newMsgID // last one is the target message (highest ID)
	}

	// 6. Copy tool calls for the copied messages
	if len(oldToNew) > 0 {
		oldIDs := make([]int64, 0, len(oldToNew))
		for oldID := range oldToNew {
			oldIDs = append(oldIDs, oldID)
		}

		placeholders := make([]string, len(oldIDs))
		args := make([]any, len(oldIDs))
		for i, id := range oldIDs {
			placeholders[i] = "?"
			args[i] = id
		}

		//nolint:gosec // placeholders are all "?"; IN clause args are parameterized
		tcQuery := fmt.Sprintf(`
			SELECT id, message_id, tool_name, tool_call_id, arguments, result, seq
			FROM session_tool_calls
			WHERE message_id IN (%s)
			ORDER BY seq`, strings.Join(placeholders, ","))

		tcRows, err := tx.Query(tcQuery, args...)
		if err != nil {
			return nil, fmt.Errorf("failed to query tool calls for fork: %w", err)
		}
		defer tcRows.Close() //nolint:sqlclosecheck // defer in loop is safe here; each iteration re-assigns tcRows

		tcInsertStmt, err := tx.Prepare(`
			INSERT INTO session_tool_calls (message_id, tool_name, tool_call_id, arguments, result, seq)
			VALUES (?, ?, ?, ?, ?, ?)`)
		if err != nil {
			return nil, fmt.Errorf("failed to prepare tool call insert: %w", err)
		}
		defer tcInsertStmt.Close() //nolint:sqlclosecheck // defer in loop is safe here; each iteration re-assigns tcInsertStmt

		for tcRows.Next() {
			var tcID int64
			var tcMsgID int64
			var tcToolName, tcToolCallID, tcArgs string
			var tcResult sql.NullString
			var tcSeq int
			if err := tcRows.Scan(&tcID, &tcMsgID, &tcToolName, &tcToolCallID, &tcArgs, &tcResult, &tcSeq); err != nil {
				return nil, fmt.Errorf("failed to scan tool call: %w", err)
			}
			newMsgID, ok := oldToNew[tcMsgID]
			if !ok {
				continue // shouldn't happen, but skip if it does
			}
			var resultVal any
			if tcResult.Valid {
				resultVal = tcResult.String
			}
			if _, err := tcInsertStmt.Exec(newMsgID, tcToolName, tcToolCallID, tcArgs, resultVal, tcSeq); err != nil {
				return nil, fmt.Errorf("failed to insert copied tool call: %w", err)
			}
		}
		if err := tcRows.Err(); err != nil {
			return nil, fmt.Errorf("failed iterating tool calls: %w", err)
		}
	}

	// 7. Set leaf_message_id on new session
	_, err = tx.Exec(`UPDATE sessions SET leaf_message_id = ? WHERE id = ?`, newLeafID, newID)
	if err != nil {
		return nil, fmt.Errorf("failed to set leaf on forked session: %w", err)
	}

	// 8. Commit
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit fork: %w", err)
	}

	newSession := &Session{
		ID:              newID,
		Name:            newName,
		ConversationID:  newConvID,
		CreatedAt:       now,
		LastActivity:    now,
		AttachedClients: []string{},
		WorkerIDs:       []string{},
		LeafMessageID:   &newLeafID,
	}

	s.logger.Info("Session forked",
		"source_id", sourceSessionID,
		"new_id", newID,
		"from_message", fromMessageID,
		"copied_messages", len(sourceMsgs),
	)

	return newSession, nil
}

// InsertCompaction inserts a compaction entry that replaces the given compressed IDs.
// The summary content is stored as JSON with the compressed_ids included.
func (s *SQLiteStore) InsertCompaction(sessionID string, parentID int64, summary string, compressedIDs []int64) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Build JSON content
	content := CompactionContent{
		Summary:       summary,
		CompressedIDs: compressedIDs,
	}
	contentJSON, err := json.Marshal(content)
	if err != nil {
		return 0, fmt.Errorf("failed to marshal compaction content: %w", err)
	}

	result, err := s.db.Exec(`
		INSERT INTO session_messages (session_id, role, content, timestamp, parent_id, entry_type, branch_id, model, name, tool_call_id)
		VALUES (?, 'system', ?, ?, ?, 'compaction', 'main', '', '', '')`,
		sessionID,
		string(contentJSON),
		time.Now().UTC().Format(time.RFC3339),
		parentID,
	)
	if err != nil {
		return 0, fmt.Errorf("failed to insert compaction: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("failed to get last insert id: %w", err)
	}

	s.logger.Info("Compaction entry inserted",
		"id", id,
		"session_id", sessionID,
		"parent_id", parentID,
		"compressed_count", len(compressedIDs),
	)
	return id, nil
}

// CompactionContent represents the JSON content of a compaction entry.
type CompactionContent struct {
	Summary       string  `json:"summary"`
	CompressedIDs []int64 `json:"compressed_ids"`
}

// ReparentAfterCompaction re-parents all messages whose parent_id is afterID
// (the last compressed message) to point to compactionID instead. This makes
// GetMessagePath walk through the compaction entry, skipping the compacted
// messages in the tree.
func (s *SQLiteStore) ReparentAfterCompaction(sessionID string, afterID, compactionID int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	result, err := s.db.Exec(`
		UPDATE session_messages
		SET parent_id = ?
		WHERE session_id = ? AND parent_id = ?`,
		compactionID, sessionID, afterID,
	)
	if err != nil {
		return fmt.Errorf("failed to re-parent messages after compaction: %w", err)
	}

	rows, _ := result.RowsAffected()
	s.logger.Debug("Reparented messages after compaction",
		"session_id", sessionID,
		"old_parent", afterID,
		"new_parent", compactionID,
		"rows_affected", rows,
	)
	return nil
}

// GetCompactionEntries retrieves all compaction entries for a session,
// ordered by ID. Returns an empty slice if none exist.
func (s *SQLiteStore) GetCompactionEntries(sessionID string) ([]CompactionEntry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.Query(`
		SELECT id, session_id, content, timestamp, parent_id
		FROM session_messages
		WHERE session_id = ? AND entry_type = 'compaction'
		ORDER BY id`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to query compaction entries: %w", err)
	}
	defer rows.Close()

	var entries []CompactionEntry
	for rows.Next() {
		var entry CompactionEntry
		var ts string
		var parentID sql.NullInt64
		if err := rows.Scan(&entry.ID, &entry.SessionID, &entry.Content, &ts, &parentID); err != nil {
			return nil, fmt.Errorf("failed to scan compaction entry: %w", err)
		}
		if t, err := time.Parse(time.RFC3339, ts); err == nil {
			entry.Timestamp = t
		}
		if parentID.Valid {
			entry.ParentID = &parentID.Int64
		}

		// Parse the JSON content to extract CompressedIDs
		var content CompactionContent
		if err := json.Unmarshal([]byte(entry.Content), &content); err == nil {
			entry.CompressedIDs = content.CompressedIDs
		} else {
			s.logger.Warn("Failed to parse compaction content JSON",
				"entry_id", entry.ID,
				"error", err,
			)
		}

		entries = append(entries, entry)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed iterating compaction entries: %w", err)
	}

	return entries, nil
}

// --- Tool call operations ---

// SaveToolCalls persists tool calls associated with a message.
func (s *SQLiteStore) SaveToolCalls(messageID int64, toolCalls []ToolCall) error {
	if len(toolCalls) == 0 {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	stmt, err := tx.Prepare(`
		INSERT INTO session_tool_calls (message_id, tool_name, tool_call_id, arguments, result, seq)
		VALUES (?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("failed to prepare tool call insert: %w", err)
	}
	defer stmt.Close()

	for _, tc := range toolCalls {
		_, err := stmt.Exec(messageID, tc.ToolName, tc.ToolCallID, tc.Arguments, tc.Result, tc.Seq)
		if err != nil {
			return fmt.Errorf("failed to insert tool call: %w", err)
		}
	}

	return tx.Commit()
}

// GetToolCalls retrieves all tool calls for a single message.
func (s *SQLiteStore) GetToolCalls(messageID int64) ([]ToolCall, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.Query(`
		SELECT id, message_id, tool_name, tool_call_id, arguments, result, seq
		FROM session_tool_calls
		WHERE message_id = ?
		ORDER BY seq`, messageID)
	if err != nil {
		return nil, fmt.Errorf("failed to query tool calls: %w", err)
	}
	defer rows.Close()

	var toolCalls []ToolCall
	for rows.Next() {
		var tc ToolCall
		var result sql.NullString
		if err := rows.Scan(&tc.ID, &tc.MessageID, &tc.ToolName, &tc.ToolCallID, &tc.Arguments, &result, &tc.Seq); err != nil {
			return nil, fmt.Errorf("failed to scan tool call: %w", err)
		}
		if result.Valid {
			tc.Result = result.String
		}
		toolCalls = append(toolCalls, tc)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed iterating tool calls: %w", err)
	}

	return toolCalls, nil
}

// GetToolCallsForMessages batch-retrieves tool calls for multiple messages.
func (s *SQLiteStore) GetToolCallsForMessages(messageIDs []int64) (map[int64][]ToolCall, error) {
	if len(messageIDs) == 0 {
		return make(map[int64][]ToolCall), nil
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	// Build placeholders for IN clause
	placeholders := make([]string, len(messageIDs))
	args := make([]any, len(messageIDs))
	for i, id := range messageIDs {
		placeholders[i] = "?"
		args[i] = id
	}

	//nolint:gosec // placeholders are all "?"; IN clause args are parameterized
	query := fmt.Sprintf(`
		SELECT id, message_id, tool_name, tool_call_id, arguments, result, seq
		FROM session_tool_calls
		WHERE message_id IN (%s)
		ORDER BY seq`, strings.Join(placeholders, ","))

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query tool calls for messages: %w", err)
	}
	defer rows.Close()

	result := make(map[int64][]ToolCall)
	for rows.Next() {
		var tc ToolCall
		var tcResult sql.NullString
		if err := rows.Scan(&tc.ID, &tc.MessageID, &tc.ToolName, &tc.ToolCallID, &tc.Arguments, &tcResult, &tc.Seq); err != nil {
			return nil, fmt.Errorf("failed to scan tool call: %w", err)
		}
		if tcResult.Valid {
			tc.Result = tcResult.String
		}
		result[tc.MessageID] = append(result[tc.MessageID], tc)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed iterating tool calls for messages: %w", err)
	}

	return result, nil
}

// Ensure SQLiteStore implements Store interface.
var _ Store = (*SQLiteStore)(nil)
