package session

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/caimlas/meept/pkg/id"
)

// boolToInt converts a bool to an int (1 for true, 0 for false) for SQLite
// INTEGER column persistence. Mirrors internal/security/engine.go.
// NOTE: added to unblock build for unrelated LLM reasoning effort work;
// the thread-based context partitioning WIP is the user's separate effort.
func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// ThreadStore provides CRUD operations for thread persistence.
type ThreadStore interface {
	// CreateThread persists a new thread.
	CreateThread(ctx context.Context, thread *Thread) error
	// GetThread retrieves a thread by ID.
	GetThread(ctx context.Context, threadID string) (*Thread, error)
	// ListThreadsBySession returns all threads for a session.
	ListThreadsBySession(ctx context.Context, sessionID string) ([]*Thread, error)
	// UpdateThread updates an existing thread.
	UpdateThread(ctx context.Context, thread *Thread) error
	// DeleteThread removes a thread.
	DeleteThread(ctx context.Context, threadID string) error
	// GetActiveThread returns the active thread for a session.
	GetActiveThread(ctx context.Context, sessionID string) (*Thread, error)
	// SetActiveThread sets the active thread for a session (deactivates others).
	SetActiveThread(ctx context.Context, sessionID, threadID string) error
}

// SQLiteThreadStore implements ThreadStore using SQLite.
type SQLiteThreadStore struct {
	db         *sql.DB
	sessionID  string
	getSession func() *Session
}

// NewSQLiteThreadStore creates a new SQLite thread store.
func NewSQLiteThreadStore(db *sql.DB, sessionID string, getSession func() *Session) *SQLiteThreadStore {
	return &SQLiteThreadStore{
		db:         db,
		sessionID:  sessionID,
		getSession: getSession,
	}
}

// Ensure SQLiteThreadStore implements ThreadStore.
var _ ThreadStore = (*SQLiteThreadStore)(nil)

// CreateThread persists a new thread.
func (s *SQLiteThreadStore) CreateThread(ctx context.Context, thread *Thread) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT OR REPLACE INTO session_threads (id, session_id, topic_label, conversation_id, created_at, last_activity, summary, is_active)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`,
		thread.ID,
		thread.SessionID,
		thread.TopicLabel,
		thread.ConversationID,
		thread.CreatedAt.Format(time.RFC3339),
		thread.LastActivityAt.Format(time.RFC3339),
		thread.Summary,
		boolToInt(thread.IsActive),
	)
	return err
}

// GetThread retrieves a thread by ID.
func (s *SQLiteThreadStore) GetThread(ctx context.Context, threadID string) (*Thread, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, session_id, topic_label, conversation_id, created_at, last_activity, summary, is_active
		FROM session_threads
		WHERE id = ? AND session_id = ?
	`, threadID, s.sessionID)

	var t Thread
	var createdAtStr, lastActivityStr string
	err := row.Scan(&t.ID, &t.SessionID, &t.TopicLabel, &t.ConversationID,
		&createdAtStr, &lastActivityStr, &t.Summary, &t.IsActive)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("thread not found: %s", threadID)
		}
		return nil, err
	}

	t.CreatedAt, _ = time.Parse(time.RFC3339, createdAtStr)
	t.LastActivityAt, _ = time.Parse(time.RFC3339, lastActivityStr)
	return &t, nil
}

// ListThreadsBySession returns all threads for a session.
func (s *SQLiteThreadStore) ListThreadsBySession(ctx context.Context, sessionID string) ([]*Thread, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, session_id, topic_label, conversation_id, created_at, last_activity, summary, is_active
		FROM session_threads
		WHERE session_id = ?
		ORDER BY last_activity DESC
	`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var threads []*Thread
	for rows.Next() {
		var t Thread
		var createdAtStr, lastActivityStr string
		if err := rows.Scan(&t.ID, &t.SessionID, &t.TopicLabel, &t.ConversationID,
			&createdAtStr, &lastActivityStr, &t.Summary, &t.IsActive); err != nil {
			return nil, err
		}
		t.CreatedAt, _ = time.Parse(time.RFC3339, createdAtStr)
		t.LastActivityAt, _ = time.Parse(time.RFC3339, lastActivityStr)
		threads = append(threads, &t)
	}
	return threads, rows.Err()
}

// UpdateThread updates an existing thread.
func (s *SQLiteThreadStore) UpdateThread(ctx context.Context, thread *Thread) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE session_threads
		SET topic_label = ?, conversation_id = ?, last_activity = ?, summary = ?, is_active = ?
		WHERE id = ? AND session_id = ?
	`,
		thread.TopicLabel,
		thread.ConversationID,
		thread.LastActivityAt.Format(time.RFC3339),
		thread.Summary,
		boolToInt(thread.IsActive),
		thread.ID,
		s.sessionID,
	)
	return err
}

// DeleteThread removes a thread.
func (s *SQLiteThreadStore) DeleteThread(ctx context.Context, threadID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM session_threads WHERE id = ? AND session_id = ?`, threadID, s.sessionID)
	return err
}

// GetActiveThread returns the active thread for a session.
func (s *SQLiteThreadStore) GetActiveThread(ctx context.Context, sessionID string) (*Thread, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, session_id, topic_label, conversation_id, created_at, last_activity, summary, is_active
		FROM session_threads
		WHERE session_id = ? AND is_active = 1
		LIMIT 1
	`, sessionID)

	var t Thread
	var createdAtStr, lastActivityStr string
	err := row.Scan(&t.ID, &t.SessionID, &t.TopicLabel, &t.ConversationID,
		&createdAtStr, &lastActivityStr, &t.Summary, &t.IsActive)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}

	t.CreatedAt, _ = time.Parse(time.RFC3339, createdAtStr)
	t.LastActivityAt, _ = time.Parse(time.RFC3339, lastActivityStr)
	return &t, nil
}

// SetActiveThread sets the active thread for a session (deactivates others).
func (s *SQLiteThreadStore) SetActiveThread(ctx context.Context, sessionID, threadID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Deactivate all threads for this session
	_, err = tx.ExecContext(ctx, `UPDATE session_threads SET is_active = 0 WHERE session_id = ?`, sessionID)
	if err != nil {
		return err
	}

	// Activate the specified thread
	_, err = tx.ExecContext(ctx, `UPDATE session_threads SET is_active = 1 WHERE id = ? AND session_id = ?`, threadID, sessionID)
	if err != nil {
		return err
	}

	return tx.Commit()
}

// CreateThreadInSession creates a new thread for a session, initializing the Threads map if needed.
func CreateThreadInSession(session *Session, topicLabel string) *Thread {
	if session.Threads == nil {
		session.Threads = make(map[string]*Thread)
	}

	threadID := "thread-" + topicLabel + "-" + id.Generate("")

	// Deactivate other threads
	for _, t := range session.Threads {
		t.IsActive = false
	}

	thread := &Thread{
		ID:             threadID,
		SessionID:      session.ID,
		TopicLabel:     topicLabel,
		ConversationID: session.ConversationID + "-" + threadID,
		CreatedAt:      time.Now().UTC(),
		LastActivityAt: time.Now().UTC(),
		IsActive:       true,
	}

	session.Threads[threadID] = thread
	session.ActiveThreadID = threadID

	return thread
}
