package knowledge

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/caimlas/meept/internal/tools/builtin"
)

func newAdapterTestStore(t *testing.T) *Store {
	t.Helper()
	return newTestStore(t, 0)
}

// TestAdapterRoundtrip exercises the builtin.KnowledgeStore seam against a
// real Store on t.TempDir: ingest through the adapter, read back, search,
// and the compile-time interface assertion.
func TestAdapterRoundtrip(t *testing.T) {
	s := newAdapterTestStore(t)
	defer s.Close() //nolint:errcheck // test cleanup

	var ks builtin.KnowledgeStore = NewToolStore(s)
	_ = ks // compile-time satisfaction check (assertion below covers the runtime form)
	if _, ok := any(NewToolStore(s)).(builtin.KnowledgeStore); !ok {
		t.Fatal("NewToolStore does not satisfy builtin.KnowledgeStore")
	}

	ctx := context.Background()
	id, err := ks.Ingest(ctx, builtin.KnowledgePage{
		Title:  "Design Notes",
		Body:   "Discusses the [[Retrieval]] pipeline and a [link](knowledge:deadbeef00000000).",
		Source: "user",
	})
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	wantID := PageID("Design Notes", "user")
	if id != wantID {
		t.Errorf("Ingest id = %q, want %q", id, wantID)
	}

	// Second page so the wikilink [[Retrieval]] heals.
	if _, err := ks.Ingest(ctx, builtin.KnowledgePage{
		Title: "Retrieval", Body: "How retrieval works.", Source: "user",
	}); err != nil {
		t.Fatalf("Ingest second: %v", err)
	}

	page, err := ks.Read(ctx, id)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if page.Title != "Design Notes" || page.Body == "" {
		t.Errorf("page = %#v", page)
	}
	// The [[Retrieval]] wikilink healed to a page ID after the second ingest.
	healed := false
	for _, l := range page.Links {
		if l == PageID("Retrieval", "user") {
			healed = true
		}
	}
	if !healed {
		t.Errorf("wikilink not healed: links=%v unresolved=%v", page.Links, page.UnresolvedLinks)
	}

	hits, err := ks.Search(ctx, "retrieval", 5)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) == 0 {
		t.Fatal("Search returned no hits for a known term")
	}
	found := false
	for _, h := range hits {
		if h.PageID == PageID("Retrieval", "user") {
			found = true
		}
	}
	if !found {
		t.Errorf("hits %v missing the Retrieval page", hits)
	}

	// Read of a missing id must surface the sentinel through the adapter.
	if _, err := ks.Read(ctx, "doesnotexist0000000"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Read missing: err = %v, want ErrNotFound", err)
	}
}

func newSyncTestStore(t *testing.T) *Store {
	t.Helper()
	return newTestStore(t, 0)
}

// TestSyncDirAddModifySkip covers the watched-dir change detection:
// new files ingest, unchanged files skip, modified files re-ingest,
// and source is "dir:<relpath>".
func TestSyncDirAddModifySkip(t *testing.T) {
	s := newSyncTestStore(t)
	defer s.Close() //nolint:errcheck // test cleanup
	ctx := context.Background()
	dir := t.TempDir()

	// Absent dir is an error (misconfiguration), not a silent no-op.
	if _, err := SyncDir(ctx, s, filepath.Join(dir, "missing")); err == nil {
		t.Error("SyncDir on missing dir should error")
	}

	if err := os.WriteFile(filepath.Join(dir, "alpha.md"), []byte("alpha content one"), 0o644); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(dir, "sub")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nested, "beta.md"), []byte("beta content"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Non-markdown files are ignored entirely.
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("ignore me"), 0o644); err != nil {
		t.Fatal(err)
	}

	rep, err := SyncDir(ctx, s, dir)
	if err != nil {
		t.Fatalf("SyncDir: %v", err)
	}
	if rep.Ingested != 2 || rep.Skipped != 0 || len(rep.Errors) != 0 {
		t.Fatalf("first sync report = %+v, want ingested=2 skipped=0 errors=0", rep)
	}

	// Identities: title = relpath minus .md, source = dir:<relpath>.
	alphaID := PageID("alpha", "dir:alpha.md")
	betaID := PageID("sub/beta", "dir:sub/beta.md")
	if _, err := s.Read(ctx, alphaID); err != nil {
		t.Errorf("alpha page missing: %v", err)
	}
	if _, err := s.Read(ctx, betaID); err != nil {
		t.Errorf("sub/beta page missing: %v", err)
	}
	if c, err := s.Count(ctx); err != nil || c != 2 {
		t.Errorf("Count = %d, %v; want 2", c, err)
	}

	// Second sync: nothing changed -> everything skipped.
	rep, err = SyncDir(ctx, s, dir)
	if err != nil {
		t.Fatalf("SyncDir 2: %v", err)
	}
	if rep.Ingested != 0 || rep.Skipped != 2 {
		t.Errorf("second sync report = %+v, want ingested=0 skipped=2", rep)
	}

	// Modify alpha: re-ingested, beta still skipped.
	if err := os.WriteFile(filepath.Join(dir, "alpha.md"), []byte("alpha content TWO"), 0o644); err != nil {
		t.Fatal(err)
	}
	rep, err = SyncDir(ctx, s, dir)
	if err != nil {
		t.Fatalf("SyncDir 3: %v", err)
	}
	if rep.Ingested != 1 || rep.Skipped != 1 {
		t.Errorf("third sync report = %+v, want ingested=1 skipped=1", rep)
	}
	page, err := s.Read(ctx, alphaID)
	if err != nil {
		t.Fatalf("read alpha after modify: %v", err)
	}
	if page.Body != "alpha content TWO" {
		t.Errorf("alpha body = %q, want the modified content", page.Body)
	}

	// FTS over dir-ingested pages works via the normal search path.
	hits, err := s.Search(ctx, "TWO", 5)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) == 0 {
		t.Error("search found no hit for the modified alpha content")
	}
}

// TestSyncDirUnreadableFile verifies per-file failures are reported in the
// report and never abort the pass.
func TestSyncDirUnreadableFile(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can read anything; permission-based test invalid")
	}
	s := newSyncTestStore(t)
	defer s.Close() //nolint:errcheck // test cleanup
	ctx := context.Background()
	dir := t.TempDir()

	if err := os.WriteFile(filepath.Join(dir, "good.md"), []byte("good"), 0o644); err != nil {
		t.Fatal(err)
	}
	bad := filepath.Join(dir, "bad.md")
	if err := os.WriteFile(bad, []byte("secret"), 0o000); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(bad, 0o644) //nolint:errcheck // test cleanup

	rep, err := SyncDir(ctx, s, dir)
	if err != nil {
		t.Fatalf("SyncDir: %v", err)
	}
	if len(rep.Errors) != 1 {
		t.Errorf("errors = %v, want exactly one", rep.Errors)
	}
	if rep.Ingested != 1 {
		t.Errorf("ingested = %d, want 1 (the readable file still lands)", rep.Ingested)
	}
}

// TestSyncDirNilGuards pins the argument validation.
func TestSyncDirNilGuards(t *testing.T) {
	if _, err := SyncDir(context.Background(), nil, t.TempDir()); err == nil {
		t.Error("nil store should error")
	}
	s := newSyncTestStore(t)
	defer s.Close() //nolint:errcheck // test cleanup
	if _, err := SyncDir(context.Background(), s, ""); err == nil {
		t.Error("empty dir should error")
	}
	// A file (not a directory) is an error.
	f := filepath.Join(t.TempDir(), "x.md")
	if err := os.WriteFile(f, []byte("y"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := SyncDir(context.Background(), s, f); err == nil {
		t.Error("file path should error")
	}
}

// TestSyncDirContentHashDiffIsByteExact: whitespace-only differences count
// as changes (byte comparison, not normalized).
func TestSyncDirContentHashDiffIsByteExact(t *testing.T) {
	s := newSyncTestStore(t)
	defer s.Close() //nolint:errcheck // test cleanup
	ctx := context.Background()
	dir := t.TempDir()
	p := filepath.Join(dir, "w.md")
	if err := os.WriteFile(p, []byte("v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := SyncDir(ctx, s, dir); err != nil {
		t.Fatal(err)
	}
	// Trailing newline added: byte-different, must re-ingest.
	if err := os.WriteFile(p, []byte("v1\n\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rep, err := SyncDir(ctx, s, dir)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Ingested != 1 || rep.Skipped != 0 {
		t.Errorf("report = %+v, want ingested=1 (byte-exact diff)", rep)
	}
}
