package session

// Session-row upsert for conversation-only history (03-context leaf 01,
// C4 / F-ORCH-29 / W11). A chat.submit carrying only a conversation_id
// never creates a sessions row, so session_messages inserts hit the
// sessions(id) foreign key ("FOREIGN KEY constraint failed (787)") and
// the exchange is silently lost. EnsureSessionRow upserts the backing
// row BEFORE any message write so conversation-only history survives.

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// ensureSessionRowInTx guarantees the sessions row backing sessionID
// exists INSIDE an open transaction, before any session_messages insert
// that the sessions(id) foreign key gates (SQLite error 787). Callers
// hold s.mu (one serialized sqlite connection), so the SELECT-then-INSERT
// pair cannot interleave with a racing writer, and ON CONFLICT DO NOTHING
// keeps the write idempotent even if a concurrent commit landed the row.
//
// Creating an existing row is a pure no-op: an existing session's fields
// (project binding, worktree, detection context, owner) are never
// touched. New rows are UNBOUND, keyed by sessionID as the primary id -
// the key conversation-only submits persist messages under.
func (s *SQLiteStore) ensureSessionRowInTx(tx *sql.Tx, sessionID string) error {
	if sessionID == "" {
		return fmt.Errorf("ensure session row: empty session id")
	}

	var one int
	err := tx.QueryRow(`SELECT 1 FROM sessions WHERE id = ? LIMIT 1`, sessionID).Scan(&one)
	if err == nil {
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("failed to probe sessions row for %q: %w", sessionID, err)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := tx.Exec(`
		INSERT INTO sessions (id, name, conversation_id, created_at, last_activity, attached_clients, worker_ids, description, leaf_message_id, project_id, project_path, no_fence, detection_context, schema_version)
		VALUES (?, ?, ?, ?, ?, '[]', '[]', '', NULL, '', '', false, '', ?)
		ON CONFLICT DO NOTHING`,
		sessionID, sessionID, sessionID, now, now, SessionFormatVersion,
	); err != nil {
		return fmt.Errorf("failed to upsert sessions row for %q: %w", sessionID, err)
	}
	return nil
}

// EnsureSessionRow guarantees a sessions row backing conversationID,
// creating it ONLY when absent. Idempotent and race-safe: callers share
// the store mutex and the INSERT uses ON CONFLICT DO NOTHING, so
// concurrent same-conversation writers produce exactly ONE row.
//
// Never widens an existing session: an existing row (matched by primary
// id OR conversation id) is returned untouched - project binding,
// worktree, detection context, owner and every other field stay as the
// session's own creation/binding paths wrote them. The created row is
// UNBOUND (no project/worktree/detection context), keyed by the
// conversation id as its primary id because that is the key
// conversation-only submits persist messages under.
func (s *SQLiteStore) EnsureSessionRow(conversationID string) (*Session, error) {
	if conversationID == "" {
		return nil, fmt.Errorf("ensure session row: empty conversation id")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if existing := s.getByColumnUnsafe("id", conversationID); existing != nil {
		return existing, nil
	}
	if existing := s.getByColumnUnsafe("conversation_id", conversationID); existing != nil {
		return existing, nil
	}

	now := time.Now().UTC()
	sess := &Session{
		ID:              conversationID,
		Name:            conversationID,
		ConversationID:  conversationID,
		CreatedAt:       now,
		LastActivity:    now,
		AttachedClients: []string{},
		WorkerIDs:       []string{},
	}

	if _, err := s.db.Exec(`
		INSERT INTO sessions (id, name, conversation_id, created_at, last_activity, attached_clients, worker_ids, description, leaf_message_id, project_id, project_path, no_fence, detection_context, schema_version)
		VALUES (?, ?, ?, ?, ?, '[]', '[]', '', NULL, '', '', false, '', ?)
		ON CONFLICT DO NOTHING`,
		sess.ID,
		sess.Name,
		sess.ConversationID,
		sess.CreatedAt.Format(time.RFC3339),
		sess.LastActivity.Format(time.RFC3339),
		SessionFormatVersion,
	); err != nil { //nolint:mutexio // mutex serializes sqlite connection access
		return nil, fmt.Errorf("failed to upsert sessions row for %q: %w", conversationID, err)
	}

	// Read back the authoritative row: a racing writer may have won the
	// insert between our miss and our write (ON CONFLICT DO NOTHING), so
	// the returned Session must reflect what actually landed.
	if existing := s.getByColumnUnsafe("id", conversationID); existing != nil {
		return existing, nil
	}
	if existing := s.getByColumnUnsafe("conversation_id", conversationID); existing != nil {
		return existing, nil
	}
	return nil, fmt.Errorf("sessions row for %q missing after upsert", conversationID)
}
