package employee

import (
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	idpkg "github.com/caimlas/meept/pkg/id"

	_ "modernc.org/sqlite" // sqlite driver registration
)

// MaxMessageBodyBytes caps inter-agent message bodies at 32KB. Enforced at
// enqueue time so oversized payloads never reach the persisted queue.
const MaxMessageBodyBytes = 32 * 1024

// Message states.
const (
	MessageStateQueued    = "queued"
	MessageStateDelivered = "delivered"
	MessageStateRead      = "read"
)

// ErrMessageTooLarge is returned by MessageStore.Enqueue when the body
// exceeds MaxMessageBodyBytes.
var ErrMessageTooLarge = errors.New("employee: message body exceeds 32KB cap")

// AgentMessage is one direct agent-to-agent message with delivery
// receipts. Messages are persisted so a message sent to a busy/offline
// employee waits until the recipient's next turn start.
type AgentMessage struct {
	ID          string
	From        string
	To          string
	Body        string
	State       string // "queued" | "delivered" | "read"
	CreatedAt   time.Time
	DeliveredAt *time.Time
}

const messageSchema = `
CREATE TABLE IF NOT EXISTS agent_messages (
    id           TEXT PRIMARY KEY,
    sender       TEXT NOT NULL,
    recipient    TEXT NOT NULL,
    body         TEXT NOT NULL,
    state        TEXT NOT NULL DEFAULT 'queued',
    created_at   TEXT NOT NULL,
    delivered_at TEXT
);
CREATE INDEX IF NOT EXISTS idx_agent_messages_recipient_state
    ON agent_messages (recipient, state);
`

// MessageStore persists agent messages in a dedicated SQLite table
// (agent_messages). Safe for concurrent use: the underlying *sql.DB is
// goroutine-safe and the store holds no mutable state beyond the handle.
type MessageStore struct {
	db  *sql.DB
	log *slog.Logger
}

// NewMessageStore opens (or creates) the SQLite database at path, runs
// migrations for the agent_messages table, and returns the store.
func NewMessageStore(path string, log *slog.Logger) (*MessageStore, error) {
	if log == nil {
		log = slog.Default()
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open message db: %w", err)
	}
	s := &MessageStore{db: db, log: log}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate message db: %w", err)
	}
	return s, nil
}

// NewMessageStoreFromDB wraps an existing *sql.DB connection. Use when
// sharing a connection with the other employee stores (one .db file per
// data dir, multiple tables).
func NewMessageStoreFromDB(db *sql.DB, log *slog.Logger) (*MessageStore, error) {
	if log == nil {
		log = slog.Default()
	}
	s := &MessageStore{db: db, log: log}
	if err := s.migrate(); err != nil {
		return nil, fmt.Errorf("migrate message db: %w", err)
	}
	return s, nil
}

func (s *MessageStore) migrate() error {
	_, err := s.db.Exec(messageSchema)
	return err
}

// Close closes the underlying database handle.
func (s *MessageStore) Close() error {
	return s.db.Close()
}

// Enqueue persists a message in state "queued", assigning an ID
// (pkg/id.Generate) and CreatedAt when unset. Body size is capped at
// MaxMessageBodyBytes.
func (s *MessageStore) Enqueue(msg *AgentMessage) error {
	if msg == nil {
		return errors.New("message enqueue: nil message")
	}
	if msg.From == "" || msg.To == "" {
		return errors.New("message enqueue: from and to are required")
	}
	if len(msg.Body) > MaxMessageBodyBytes {
		return fmt.Errorf("%w: %d > %d bytes", ErrMessageTooLarge, len(msg.Body), MaxMessageBodyBytes)
	}
	if msg.ID == "" {
		msg.ID = idpkg.Generate("msg-")
	}
	if msg.CreatedAt.IsZero() {
		msg.CreatedAt = time.Now().UTC()
	}
	msg.State = MessageStateQueued
	deliveredAt := ""
	if msg.DeliveredAt != nil {
		deliveredAt = msg.DeliveredAt.Format(time.RFC3339Nano)
	}
	_, err := s.db.Exec(`
		INSERT INTO agent_messages (id, sender, recipient, body, state, created_at, delivered_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		msg.ID, msg.From, msg.To, msg.Body, msg.State,
		msg.CreatedAt.Format(time.RFC3339Nano), deliveredAt,
	)
	if err != nil {
		return fmt.Errorf("message enqueue: insert: %w", err)
	}
	return nil
}

// MarkDelivered transitions a queued message to "delivered", stamping
// DeliveredAt. No-op when the message is missing or already delivered/read.
func (s *MessageStore) MarkDelivered(id string) error {
	now := time.Now().UTC()
	res, err := s.db.Exec(`
		UPDATE agent_messages SET state = 'delivered', delivered_at = ?
		WHERE id = ? AND state = 'queued'`,
		now.Format(time.RFC3339Nano), id,
	)
	if err != nil {
		return fmt.Errorf("message mark delivered: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		s.log.Debug("message mark delivered: no queued row", "id", id)
	}
	return nil
}

func scanMessage(row interface{ Scan(...any) error }) (AgentMessage, error) {
	var (
		m         AgentMessage
		createdAt string
		deliv     sql.NullString
	)
	if err := row.Scan(&m.ID, &m.From, &m.To, &m.Body, &m.State, &createdAt, &deliv); err != nil {
		return m, err
	}
	if t, err := time.Parse(time.RFC3339Nano, createdAt); err == nil {
		m.CreatedAt = t
	}
	if deliv.Valid && deliv.String != "" {
		if t, err := time.Parse(time.RFC3339Nano, deliv.String); err == nil {
			m.DeliveredAt = &t
		}
	}
	return m, nil
}

// DrainInbox returns up to limit queued messages for recipient `to` in FIFO
// order (oldest first) and atomically transitions them to "delivered".
// Because drained messages leave the queued set, a subsequent drain will
// not re-deliver them — this gives exactly-once turn-start injection.
func (s *MessageStore) DrainInbox(to string, limit int) ([]AgentMessage, error) {
	if limit <= 0 {
		limit = 20
	}
	tx, err := s.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("message drain: begin: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // committed or rolled back below

	rows, err := tx.Query(`
		SELECT id, sender, recipient, body, state, created_at, delivered_at
		FROM agent_messages
		WHERE recipient = ? AND state = 'queued'
		ORDER BY created_at ASC, rowid ASC
		LIMIT ?`, to, limit)
	if err != nil {
		return nil, fmt.Errorf("message drain: query: %w", err)
	}
	var msgs []AgentMessage
	var ids []string
	for rows.Next() {
		m, scanErr := scanMessage(rows)
		if scanErr != nil {
			rows.Close()
			return nil, fmt.Errorf("message drain: scan: %w", scanErr)
		}
		msgs = append(msgs, m)
		ids = append(ids, m.ID)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("message drain: rows: %w", err)
	}
	if len(ids) == 0 {
		if err := tx.Commit(); err != nil {
			return nil, fmt.Errorf("message drain: commit: %w", err)
		}
		return []AgentMessage{}, nil
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for _, mid := range ids {
		if _, err := tx.Exec(`
			UPDATE agent_messages SET state = 'delivered', delivered_at = ?
			WHERE id = ? AND state = 'queued'`, now, mid); err != nil {
			return nil, fmt.Errorf("message drain: update: %w", err)
		}
		for i := range msgs {
			if msgs[i].ID == mid && msgs[i].DeliveredAt == nil {
				t := time.Now().UTC()
				msgs[i].State = MessageStateDelivered
				msgs[i].DeliveredAt = &t
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("message drain: commit: %w", err)
	}
	return msgs, nil
}

// MarkRead transitions the given delivered messages to "read".
func (s *MessageStore) MarkRead(ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	for _, id := range ids {
		if _, err := s.db.Exec(
			`UPDATE agent_messages SET state = 'read' WHERE id = ? AND state != 'read'`, id,
		); err != nil {
			return fmt.Errorf("message mark read: %w", err)
		}
	}
	return nil
}

// --------------------------------------------------------------------------
// Roster heartbeat / reachability
// --------------------------------------------------------------------------

// seenTracker records the last time each employee was observed active
// (turn start). Guarded by mu; intentionally in-memory (reachability is
// ephemeral liveness, not durable state).
type seenTracker struct {
	mu   sync.Mutex
	seen map[string]time.Time
}

func (t *seenTracker) mark(id string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.seen == nil {
		t.seen = make(map[string]time.Time)
	}
	t.seen[id] = time.Now().UTC()
}

func (t *seenTracker) last(id string) (time.Time, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	ts, ok := t.seen[id]
	return ts, ok
}

const reachabilityStaleAfter = 10 * time.Minute

// MarkSeen records a heartbeat for employeeID (called at turn start).
func (m *Manager) MarkSeen(employeeID string) {
	if m == nil || employeeID == "" {
		return
	}
	m.seen.mark(employeeID)
}

// Reachability reports whether an employee is reachable and when it was
// last observed active. An employee is reachable when it has a recent
// heartbeat (within reachabilityStaleAfter) — i.e. its loop is active.
func (m *Manager) Reachability(employeeID string) (reachable bool, lastSeen time.Time) {
	if m == nil || employeeID == "" {
		return false, time.Time{}
	}
	ts, ok := m.seen.last(employeeID)
	return ok && time.Since(ts) <= reachabilityStaleAfter, ts
}

// EmployeeMessager is the messaging surface the Manager exposes to the
// daemon wiring. The concrete adapter over MessageStore lives in
// internal/daemon; its DrainInbox returns tool-layer message types, so
// wiring stores it as `any` via SetEmployeeMessager.
func (m *Manager) SetEmployeeMessager(ms any) {
	m.mu.Lock()
	m.employeeMessager = ms
	m.mu.Unlock()
}
