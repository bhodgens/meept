package session

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	_ "github.com/mattn/go-sqlite3"
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

	db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_busy_timeout=5000&_foreign_keys=ON")
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
	_, err := s.db.Exec("ALTER TABLE sessions ADD COLUMN description TEXT DEFAULT ''")
	if err != nil {
		// Ignore "duplicate column" error - column already exists
		if err.Error() != "duplicate column name: description" {
			s.logger.Debug("Description column migration note", "info", err.Error())
		}
	}

	return nil
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
		INSERT INTO sessions (id, name, conversation_id, created_at, last_activity, attached_clients, worker_ids, description)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
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
		SELECT id, name, conversation_id, created_at, last_activity, attached_clients, worker_ids, description
		FROM sessions
		ORDER BY last_activity DESC
		LIMIT 1`)

	return s.scanSession(row)
}

func (s *SQLiteStore) getByColumn(column, value string) *Session {
	query := fmt.Sprintf(`
		SELECT id, name, conversation_id, created_at, last_activity, attached_clients, worker_ids, description
		FROM sessions
		WHERE %s = ?`, column)

	row := s.db.QueryRow(query, value)
	return s.scanSession(row)
}

func (s *SQLiteStore) scanSession(row *sql.Row) *Session {
	var (
		id, name, convID            string
		createdAt, lastActivity     string
		attachedJSON, workersJSON   string
		description                 sql.NullString
	)

	err := row.Scan(&id, &name, &convID, &createdAt, &lastActivity, &attachedJSON, &workersJSON, &description)
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
		SELECT s.id, s.name, s.conversation_id, s.created_at, s.last_activity, s.attached_clients, s.worker_ids, s.description
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
		)

		if err := rows.Scan(&id, &name, &convID, &createdAt, &lastActivity, &attachedJSON, &workersJSON, &description); err != nil {
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
	for _, c := range session.AttachedClients {
		if c == clientID {
			return nil
		}
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
	for _, w := range session.WorkerIDs {
		if w == workerID {
			return nil
		}
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
	defer tx.Rollback()

	stmt, err := tx.Prepare(`
		INSERT INTO session_messages (session_id, role, content, timestamp)
		VALUES (?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("failed to prepare statement: %w", err)
	}
	defer stmt.Close()

	for _, msg := range messages {
		_, err := stmt.Exec(sessionID, msg.Role, msg.Content, msg.Timestamp.Format(time.RFC3339))
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
		SELECT id, session_id, role, content, timestamp
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
		if err := rows.Scan(&msg.ID, &msg.SessionID, &msg.Role, &msg.Content, &ts); err != nil {
			return nil, fmt.Errorf("failed to scan message: %w", err)
		}
		if t, err := time.Parse(time.RFC3339, ts); err == nil {
			msg.Timestamp = t
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

// getByColumnUnsafe is like getByColumn but assumes the lock is already held.
func (s *SQLiteStore) getByColumnUnsafe(column, value string) *Session {
	query := fmt.Sprintf(`
		SELECT id, name, conversation_id, created_at, last_activity, attached_clients, worker_ids, description
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
		SET name = ?, attached_clients = ?, worker_ids = ?, last_activity = ?, description = ?
		WHERE id = ?`,
		session.Name,
		string(attachedJSON),
		string(workersJSON),
		now,
		session.Description,
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

// Ensure SQLiteStore implements Store interface.
var _ Store = (*SQLiteStore)(nil)
