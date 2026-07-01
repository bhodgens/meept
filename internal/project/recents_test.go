package project

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite" // SQLite driver
)

// newTestRecentsDB opens a standalone *sql.DB with the project_recents
// table created, so tests don't need the full Store or Pool.
func newTestRecentsDB(t *testing.T) *sql.DB {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "recents.db")
	db, err := sql.Open("sqlite", "file:"+dbPath+"?_fk=1")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	_, err = db.Exec(`
		CREATE TABLE project_recents (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			project_path TEXT UNIQUE NOT NULL,
			last_used_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		)
	`)
	if err != nil {
		t.Fatal(err)
	}
	return db
}

func TestRecentsStore_TouchRecent(t *testing.T) {
	db := newTestRecentsDB(t)
	ctx := context.Background()
	s := NewRecentsStore(db)

	// First touch — should insert.
	if err := s.TouchRecent(ctx, "/path/one"); err != nil {
		t.Fatalf("TouchRecent failed: %v", err)
	}

	// Second touch (same path) — should update, not fail.
	if err := s.TouchRecent(ctx, "/path/one"); err != nil {
		t.Fatalf("TouchRecent duplicate failed: %v", err)
	}

	// Verify count — only one row.
	paths, err := s.ListRecents(ctx, 10)
	if err != nil {
		t.Fatalf("ListRecents failed: %v", err)
	}
	if len(paths) != 1 {
		t.Errorf("expected 1 path, got %d", len(paths))
	}
}

func TestRecentsStore_ListRecents(t *testing.T) {
	db := newTestRecentsDB(t)
	ctx := context.Background()
	s := NewRecentsStore(db)

	// Touch 5 paths with delays so timestamps are distinct.
	testPaths := []string{"/path/a", "/path/b", "/path/c", "/path/d", "/path/e"}
	for _, p := range testPaths {
		if err := s.TouchRecent(ctx, p); err != nil {
			t.Fatal(err)
		}
		time.Sleep(50 * time.Millisecond)
	}

	// List top 3 — should be the 3 most recently touched.
	got, err := s.ListRecents(ctx, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Errorf("expected 3 paths, got %d", len(got))
	}
	// Most recent first.
	if got[0] != "/path/e" {
		t.Errorf("got[0] = %q, want %q", got[0], "/path/e")
	}
	if got[1] != "/path/d" {
		t.Errorf("got[1] = %q, want %q", got[1], "/path/d")
	}
	if got[2] != "/path/c" {
		t.Errorf("got[2] = %q, want %q", got[2], "/path/c")
	}
}

func TestRecentsStore_PruneOlderThan(t *testing.T) {
	db := newTestRecentsDB(t)
	ctx := context.Background()
	s := NewRecentsStore(db)

	// Touch /path/one, wait, then touch /path/two.
	if err := s.TouchRecent(ctx, "/path/one"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(150 * time.Millisecond)
	if err := s.TouchRecent(ctx, "/path/two"); err != nil {
		t.Fatal(err)
	}

	// Prune entries older than 50ms — should remove /path/one only.
	deleted, err := s.PruneOlderThan(ctx, 50*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 1 {
		t.Errorf("expected 1 deleted, got %d", deleted)
	}
}

func TestRecentsStore_CapToN(t *testing.T) {
	db := newTestRecentsDB(t)
	ctx := context.Background()
	s := NewRecentsStore(db)

	// Touch 10 paths with delays so timestamps are distinct.
	paths := []string{
		"/path/a", "/path/b", "/path/c", "/path/d", "/path/e",
		"/path/f", "/path/g", "/path/h", "/path/i", "/path/j",
	}
	for _, p := range paths {
		if err := s.TouchRecent(ctx, p); err != nil {
			t.Fatal(err)
		}
		time.Sleep(50 * time.Millisecond)
	}

	// Cap to 5 — 5 should be deleted.
	deleted, err := s.CapToN(ctx, 5)
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 5 {
		t.Errorf("expected 5 deleted, got %d", deleted)
	}

	remaining, err := s.ListRecents(ctx, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(remaining) != 5 {
		t.Errorf("expected 5 remaining, got %d", len(remaining))
	}

	// Should be the 5 most recent, in DESC order: j, i, h, g, f.
	wantDesc := []string{"/path/j", "/path/i", "/path/h", "/path/g", "/path/f"}
	if len(remaining) != len(wantDesc) {
		t.Fatalf("expected %d remaining, got %d", len(wantDesc), len(remaining))
	}
	for i, want := range wantDesc {
		if remaining[i] != want {
			t.Errorf("remaining[%d] = %q, want %q", i, remaining[i], want)
		}
	}
}
