package agent

// Owner-column migration tests (C-K1, tree 04 leaf 01 T1).
//
// A pre-migration parks.db (parked_turns WITHOUT the owner column) must
// open cleanly through NewSQLiteParkStore, backfill every legacy row to
// its owning parker by kind/class — chat quota -> 'quota-watcher', chat
// throttle -> 'throttle', episode -> 'episode', gate -> 'gate', unknown
// chat class -> 'quota-watcher' (dropped at first Load) — and stay fully
// functional afterwards (Save stamps the new column, Load filters by it).

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/llm"

	_ "modernc.org/sqlite"
)

// createLegacyParkDB writes a parks.db in the PRE-C-K1 schema (no owner
// column) and returns its path. The schema copy is the historical
// parkedTurnsSchema minus the owner column — kept literal here on purpose:
// if the current schema ever drifts, this test still exercises the ALTER
// path against the true legacy shape.
func createLegacyParkDB(t *testing.T, dbPath string) {
	t.Helper()
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open legacy db: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(`
CREATE TABLE parked_turns (
	id            INTEGER PRIMARY KEY AUTOINCREMENT,
	kind          TEXT NOT NULL,
	park_key      TEXT NOT NULL,
	session_id    TEXT NOT NULL DEFAULT '',
	conversation_id TEXT NOT NULL DEFAULT '',
	agent_id      TEXT NOT NULL DEFAULT '',
	class         TEXT NOT NULL DEFAULT '',
	resume_at     TEXT NOT NULL,
	attempt       INTEGER NOT NULL DEFAULT 0,
	max_attempts  INTEGER NOT NULL DEFAULT 0,
	turn_payload  TEXT NOT NULL DEFAULT '',
	created_at    TEXT NOT NULL,
	UNIQUE(kind, park_key)
);
CREATE INDEX idx_parked_turns_kind_resume ON parked_turns(kind, resume_at);`); err != nil {
		t.Fatalf("create legacy schema: %v", err)
	}
	resumeAt := time.Now().Add(2 * time.Hour).UTC().Format(time.RFC3339Nano)
	ins := `INSERT INTO parked_turns (kind, park_key, session_id, conversation_id, agent_id, class, resume_at, attempt, max_attempts, turn_payload, created_at) VALUES (?,?,?,?,?,?,?,?,?,?,?)`
	legacyRows := [][6]string{
		{"chat", "quota|s-q|t1", "s-q", "conv-q", "a", "quota"},
		{"chat", "throttle|s-t|t1", "s-t", "conv-t", "a", "throttle"},
		{"episode", "emp|assess|trig", "s-e", "s-e", "emp-1", "quota"},
		{"gate", "plan|p-1", "s-g", "s-g", "gate", "none"},
		{"chat", "weird|s-w|t1", "s-w", "conv-w", "a", "server_error"}, // unknown class
	}
	for _, r := range legacyRows {
		if _, err := db.Exec(ins, r[0], r[1], r[2], r[3], r[4], r[5], resumeAt, 0, 0, `{}`, resumeAt); err != nil {
			t.Fatalf("insert legacy row %s/%s: %v", r[0], r[1], err)
		}
	}
}

func TestParkStore_LegacyDBMigratesOwnerColumn(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "parks.db")
	createLegacyParkDB(t, dbPath)

	store, err := NewSQLiteParkStore(dbPath, newParkStoreTestLogger())
	if err != nil {
		t.Fatalf("open legacy db through the store: %v", err)
	}
	defer store.Close()

	// Backfill partition (kind/class aware, evidence in leaf notes).
	type ownerCase struct{ kind, key, wantOwner string }
	for _, tc := range []ownerCase{
		{"chat", "quota|s-q|t1", "quota-watcher"},
		{"chat", "throttle|s-t|t1", "throttle"},
		{"episode", "emp|assess|trig", "episode"},
		{"gate", "plan|p-1", "gate"},
		{"chat", "weird|s-w|t1", "quota-watcher"}, // catch-all (drops at Load)
	} {
		var got string
		row := store.db.QueryRowContext(ctx,
			`SELECT owner FROM parked_turns WHERE kind = ? AND park_key = ?`, tc.kind, tc.key)
		if err := row.Scan(&got); err != nil {
			t.Fatalf("read owner for %s/%s: %v", tc.kind, tc.key, err)
		}
		if got != tc.wantOwner {
			t.Errorf("backfilled owner for %s/%s = %q, want %q", tc.kind, tc.key, got, tc.wantOwner)
		}
	}

	// Functional after migration: the unknown-class chat row DROPS at the
	// first owner-filtered Load (C-K1 strict class parse), the quota row
	// survives, and throttle/gate/episode rows are invisible to the
	// quota-watcher's scoped load.
	recs, _, err := store.LoadOwned(ctx, ParkKindChat, "quota-watcher", time.Now())
	if err != nil {
		t.Fatalf("owner-scoped load after migrate: %v", err)
	}
	if len(recs) != 1 || recs[0].SessionID != "s-q" {
		t.Fatalf("quota-watcher load = %d rows %+v, want exactly the 1 quota row (unknown-class row must drop)", len(recs), recs)
	}
	var left int
	if err := store.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM parked_turns WHERE kind='chat' AND class NOT IN ('quota','throttle')`).Scan(&left); err != nil {
		t.Fatalf("count unknown rows: %v", err)
	}
	if left != 0 {
		t.Errorf("unknown-class chat row survived Load: %d left, want 0 (dropped)", left)
	}
	recs, _, err = store.LoadOwned(ctx, ParkKindChat, "throttle", time.Now())
	if err != nil {
		t.Fatalf("throttle-scoped load: %v", err)
	}
	if len(recs) != 1 || recs[0].Class != llm.FailureThrottle {
		t.Fatalf("throttle load = %+v, want exactly 1 throttle row", recs)
	}

	// Save stamps the new column end-to-end on a migrated db.
	if err := store.Save(ctx, ParkKindChat, "quota|s-new|t2", ParkedTurnRecord{
		SessionID: "s-new", Class: llm.FailureQuota,
		ResumeAt: time.Now().Add(time.Hour), Owner: "quota-watcher",
	}); err != nil {
		t.Fatalf("save on migrated db: %v", err)
	}
	var owner string
	if err := store.db.QueryRowContext(ctx,
		`SELECT owner FROM parked_turns WHERE kind='chat' AND park_key='quota|s-new|t2'`).Scan(&owner); err != nil {
		t.Fatalf("read stamped owner: %v", err)
	}
	if owner != "quota-watcher" {
		t.Errorf("Save did not stamp owner on migrated db: %q", owner)
	}

	// Re-open (idempotent migrate): no error, rows intact.
	again, err := NewSQLiteParkStore(dbPath, newParkStoreTestLogger())
	if err != nil {
		t.Fatalf("reopen migrated db: %v", err)
	}
	defer again.Close()
	if n := again.persistedRowCount(t, ParkKindChat); n != 3 {
		// quota s-q + throttle s-t + quota s-new (unknown-class dropped,
		// gate/episode are other kinds).
		t.Errorf("chat rows after reopen = %d, want 3", n)
	}
}
