package knowledge

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

// newTestStore opens a fresh store on a temp dir with a small page cap.
func newTestStore(t *testing.T, maxPages int) *Store {
	t.Helper()
	s, err := New(t.TempDir(), maxPages, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Fatalf("close store: %v", err)
		}
	})
	return s
}

func TestRoundtrip(t *testing.T) {
	s := newTestStore(t, 0)
	ctx := context.Background()

	id, err := s.Ingest(ctx, Page{
		Title:  "Deploy runbook",
		Body:   "Restart the fleet, then verify health endpoints.",
		Source: "ops",
	})
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if id == "" {
		t.Fatal("Ingest returned empty id")
	}

	got, err := s.Read(ctx, id)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got.Title != "Deploy runbook" {
		t.Errorf("title = %q, want %q", got.Title, "Deploy runbook")
	}
	if got.Source != "ops" {
		t.Errorf("source = %q, want %q", got.Source, "ops")
	}
	if got.IngestedAt.IsZero() {
		t.Error("ingested_at not populated")
	}

	// Deterministic ID: re-derive and compare.
	if id != PageID("Deploy runbook", "ops") {
		t.Errorf("id %q != PageID derive %q", id, PageID("Deploy runbook", "ops"))
	}
}

func TestReadMissing(t *testing.T) {
	s := newTestStore(t, 0)
	if _, err := s.Read(context.Background(), "nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestUpsertReplaces(t *testing.T) {
	s := newTestStore(t, 0)
	ctx := context.Background()

	id1, err := s.Ingest(ctx, Page{Title: "Same", Body: "old body", Source: "src"})
	if err != nil {
		t.Fatalf("Ingest 1: %v", err)
	}
	ingestedAt := time.Now().UTC().Add(-time.Hour)
	id2, err := s.Ingest(ctx, Page{Title: "Same", Body: "new body", Source: "src", IngestedAt: ingestedAt})
	if err != nil {
		t.Fatalf("Ingest 2: %v", err)
	}
	if id1 != id2 {
		t.Fatalf("ids differ: %q vs %q", id1, id2)
	}

	count, err := s.Count(ctx)
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if count != 1 {
		t.Fatalf("count = %d, want 1 (upsert, not insert)", count)
	}

	got, err := s.Read(ctx, id1)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got.Body != "new body" {
		t.Errorf("body = %q, want replaced %q", got.Body, "new body")
	}
	if !got.IngestedAt.Equal(ingestedAt.Truncate(time.Second)) &&
		got.IngestedAt.Sub(ingestedAt) > time.Second {
		t.Errorf("ingested_at = %v, want ~%v (replaced)", got.IngestedAt, ingestedAt)
	}
}

// TestLinkResolutionBothDirections ingests A linking to B, then B, then
// asserts both read back with resolved links in each direction.
func TestLinkResolutionBothDirections(t *testing.T) {
	s := newTestStore(t, 0)
	ctx := context.Background()

	idA, err := s.Ingest(ctx, Page{
		Title: "Page A",
		Body:  "See [B](knowledge:" + PageID("Page B", "") + ") and [[Page B]] too.",
	})
	if err != nil {
		t.Fatalf("Ingest A: %v", err)
	}
	idB, err := s.Ingest(ctx, Page{Title: "Page B", Body: "Back to [[Page A]]."})
	if err != nil {
		t.Fatalf("Ingest B: %v", err)
	}

	a, err := s.Read(ctx, idA)
	if err != nil {
		t.Fatalf("Read A: %v", err)
	}
	foundB := false
	for _, l := range a.Links {
		if l == idB {
			foundB = true
		}
	}
	if !foundB {
		t.Errorf("A.Links = %v, want to contain B id %q", a.Links, idB)
	}

	b, err := s.Read(ctx, idB)
	if err != nil {
		t.Fatalf("Read B: %v", err)
	}
	foundA := false
	for _, l := range b.Links {
		if l == idA {
			foundA = true
		}
	}
	if !foundA {
		t.Errorf("B.Links = %v, want to contain A id %q", b.Links, idA)
	}
}

// TestWikilinkHealsOnLateIngest: a wikilink stored unresolved resolves once
// the target page exists (read-time resolution).
func TestWikilinkHealsOnLateIngest(t *testing.T) {
	s := newTestStore(t, 0)
	ctx := context.Background()

	idFrom, err := s.Ingest(ctx, Page{Title: "From", Body: "points at [[Later Page]]"})
	if err != nil {
		t.Fatalf("Ingest from: %v", err)
	}

	from, err := s.Read(ctx, idFrom)
	if err != nil {
		t.Fatalf("Read from: %v", err)
	}
	if len(from.Links) != 0 {
		t.Errorf("links before target exists = %v, want empty", from.Links)
	}
	if len(from.UnresolvedLinks) != 1 || from.UnresolvedLinks[0] != "Later Page" {
		t.Errorf("unresolved = %v, want [Later Page]", from.UnresolvedLinks)
	}

	idLater, err := s.Ingest(ctx, Page{Title: "Later Page", Body: "exists now"})
	if err != nil {
		t.Fatalf("Ingest later: %v", err)
	}

	from, err = s.Read(ctx, idFrom)
	if err != nil {
		t.Fatalf("Re-read from: %v", err)
	}
	if len(from.Links) != 1 || from.Links[0] != idLater {
		t.Errorf("links after heal = %v, want [%s]", from.Links, idLater)
	}
	if len(from.UnresolvedLinks) != 0 {
		t.Errorf("unresolved after heal = %v, want empty", from.UnresolvedLinks)
	}
}

func TestDanglingKnowledgeURILinkTolerated(t *testing.T) {
	s := newTestStore(t, 0)
	ctx := context.Background()

	// Link to a page ID that never exists: ingest and read must not error.
	id, err := s.Ingest(ctx, Page{Title: "Dangler", Body: "[missing](knowledge:deadbeefdeadbeef)"})
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	p, err := s.Read(ctx, id)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	// The dangling target is stored but not resolvable — it must not appear
	// in Links (points at a nonexistent page) nor crash the read.
	for _, l := range p.Links {
		if l == "deadbeefdeadbeef" {
			t.Errorf("dangling id leaked into Links: %v", p.Links)
		}
	}
}

func TestEviction(t *testing.T) {
	const maxPages = 3
	s := newTestStore(t, maxPages)
	ctx := context.Background()

	base := time.Now().UTC().Add(-10 * time.Hour)
	for i := 0; i < 5; i++ {
		_, err := s.Ingest(ctx, Page{
			Title:      fmt.Sprintf("Page %d", i),
			Body:       fmt.Sprintf("body %d", i),
			IngestedAt: base.Add(time.Duration(i) * time.Hour),
		})
		if err != nil {
			t.Fatalf("Ingest %d: %v", i, err)
		}
	}

	count, err := s.Count(ctx)
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if count != maxPages {
		t.Fatalf("count = %d, want %d after eviction", count, maxPages)
	}

	// Oldest two must be gone; newest three present.
	for _, tc := range []struct {
		title string
		want  bool
	}{
		{"Page 0", false}, {"Page 1", false},
		{"Page 2", true}, {"Page 3", true}, {"Page 4", true},
	} {
		_, err := s.Read(ctx, PageID(tc.title, ""))
		got := !errors.Is(err, ErrNotFound)
		if got != tc.want {
			t.Errorf("page %q present = %v, want %v", tc.title, got, tc.want)
		}
	}
}

func TestSearchRanking(t *testing.T) {
	s := newTestStore(t, 0)
	ctx := context.Background()

	pages := []Page{
		{Title: "Kubernetes overview", Body: "kubernetes is a container orchestrator"},
		{Title: "Kubernetes deep dive", Body: "kubernetes scheduling, kubernetes networking, kubernetes storage and kubernetes security in depth"},
		{Title: "Gardening notes", Body: "tomatoes need sun and water"},
	}
	for i := range pages {
		if _, err := s.Ingest(ctx, pages[i]); err != nil {
			t.Fatalf("Ingest %d: %v", i, err)
		}
	}

	hits, err := s.Search(ctx, "kubernetes", 5)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) < 2 {
		t.Fatalf("hits = %d, want >= 2", len(hits))
	}
	// More occurrences → higher BM25 → ranked first.
	if hits[0].Title != "Kubernetes deep dive" {
		t.Errorf("top hit = %q, want %q (full hits[0]=%+v)", hits[0].Title, "Kubernetes deep dive", hits[0])
	}
	if hits[0].Score <= 0 {
		t.Errorf("score = %v, want > 0", hits[0].Score)
	}
	if hits[0].Snippet == "" {
		t.Error("snippet empty")
	}

	gardening, err := s.Search(ctx, "tomatoes", 5)
	if err != nil {
		t.Fatalf("Search tomatoes: %v", err)
	}
	if len(gardening) != 1 || gardening[0].Title != "Gardening notes" {
		t.Fatalf("tomatoes hits = %+v, want only Gardening notes", gardening)
	}
}

func TestSearchLimit(t *testing.T) {
	s := newTestStore(t, 0)
	ctx := context.Background()
	for i := 0; i < 8; i++ {
		if _, err := s.Ingest(ctx, Page{Title: fmt.Sprintf("Widget %d", i), Body: "widget widget widget"}); err != nil {
			t.Fatalf("Ingest %d: %v", i, err)
		}
	}
	hits, err := s.Search(ctx, "widget", 3)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) != 3 {
		t.Fatalf("hits = %d, want 3", len(hits))
	}
}

func TestSearchNoFTS5Fallback(t *testing.T) {
	// Even if FTS5 were unavailable, Search must not error — it returns no
	// hits. We can't easily remove FTS5 from modernc sqlite, so this asserts
	// the contract on an empty store: zero hits, nil error.
	s := newTestStore(t, 0)
	hits, err := s.Search(context.Background(), "anything", 5)
	if err != nil {
		t.Fatalf("Search on empty store: %v", err)
	}
	if len(hits) != 0 {
		t.Fatalf("hits = %d, want 0", len(hits))
	}
	if !s.HasFTS5(context.Background()) {
		t.Log("note: FTS5 unavailable in this build; search degraded gracefully")
	}
}

func TestExtractLinks(t *testing.T) {
	body := "see [x](knowledge:abc123) and [[Some Title]] plus [[Some Title]] again and [not this](https://example.com)"
	toIDs, titles := extractLinks(body)
	if len(toIDs) != 1 || toIDs[0] != "abc123" {
		t.Errorf("toIDs = %v, want [abc123]", toIDs)
	}
	if len(titles) != 1 || titles[0] != "Some Title" {
		t.Errorf("titles = %v, want [Some Title] (deduped)", titles)
	}
}

func TestCloseIdempotent(t *testing.T) {
	s, err := New(t.TempDir(), 0, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}
