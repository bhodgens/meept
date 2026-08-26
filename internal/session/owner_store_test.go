package session

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

// newOwnerTestSQLiteStore creates a SQLite store in a temp dir for
// ownership tests.
func newOwnerTestSQLiteStore(t *testing.T) *SQLiteStore {
	t.Helper()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "sessions.db"), nil)
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

// TestSQLiteStore_OwnerIDMigrationOnPreMultiuserDB opens a store against a
// fixture carrying the pre-multiuser sessions schema with an existing row,
// and verifies the owner_id column is added nullable WITHOUT touching the
// existing data.
func TestSQLiteStore_OwnerIDMigrationOnPreMultiuserDB(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "old_sessions.db")

	// Build the old-style database: sessions table without owner_id.
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open fixture db: %v", err)
	}
	oldSchema := `
	CREATE TABLE sessions (
		id              TEXT PRIMARY KEY,
		name            TEXT NOT NULL,
		conversation_id TEXT UNIQUE NOT NULL,
		created_at      TEXT NOT NULL,
		last_activity   TEXT NOT NULL,
		attached_clients TEXT DEFAULT '[]',
		worker_ids      TEXT DEFAULT '[]',
		description     TEXT DEFAULT ''
	);`
	if _, err := db.Exec(oldSchema); err != nil {
		db.Close()
		t.Fatalf("create old schema: %v", err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.Exec(
		`INSERT INTO sessions (id, name, conversation_id, created_at, last_activity, attached_clients, worker_ids, description)
		 VALUES ('session-legacy1', 'legacy session', 'conv-legacy1', ?, ?, '[]', '[]', 'kept')`,
		now, now,
	); err != nil {
		db.Close()
		t.Fatalf("insert legacy row: %v", err)
	}
	db.Close()

	// Open through the real store — the migration must add owner_id.
	fixtureStore, err := NewSQLiteStore(dbPath, nil)
	if err != nil {
		t.Fatalf("open migrated store: %v", err)
	}
	defer fixtureStore.Close()

	mdb, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("reopen migrated db: %v", err)
	}
	defer mdb.Close()

	rows, err := mdb.Query("PRAGMA table_info(sessions)")
	if err != nil {
		t.Fatalf("pragma table_info: %v", err)
	}
	defer rows.Close()
	columns := make(map[string]bool)
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull int
		var dfltValue sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dfltValue, &pk); err != nil {
			t.Fatalf("scan table info: %v", err)
		}
		columns[name] = true
	}
	if !columns["owner_id"] {
		t.Fatal("owner_id column missing after migration")
	}

	// Existing data intact and reports NULL owner (scans as "").
	sess := fixtureStore.Get("session-legacy1")
	if sess == nil {
		t.Fatal("legacy session lost during migration")
	}
	if sess.OwnerID != "" {
		t.Errorf("legacy session OwnerID = %q, want empty", sess.OwnerID)
	}
	if sess.Description != "kept" {
		t.Errorf("legacy description changed: %q", sess.Description)
	}
}

func TestSQLiteStore_CreateForOwnerStampsOwner(t *testing.T) {
	store := newOwnerTestSQLiteStore(t)
	ctx := context.Background()

	sess, err := store.CreateForOwner(ctx, CreateForOwnerRequest{Name: "alice session", OwnerID: "user-alice"})
	if err != nil {
		t.Fatalf("CreateForOwner: %v", err)
	}
	if sess.OwnerID != "user-alice" {
		t.Fatalf("OwnerID = %q, want user-alice", sess.OwnerID)
	}

	got := store.Get(sess.ID)
	if got == nil || got.OwnerID != "user-alice" {
		t.Fatalf("persisted OwnerID = %+v, want user-alice", got)
	}
}

func TestViewerFiltering_Sqlite(t *testing.T) {
	store := newOwnerTestSQLiteStore(t)
	ctx := context.Background()

	aSess, err := store.CreateForOwner(ctx, CreateForOwnerRequest{Name: "a", OwnerID: "user-a"})
	if err != nil {
		t.Fatalf("create a: %v", err)
	}
	bSess, err := store.CreateForOwner(ctx, CreateForOwnerRequest{Name: "b", OwnerID: "user-b"})
	if err != nil {
		t.Fatalf("create b: %v", err)
	}
	legacy, err := store.Create("legacy unowned")
	if err != nil {
		t.Fatalf("create legacy: %v", err)
	}

	viewerA := NewViewer("user-a")

	// User A lists own sessions plus unowned legacy ones, never B's.
	listA, err := store.ListForViewer(ctx, viewerA)
	if err != nil {
		t.Fatalf("ListForViewer(a): %v", err)
	}
	ids := map[string]bool{}
	for _, s := range listA {
		ids[s.ID] = true
	}
	if !ids[aSess.ID] || !ids[legacy.ID] {
		t.Errorf("viewer A list missing own/unowned sessions: %v", ids)
	}
	if ids[bSess.ID] {
		t.Error("viewer A sees user B's owned session")
	}

	// Nil viewer is unfiltered.
	all, err := store.ListForViewer(ctx, nil)
	if err != nil {
		t.Fatalf("ListForViewer(nil): %v", err)
	}
	if len(all) != 3 {
		t.Errorf("nil viewer saw %d sessions, want 3", len(all))
	}

	// Get is scoped the same way.
	if got := store.GetForViewer(ctx, viewerA, bSess.ID); got != nil {
		t.Error("viewer A can read user B's session via GetForViewer")
	}
	if got := store.GetForViewer(ctx, viewerA, aSess.ID); got == nil {
		t.Error("viewer A cannot read own session")
	}
	if got := store.GetForViewer(ctx, nil, bSess.ID); got == nil {
		t.Error("nil viewer cannot read an existing session")
	}
}

func TestMemoryStore_OwnerParity(t *testing.T) {
	store := NewMemoryStore(nil)
	ctx := context.Background()

	aSess, err := store.CreateForOwner(ctx, CreateForOwnerRequest{Name: "a", OwnerID: "user-a"})
	if err != nil {
		t.Fatalf("CreateForOwner: %v", err)
	}
	bSess, err := store.CreateForOwner(ctx, CreateForOwnerRequest{Name: "b", OwnerID: "user-b"})
	if err != nil {
		t.Fatalf("CreateForOwner: %v", err)
	}

	// MemoryStore.List only surfaces sessions with at least one assistant
	// message; give each session one so listing exercises the filter path.
	for _, s := range []*Session{aSess, bSess} {
		if err := store.SaveMessages(s.ID, []Message{{Role: "assistant", Content: "hi", Timestamp: time.Now().UTC()}}); err != nil {
			t.Fatalf("SaveMessages: %v", err)
		}
	}

	viewerB := NewViewer("user-b")
	listB, err := store.ListForViewer(ctx, viewerB)
	if err != nil {
		t.Fatalf("ListForViewer(b): %v", err)
	}
	if len(listB) != 1 || listB[0].ID != bSess.ID {
		t.Errorf("viewer B list = %d entries, want exactly own session", len(listB))
	}

	listNil, err := store.ListForViewer(ctx, nil)
	if err != nil {
		t.Fatalf("ListForViewer(nil): %v", err)
	}
	if len(listNil) < 2 {
		t.Errorf("nil viewer list = %d entries, want both", len(listNil))
	}

	if got := store.GetForViewer(ctx, viewerB, aSess.ID); got != nil {
		t.Error("viewer B can read user A's memory-store session")
	}
	if got := store.GetForViewer(ctx, viewerB, bSess.ID); got == nil {
		t.Error("viewer B cannot read own memory-store session")
	}
}

func TestVisibleToRules(t *testing.T) {
	owned := &Session{OwnerID: "user-a"}
	unowned := &Session{OwnerID: ""}

	cases := []struct {
		name   string
		sess   *Session
		viewer *Viewer
		want   bool
	}{
		{"nil viewer sees everything", owned, nil, true},
		{"owner sees own", owned, NewViewer("user-a"), true},
		{"other user denied owned", owned, NewViewer("user-b"), false},
		{"unowned visible to all", unowned, NewViewer("user-b"), true},
		{"missing session invisible", nil, NewViewer("user-a"), false},
		{"missing session nil viewer", nil, nil, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := VisibleTo(tc.sess, tc.viewer); got != tc.want {
				t.Errorf("VisibleTo(%v, %v) = %v, want %v", tc.sess, tc.viewer, got, tc.want)
			}
		})
	}
}

// compile-time check: both stores implement OwnerStore.
var (
	_ OwnerStore = (*SQLiteStore)(nil)
	_ OwnerStore = (*MemoryStore)(nil)
)
