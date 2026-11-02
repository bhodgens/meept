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

	// Touch /path/one, wait, then touch /path/two. The 250ms gap gives
	// the TTL a wide margin — a CI scheduler stall between the two touches
	// must not age /path/two past the cutoff (50ms wall-clock flake).
	if err := s.TouchRecent(ctx, "/path/one"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(250 * time.Millisecond)
	if err := s.TouchRecent(ctx, "/path/two"); err != nil {
		t.Fatal(err)
	}

	// Prune entries older than 120ms — should remove /path/one only.
	deleted, err := s.PruneOlderThan(ctx, 120*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 1 {
		t.Errorf("expected 1 deleted, got %d", deleted)
	}
	// Pin the survivor by path, not just the count.
	paths, err := s.ListRecents(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 1 || paths[0] != "/path/two" {
		t.Errorf("expected [/path/two] to survive, got %v", paths)
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

// TestRecentsStore_OrderIsChronological pins the fix for a real ordering bug:
// last_used_at is stored as TEXT, so SQLite's ORDER BY compares strings. The
// old time.RFC3339Nano format TRIMS trailing zeros, so within one second
// "...:37.1Z" (nanos 100000000) sorted AFTER "...:37.123456789Z" and the older
// touch came back first. The fixed-width format makes byte order equal numeric
// order; the id tiebreaker keeps same-instant touches deterministic.
func TestRecentsStore_OrderIsChronological(t *testing.T) {
	db := newTestRecentsDB(t)
	ctx := context.Background()
	s := NewRecentsStore(db)

	// Insert rows whose timestamps are out of insertion order and whose
	// fractional parts have DIFFERENT digit counts — the exact shape that
	// tripped the trimmed-nanoseconds sort. Written directly so the test does
	// not depend on wall-clock timing.
	rows := []struct {
		path string
		ts   string
	}{
		{"/repo/earlier-9digits", "2026-11-01T20:10:37.123456789Z"},
		{"/repo/mid-1digit", "2026-11-01T20:10:37.2Z"},
		{"/repo/latest-2digits", "2026-11-01T20:10:37.30Z"},
	}
	for _, r := range rows {
		if _, err := db.ExecContext(ctx,
			`INSERT INTO project_recents (project_path, last_used_at) VALUES (?, ?)`,
			r.path, r.ts); err != nil {
			t.Fatalf("insert %s: %v", r.path, err)
		}
	}

	got, err := s.ListRecents(ctx, 10)
	if err != nil {
		t.Fatalf("ListRecents: %v", err)
	}
	want := []string{"/repo/latest-2digits", "/repo/mid-1digit", "/repo/earlier-9digits"}
	if len(got) != len(want) {
		t.Fatalf("ListRecents returned %d rows, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("recents[%d] = %q, want %q (TEXT sort must be chronological)", i, got[i], want[i])
		}
	}
}

// TestRecentsStore_OrderAcrossTimestampShapes pins the fix for a real ordering
// bug: last_used_at is TEXT, so a raw ORDER BY compares bytes. Rows written by
// TouchRecent carry a fractional second; rows created by the table DEFAULT
// CURRENT_TIMESTAMP carry none. Byte order gets that wrong — 'Z' (0x5A) sorts
// after '.' (0x2E), so a no-fraction row at :07 sorts as NEWER than a
// fractional row at :07.5. julianday() compares instants instead.
func TestRecentsStore_OrderAcrossTimestampShapes(t *testing.T) {
	db := newTestRecentsDB(t)
	ctx := context.Background()
	s := NewRecentsStore(db)

	// Insertion order deliberately contradicts chronological order, and mixes
	// shapes: fractional (9 digits), fractional (1 digit), and no fraction.
	rows := []struct {
		path string
		ts   string
	}{
		{"/repo/nine-digits", "2026-11-01T20:10:37.123456789Z"},
		{"/repo/one-digit", "2026-11-01T20:10:37.1Z"},
		{"/repo/no-fraction-earlier", "2026-11-01T20:10:36Z"},
		{"/repo/no-fraction-later", "2026-11-01T20:10:38Z"},
	}
	for _, r := range rows {
		if _, err := db.ExecContext(ctx,
			`INSERT INTO project_recents (project_path, last_used_at) VALUES (?, ?)`,
			r.path, r.ts); err != nil {
			t.Fatalf("insert %s: %v", r.path, err)
		}
	}

	got, err := s.ListRecents(ctx, 10)
	if err != nil {
		t.Fatalf("ListRecents: %v", err)
	}
	want := []string{
		"/repo/no-fraction-later",   // :38 — newest
		"/repo/nine-digits",         // :37.123456789
		"/repo/one-digit",           // :37.1
		"/repo/no-fraction-earlier", // :36 — oldest
	}
	if len(got) != len(want) {
		t.Fatalf("ListRecents returned %d rows, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("recents[%d] = %q, want %q (TEXT sort is not chronological; sort by julianday)", i, got[i], want[i])
		}
	}
}

// TestRecentsStore_CapToNKeepsNewest pins that the cap uses the same instant
// ordering, so a mixed-shape table never truncates the wrong end.
func TestRecentsStore_CapToNKeepsNewest(t *testing.T) {
	db := newTestRecentsDB(t)
	ctx := context.Background()
	s := NewRecentsStore(db)

	rows := []struct {
		path string
		ts   string
	}{
		{"/repo/oldest", "2026-11-01T20:10:36Z"},
		{"/repo/mid", "2026-11-01T20:10:37.1Z"},
		{"/repo/newest", "2026-11-01T20:10:37.123456789Z"},
	}
	for _, r := range rows {
		if _, err := db.ExecContext(ctx,
			`INSERT INTO project_recents (project_path, last_used_at) VALUES (?, ?)`,
			r.path, r.ts); err != nil {
			t.Fatalf("insert %s: %v", r.path, err)
		}
	}

	if _, err := s.CapToN(ctx, 2); err != nil {
		t.Fatalf("CapToN: %v", err)
	}
	got, err := s.ListRecents(ctx, 10)
	if err != nil {
		t.Fatalf("ListRecents: %v", err)
	}
	want := []string{"/repo/newest", "/repo/mid"}
	if len(got) != len(want) {
		t.Fatalf("after CapToN(2) got %d rows %v, want %d", len(got), got, len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("kept[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}
