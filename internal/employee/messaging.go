package employee

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
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

// AgentInboxBody is the optional typed envelope carried in an
// AgentMessage's Body field (inbox leaf 03, C4). Legacy messages keep
// raw free-text bodies; when a sender supplies structured context, the
// tool layer marshals this envelope into Body before enqueueing, and
// consumers decode it with ParseAgentInboxBody. The JSON tags are the
// wire contract.
type AgentInboxBody struct {
	Body       string           `json:"body"`
	Structured *InboxStructured `json:"structured,omitempty"`
}

// InboxStructured is the machine-readable payload alongside the human
// text: the tools the recipient should reach for, the files in play,
// and the plan entries the message relates to. All fields optional.
type InboxStructured struct {
	ToolTargets []string `json:"tool_targets,omitempty"`
	FilePaths   []string `json:"file_paths,omitempty"`
	PlanEntries []string `json:"plan_entries,omitempty"`
}

// ParseAgentInboxBody decodes a stored message body into an
// AgentInboxBody. Body strings that are not a valid typed envelope
// (legacy raw text, truncated JSON) fall back verbatim:
// {Body: raw, Structured: nil}.
func ParseAgentInboxBody(raw string) AgentInboxBody {
	var parsed AgentInboxBody
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return AgentInboxBody{Body: raw}
	}
	return parsed
}

const messageSchema = `
CREATE TABLE IF NOT EXISTS agent_messages (
    id           TEXT PRIMARY KEY,
    sender       TEXT NOT NULL,
    recipient    TEXT NOT NULL,
    body         TEXT NOT NULL,
    -- state is pinned to the message enum (audit F6, leaf 06 T6), the same
    -- pattern as queue jobs.state: the DB refuses garbage the code would
    -- never write; migrateMessageStateCheck carries pre-CHECK databases
    -- through a guarded table rebuild.
    state        TEXT NOT NULL DEFAULT 'queued' CHECK (state IN ('queued','delivered','read')),
    created_at   TEXT NOT NULL,
    delivered_at TEXT
);
CREATE INDEX IF NOT EXISTS idx_agent_messages_recipient_state
    ON agent_messages (recipient, state);
`

func (s *MessageStore) migrate() error {
	if _, err := s.db.Exec(messageSchema); err != nil {
		return err
	}
	return s.migrateMessageStateCheck()
}

// migrateMessageStateCheck adds the state CHECK to a pre-CHECK
// agent_messages table (leaf 06 T6, mirroring the queue jobs F7 pattern).
// SQLite cannot ALTER TABLE ADD CONSTRAINT, so: probe sqlite_master for
// CHECK (no-op on fresh + already-migrated DBs), pre-flight refuse any row
// outside the enum (data is never deleted or rewritten to fit), then rebuild
// in one transaction — new table, copy, swap names, recreate the captured
// indexes. agent_messages has no child references, so the plain rename-swap
// is FK-safe without the pragma dance the plan store needs.
func (s *MessageStore) migrateMessageStateCheck() error {
	var tableSQL string
	err := s.db.QueryRow(`SELECT sql FROM sqlite_master WHERE type='table' AND name='agent_messages'`).Scan(&tableSQL)
	if errors.Is(err, sql.ErrNoRows) {
		return nil // table just created by messageSchema (with CHECK); defensive
	}
	if err != nil {
		return fmt.Errorf("message CHECK migration probe: %w", err)
	}
	if strings.Contains(strings.ToUpper(tableSQL), "CHECK") {
		return nil // fresh schema or already migrated — nothing to do
	}

	// Live column set from PRAGMA (the rebuild must carry every column).
	colRows, err := s.db.Query(`PRAGMA table_info(agent_messages)`)
	if err != nil {
		return fmt.Errorf("message CHECK migration table_info: %w", err)
	}
	var cols []string
	for colRows.Next() {
		var (
			cid     int
			name    string
			ctype   string
			notnull int
			dflt    sql.NullString
			pk      int
		)
		if err := colRows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			colRows.Close()
			return fmt.Errorf("message CHECK migration table_info scan: %w", err)
		}
		cols = append(cols, name)
	}
	if err := colRows.Err(); err != nil {
		colRows.Close()
		return fmt.Errorf("message CHECK migration table_info iteration: %w", err)
	}
	colRows.Close()
	if len(cols) == 0 {
		return fmt.Errorf("message CHECK migration: PRAGMA table_info(agent_messages) returned no columns")
	}

	idxRows, err := s.db.Query(`SELECT sql FROM sqlite_master WHERE type='index' AND tbl_name='agent_messages' AND sql IS NOT NULL`)
	if err != nil {
		return fmt.Errorf("message CHECK migration index capture: %w", err)
	}
	var indexSQL []string
	for idxRows.Next() {
		var sqlText string
		if err := idxRows.Scan(&sqlText); err != nil {
			idxRows.Close()
			return fmt.Errorf("message CHECK migration index scan: %w", err)
		}
		indexSQL = append(indexSQL, sqlText)
	}
	if err := idxRows.Err(); err != nil {
		idxRows.Close()
		return fmt.Errorf("message CHECK migration index iteration: %w", err)
	}
	idxRows.Close()

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin message CHECK migration: %w", err)
	}
	defer func() {
		if rErr := tx.Rollback(); rErr != nil && !errors.Is(rErr, sql.ErrTxDone) {
			s.log.Debug("message CHECK migration rollback (no-op after commit)", "error", rErr)
		}
	}()

	var badCount int
	var badStates sql.NullString
	if err := tx.QueryRow(`SELECT
		(SELECT COUNT(*) FROM agent_messages WHERE state NOT IN ('queued','delivered','read')),
		(SELECT group_concat(s, ', ') FROM
			(SELECT DISTINCT state AS s FROM agent_messages WHERE state NOT IN ('queued','delivered','read')))`,
	).Scan(&badCount, &badStates); err != nil {
		return fmt.Errorf("message CHECK migration pre-flight: %w", err)
	}
	if badCount > 0 {
		return fmt.Errorf("message CHECK migration refused: %d row(s) have state outside the message enum (values: %s); repair them before upgrading",
			badCount, badStates.String)
	}

	if _, err := tx.Exec(`CREATE TABLE agent_messages_new (
		id           TEXT PRIMARY KEY,
		sender       TEXT NOT NULL,
		recipient    TEXT NOT NULL,
		body         TEXT NOT NULL,
		state        TEXT NOT NULL DEFAULT 'queued' CHECK (state IN ('queued','delivered','read')),
		created_at   TEXT NOT NULL,
		delivered_at TEXT
	)`); err != nil {
		return fmt.Errorf("message CHECK migration create agent_messages_new: %w", err)
	}
	quoted := make([]string, len(cols))
	for i, c := range cols {
		quoted[i] = `"` + strings.ReplaceAll(c, `"`, `""`) + `"`
	}
	colsJoined := strings.Join(quoted, ", ")
	if _, err := tx.Exec(fmt.Sprintf("INSERT INTO agent_messages_new (%s) SELECT %s FROM agent_messages", colsJoined, colsJoined)); err != nil {
		return fmt.Errorf("message CHECK migration copy rows: %w", err)
	}
	if _, err := tx.Exec(`DROP TABLE agent_messages`); err != nil {
		return fmt.Errorf("message CHECK migration drop old agent_messages: %w", err)
	}
	if _, err := tx.Exec(`ALTER TABLE agent_messages_new RENAME TO agent_messages`); err != nil {
		return fmt.Errorf("message CHECK migration rename: %w", err)
	}
	for _, stmt := range indexSQL {
		if _, err := tx.Exec(stmt); err != nil {
			return fmt.Errorf("message CHECK migration recreate index %q: %w", stmt, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit message CHECK migration: %w", err)
	}
	s.log.Warn("message CHECK migration: rebuilt agent_messages with state CHECK")
	return nil
}

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
	// Closed via defer: scan errors and later returns inside the
	// transaction must not leak the rows handle.
	defer rows.Close()
	for rows.Next() {
		m, scanErr := scanMessage(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("message drain: scan: %w", scanErr)
		}
		msgs = append(msgs, m)
		ids = append(ids, m.ID)
	}
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

// MarkRead transitions the given delivered messages to "read". Only
// DELIVERED rows advance: a queued message may not skip to read (leaf 06
// T5) — that would erase the delivery receipt the DrainInbox stamp is the
// record of.
func (s *MessageStore) MarkRead(ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	for _, id := range ids {
		if _, err := s.db.Exec(
			`UPDATE agent_messages SET state = 'read' WHERE id = ? AND state = 'delivered'`, id,
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
