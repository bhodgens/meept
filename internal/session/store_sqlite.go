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

	db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
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
	`

	_, err := s.db.Exec(schema)
	return err
}

// Create creates a new session with the given name.
func (s *SQLiteStore) Create(name string) *Session {
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
		INSERT INTO sessions (id, name, conversation_id, created_at, last_activity, attached_clients, worker_ids)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		session.ID,
		session.Name,
		session.ConversationID,
		session.CreatedAt.Format(time.RFC3339),
		session.LastActivity.Format(time.RFC3339),
		string(attachedJSON),
		string(workersJSON),
	)

	if err != nil {
		s.logger.Error("Failed to create session", "error", err)
		return nil
	}

	s.logger.Info("Session created", "id", id, "name", name)
	return session
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
		SELECT id, name, conversation_id, created_at, last_activity, attached_clients, worker_ids
		FROM sessions
		ORDER BY last_activity DESC
		LIMIT 1`)

	return s.scanSession(row)
}

func (s *SQLiteStore) getByColumn(column, value string) *Session {
	query := fmt.Sprintf(`
		SELECT id, name, conversation_id, created_at, last_activity, attached_clients, worker_ids
		FROM sessions
		WHERE %s = ?`, column)

	row := s.db.QueryRow(query, value)
	return s.scanSession(row)
}

func (s *SQLiteStore) scanSession(row *sql.Row) *Session {
	var (
		id, name, convID      string
		createdAt, lastActivity string
		attachedJSON, workersJSON string
	)

	err := row.Scan(&id, &name, &convID, &createdAt, &lastActivity, &attachedJSON, &workersJSON)
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

	if t, err := time.Parse(time.RFC3339, createdAt); err == nil {
		session.CreatedAt = t
	}
	if t, err := time.Parse(time.RFC3339, lastActivity); err == nil {
		session.LastActivity = t
	}

	json.Unmarshal([]byte(attachedJSON), &session.AttachedClients)
	json.Unmarshal([]byte(workersJSON), &session.WorkerIDs)

	return session
}

// List returns all sessions ordered by last activity.
func (s *SQLiteStore) List() []*Session {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.Query(`
		SELECT id, name, conversation_id, created_at, last_activity, attached_clients, worker_ids
		FROM sessions
		ORDER BY last_activity DESC`)
	if err != nil {
		s.logger.Error("Failed to list sessions", "error", err)
		return nil
	}
	defer rows.Close()

	var sessions []*Session
	for rows.Next() {
		var (
			id, name, convID        string
			createdAt, lastActivity string
			attachedJSON, workersJSON string
		)

		if err := rows.Scan(&id, &name, &convID, &createdAt, &lastActivity, &attachedJSON, &workersJSON); err != nil {
			s.logger.Error("Failed to scan session row", "error", err)
			continue
		}

		session := &Session{
			ID:             id,
			Name:           name,
			ConversationID: convID,
		}

		if t, err := time.Parse(time.RFC3339, createdAt); err == nil {
			session.CreatedAt = t
		}
		if t, err := time.Parse(time.RFC3339, lastActivity); err == nil {
			session.LastActivity = t
		}

		json.Unmarshal([]byte(attachedJSON), &session.AttachedClients)
		json.Unmarshal([]byte(workersJSON), &session.WorkerIDs)

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

	rows, _ := result.RowsAffected()
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
func (s *SQLiteStore) UpdateActivity(sessionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.Exec("UPDATE sessions SET last_activity = ? WHERE id = ?", now, sessionID)
	if err != nil {
		s.logger.Error("Failed to update session activity", "id", sessionID, "error", err)
	}
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

// Close closes the database connection.
func (s *SQLiteStore) Close() error {
	return s.db.Close()
}

// getByColumnUnsafe is like getByColumn but assumes the lock is already held.
func (s *SQLiteStore) getByColumnUnsafe(column, value string) *Session {
	query := fmt.Sprintf(`
		SELECT id, name, conversation_id, created_at, last_activity, attached_clients, worker_ids
		FROM sessions
		WHERE %s = ?`, column)

	row := s.db.QueryRow(query, value)
	return s.scanSession(row)
}

// updateSession updates a session in the database.
func (s *SQLiteStore) updateSession(session *Session) error {
	attachedJSON, _ := json.Marshal(session.AttachedClients)
	workersJSON, _ := json.Marshal(session.WorkerIDs)
	now := time.Now().UTC().Format(time.RFC3339)

	_, err := s.db.Exec(`
		UPDATE sessions
		SET name = ?, attached_clients = ?, worker_ids = ?, last_activity = ?
		WHERE id = ?`,
		session.Name,
		string(attachedJSON),
		string(workersJSON),
		now,
		session.ID,
	)

	if err != nil {
		s.logger.Error("Failed to update session", "id", session.ID, "error", err)
	}
	return err
}

// Ensure SQLiteStore implements Store interface.
var _ Store = (*SQLiteStore)(nil)
