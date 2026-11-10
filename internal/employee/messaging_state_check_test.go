package employee

import (
	"database/sql"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite" //nolint:revive // blank import for side effects
)

// leaf 06 T6 (audit F6): agent_messages.state CHECK constraint + the legacy
// pre-CHECK rebuild path. Mirrors the queue jobs F7 migration trio.

const legacyMessageSchemaDDL = `
CREATE TABLE agent_messages (
    id           TEXT PRIMARY KEY,
    sender       TEXT NOT NULL,
    recipient    TEXT NOT NULL,
    body         TEXT NOT NULL,
    state        TEXT NOT NULL DEFAULT 'queued',
    created_at   TEXT NOT NULL,
    delivered_at TEXT
);
CREATE INDEX idx_agent_messages_recipient_state
    ON agent_messages (recipient, state);
`

func createLegacyMessageDB(t *testing.T, dbPath string) {
	t.Helper()
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open legacy db: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(legacyMessageSchemaDDL); err != nil {
		t.Fatalf("create legacy schema: %v", err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.Exec(`INSERT INTO agent_messages (id, sender, recipient, body, state, created_at)
		VALUES ('msg-legacy', 'a', 'b', 'survive-me', 'queued', ?)`, now); err != nil {
		t.Fatalf("insert legacy row: %v", err)
	}
	// delivered_at stays NULL on the legacy row: the rebuild must preserve it.
}

func messageTableHasCheck(t *testing.T, db *sql.DB) bool {
	t.Helper()
	var tableSQL string
	if err := db.QueryRow(`SELECT sql FROM sqlite_master WHERE type='table' AND name='agent_messages'`).Scan(&tableSQL); err != nil {
		t.Fatalf("read agent_messages sql: %v", err)
	}
	return strings.Contains(strings.ToUpper(tableSQL), "CHECK")
}

func TestMessageStateCheckFreshDBRefusesGarbage(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "fresh.db")
	store, err := NewMessageStore(dbPath, slog.Default())
	if err != nil {
		t.Fatalf("NewMessageStore: %v", err)
	}
	defer store.Close()

	if !messageTableHasCheck(t, store.db) {
		t.Fatal("fresh agent_messages table lacks the state CHECK")
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, err = store.db.Exec(`INSERT INTO agent_messages (id, sender, recipient, body, state, created_at)
		VALUES ('garbage', 'a', 'b', 'x', 'wat_is_this', ?)`, now)
	if err == nil {
		t.Fatal("expected constraint error on agent_messages.state garbage insert, got none")
	}
	// Enum values remain insertable.
	if err := store.Enqueue(&AgentMessage{From: "a", To: "b", Body: "ok"}); err != nil {
		t.Fatalf("Enqueue of a valid message failed: %v", err)
	}
}

func TestMessageStateCheckMigratedDB(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "legacy.db")
	createLegacyMessageDB(t, dbPath)

	probeDB, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("probe open: %v", err)
	}
	if messageTableHasCheck(t, probeDB) {
		probeDB.Close()
		t.Fatal("fixture already has CHECK — fixture is invalid")
	}
	probeDB.Close()

	store, err := NewMessageStore(dbPath, slog.Default())
	if err != nil {
		t.Fatalf("open legacy db through store: %v", err)
	}
	defer store.Close()

	// 1. CHECK landed.
	if !messageTableHasCheck(t, store.db) {
		t.Fatal("agent_messages still lacks CHECK after migration")
	}

	// 2. Garbage INSERT refused.
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := store.db.Exec(`INSERT INTO agent_messages (id, sender, recipient, body, state, created_at)
		VALUES ('garbage-migrated', 'a', 'b', 'x', 'wat_is_this', ?)`, now); err == nil {
		t.Fatal("expected constraint error on migrated DB, got none")
	}

	// 3. Row survived intact (body + NULL delivered_at preserved).
	var body string
	var deliv sql.NullString
	if err := store.db.QueryRow(`SELECT body, delivered_at FROM agent_messages WHERE id = 'msg-legacy'`).Scan(&body, &deliv); err != nil {
		t.Fatalf("legacy row lost: %v", err)
	}
	if body != "survive-me" {
		t.Errorf("legacy body = %q, want survive-me", body)
	}
	if deliv.Valid {
		t.Errorf("legacy NULL delivered_at became %q; want NULL preserved", deliv.String)
	}

	// 4. Index parity: the recipient_state index survived the rebuild.
	var idxCount int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND tbl_name='agent_messages' AND name='idx_agent_messages_recipient_state'`).Scan(&idxCount); err != nil {
		t.Fatalf("index query: %v", err)
	}
	if idxCount != 1 {
		t.Error("idx_agent_messages_recipient_state missing after rebuild")
	}

	// 5. The full lifecycle still works on the migrated table:
	// queued -> delivered -> read via the store's own paths.
	if err := store.MarkDelivered("msg-legacy"); err != nil {
		t.Fatalf("MarkDelivered: %v", err)
	}
	if err := store.MarkRead([]string{"msg-legacy"}); err != nil {
		t.Fatalf("MarkRead: %v", err)
	}
	var state string
	if err := store.db.QueryRow(`SELECT state FROM agent_messages WHERE id = 'msg-legacy'`).Scan(&state); err != nil {
		t.Fatalf("read state: %v", err)
	}
	if state != MessageStateRead {
		t.Errorf("state after lifecycle = %q, want %q", state, MessageStateRead)
	}

	// 6. Idempotence: reopen is a silent no-op, CHECK stays.
	store.Close()
	store2, err := NewMessageStore(dbPath, slog.Default())
	if err != nil {
		t.Fatalf("reopen migrated db: %v", err)
	}
	defer store2.Close()
	if !messageTableHasCheck(t, store2.db) {
		t.Fatal("CHECK vanished on reopen")
	}
}

func TestMessageStateCheckMigrationRefusesGarbageRows(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "garbage.db")
	createLegacyMessageDB(t, dbPath)

	probeDB, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := probeDB.Exec(`INSERT INTO agent_messages (id, sender, recipient, body, state, created_at)
		VALUES ('bad-state-row', 'a', 'b', 'x', 'wat_is_this', ?)`, now); err != nil {
		probeDB.Close()
		t.Fatalf("insert bad row: %v", err)
	}
	probeDB.Close()

	if _, err := NewMessageStore(dbPath, nil); err == nil {
		t.Fatal("expected migration error for out-of-enum row, got none")
	} else if !strings.Contains(err.Error(), "wat_is_this") {
		t.Errorf("migration error %q does not name the offending value", err)
	}

	// Data untouched: the bad row is still on disk (migration rolled back).
	recoverDB, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer recoverDB.Close()
	var state string
	if err := recoverDB.QueryRow(`SELECT state FROM agent_messages WHERE id = 'bad-state-row'`).Scan(&state); err != nil {
		t.Fatalf("bad row was deleted or rewritten: %v", err)
	}
	if state != "wat_is_this" {
		t.Errorf("bad row state = %q, want original 'wat_is_this'", state)
	}
}
