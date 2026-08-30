package selfimprove

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// wikiFixtureTime is a deterministic fixed-noon-UTC timestamp for fixtures.
var wikiFixtureTime = time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)

func newTestWikiStore(t *testing.T) (*WikiStore, string) {
	t.Helper()
	dir := t.TempDir()
	return NewWikiStore(dir, slogDiscardLogger()), dir
}

func mustUpsert(t *testing.T, ws *WikiStore, p *LearnedPattern) string {
	t.Helper()
	slug, err := ws.UpsertPattern(p)
	if err != nil {
		t.Fatalf("UpsertPattern: %v", err)
	}
	return slug
}

// Task 1: UpsertPattern + slug stability.

func TestUpsertPattern_StableSlugOverwrite(t *testing.T) {
	dir := t.TempDir()
	ws := NewWikiStore(dir, slogDiscardLogger())
	p := &LearnedPattern{
		ID: "pat-1", Type: PatternTypeStrategy, Status: PatternStatusActive,
		Domain: "code", Description: "prefer table-driven tests",
		Pattern: "write table tests", Confidence: 0.8, UseCount: 5,
		CreatedAt:   wikiFixtureTime,
		UpdatedAt:   wikiFixtureTime,
		ContentHash: "abc123def456abc123def456",
	}
	p1, err := ws.UpsertPattern(p)
	if err != nil {
		t.Fatalf("upsert1: %v", err)
	}
	p.UseCount = 6
	p2, err := ws.UpsertPattern(p)
	if err != nil {
		t.Fatalf("upsert2: %v", err)
	}
	if p1 != p2 {
		t.Fatalf("slug unstable: %s vs %s", p1, p2)
	}
	if !strings.Contains(filepath.Base(p2), "code-") {
		t.Fatalf("slug missing domain: %s", p2)
	}
}

func TestUpsertPattern_EmptyDomainSlugIsGeneral(t *testing.T) {
	ws, _ := newTestWikiStore(t)
	slug := mustUpsert(t, ws, &LearnedPattern{
		ID: "pat-2", Pattern: "measure twice",
		ContentHash: "0123456789abcdef",
		CreatedAt:   wikiFixtureTime, UpdatedAt: wikiFixtureTime,
	})
	if !strings.HasPrefix(filepath.Base(slug), "general-0123456789ab") {
		t.Fatalf("empty domain slug = %q, want general-0123456789ab prefix", slug)
	}
}

func TestUpsertPattern_FrozenPageFormat(t *testing.T) {
	ws, dir := newTestWikiStore(t)
	mustUpsert(t, ws, &LearnedPattern{
		ID: "pat-1", Type: PatternTypeStrategy, Status: PatternStatusActive,
		Domain: "code", Description: "prefer table-driven tests",
		Pattern: "write table tests", Confidence: 0.8, UseCount: 5,
		Examples:    []string{"unit test named cases", "t.Run subtests"},
		CreatedAt:   wikiFixtureTime,
		UpdatedAt:   wikiFixtureTime,
		ContentHash: "abc123def456abc123def456",
	})
	data, err := os.ReadFile(filepath.Join(dir, "patterns", "code-abc123def456.md"))
	if err != nil {
		t.Fatalf("read page: %v", err)
	}
	want := "---\n" +
		"id: pat-1\n" +
		"type: strategy\n" +
		"status: active\n" +
		"confidence: 0.8\n" +
		"use_count: 5\n" +
		"created_at: 2025-06-15T12:00:00Z\n" +
		"updated_at: 2025-06-15T12:00:00Z\n" +
		"---\n" +
		"# prefer table-driven tests\n" +
		"## Pattern\n" +
		"write table tests\n" +
		"## Examples\n" +
		"- unit test named cases\n" +
		"- t.Run subtests\n"
	if string(data) != want {
		t.Fatalf("page format mismatch\n--- got ---\n%s\n--- want ---\n%s", data, want)
	}
}

// Task 2: RebuildIndex + AppendLog + AppendSkillImpact/ReadSkillImpact.

func TestRebuildIndex_ListsPatternsWithLinks(t *testing.T) {
	ws, dir := newTestWikiStore(t)
	mustUpsert(t, ws, &LearnedPattern{
		ID: "pat-1", Domain: "code", Description: "prefer table-driven tests",
		Pattern: "write table tests", Confidence: 0.8,
		ContentHash: "abc123def456abc123def456",
		CreatedAt:   wikiFixtureTime, UpdatedAt: wikiFixtureTime,
	})
	mustUpsert(t, ws, &LearnedPattern{
		ID: "pat-9", Domain: "debug", Description: "grep before read",
		Pattern: "grep first", Confidence: 0.9,
		ContentHash: "cafebabecafebabecafebabe",
		CreatedAt:   wikiFixtureTime, UpdatedAt: wikiFixtureTime,
	})
	if err := ws.RebuildIndex(); err != nil {
		t.Fatalf("RebuildIndex: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "index.md"))
	if err != nil {
		t.Fatalf("read index: %v", err)
	}
	idx := string(data)
	for _, want := range []string{
		"- [code-abc123def456](patterns/code-abc123def456.md): prefer table-driven tests",
		"- [debug-cafebabecafe](patterns/debug-cafebabecafe.md): grep before read",
	} {
		if !strings.Contains(idx, want) {
			t.Fatalf("index missing %q\nindex:\n%s", want, idx)
		}
	}
}

func TestRebuildIndex_EmptyWiki(t *testing.T) {
	ws, dir := newTestWikiStore(t)
	if err := ws.RebuildIndex(); err != nil {
		t.Fatalf("RebuildIndex on empty wiki: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "index.md"))
	if err != nil {
		t.Fatalf("read index: %v", err)
	}
	if strings.TrimSpace(string(data)) != "" {
		t.Fatalf("empty wiki index should be empty, got %q", data)
	}
}

func TestAppendLog_FormatAndAppendOnly(t *testing.T) {
	ws, dir := newTestWikiStore(t)
	if err := ws.AppendLog("consolidation ran"); err != nil {
		t.Fatalf("append 1: %v", err)
	}
	if err := ws.AppendLog("second entry"); err != nil {
		t.Fatalf("append 2: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "logs.md"))
	if err != nil {
		t.Fatalf("read logs: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("want 2 log lines, got %d: %q", len(lines), data)
	}
	ts, entry, ok := strings.Cut(lines[0], " ")
	if !ok {
		t.Fatalf("log line missing timestamp separator: %q", lines[0])
	}
	if _, err := time.Parse(time.RFC3339, ts); err != nil {
		t.Fatalf("log timestamp not RFC3339: %v (%q)", err, ts)
	}
	if entry != "consolidation ran" {
		t.Fatalf("first entry mutated: %q", entry)
	}
	if lines[1] != ts+" second entry" {
		t.Fatalf("second entry wrong: %q", lines[1])
	}
}

func TestAppendSkillImpact_JSONLAndRead(t *testing.T) {
	dir := t.TempDir()
	ws := NewWikiStore(dir, slogDiscardLogger())
	e := SkillImpactEntry{
		Time:   wikiFixtureTime,
		Action: "refine", SkillName: "reviewer", Score: 0.42, Accepted: false,
		Reason: "dims below floor",
	}
	if err := ws.AppendSkillImpact(e); err != nil {
		t.Fatal(err)
	}
	if err := ws.AppendSkillImpact(e); err != nil {
		t.Fatal(err)
	} // two rows
	got, err := ws.ReadSkillImpact()
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(got), "\n")
	if len(lines) != 2 {
		t.Fatalf("want 2 rows, got %d: %q", len(lines), got)
	}
	var back SkillImpactEntry
	if err := json.Unmarshal([]byte(lines[0]), &back); err != nil {
		t.Fatalf("row not JSONL: %v", err)
	}
	if back.Accepted {
		t.Fatal("accepted flag lost")
	}
	if back.SkillName != "reviewer" || back.Action != "refine" || back.Score != 0.42 {
		t.Fatalf("entry fields lost: %+v", back)
	}
}

func TestReadSkillImpact_MissingFileIsEmpty(t *testing.T) {
	ws, _ := newTestWikiStore(t)
	got, err := ws.ReadSkillImpact()
	if err != nil {
		t.Fatalf("missing ledger should not error: %v", err)
	}
	if got != "" {
		t.Fatalf("missing ledger should be empty, got %q", got)
	}
}

// Task 3: LoadPatterns round-trip + Initialize repopulation.

func TestLoadPatterns_RoundTrip(t *testing.T) {
	ws, _ := newTestWikiStore(t)
	mustUpsert(t, ws, &LearnedPattern{
		ID: "pat-9", Type: PatternTypeTactic, Status: PatternStatusActive,
		Domain: "debug", Description: "grep before read",
		Pattern: "grep first", Confidence: 0.9, UseCount: 7,
		Examples:    []string{"rg TODO", "git log -S"},
		ContentHash: "cafebabecafebabecafebabe",
		CreatedAt:   wikiFixtureTime, UpdatedAt: wikiFixtureTime,
	})
	got, err := ws.LoadPatterns()
	if err != nil {
		t.Fatalf("LoadPatterns: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 pattern, got %d", len(got))
	}
	p := got[0]
	if p.ID != "pat-9" {
		t.Errorf("ID = %q, want pat-9", p.ID)
	}
	if p.Type != PatternTypeTactic {
		t.Errorf("Type = %q, want tactic", p.Type)
	}
	if p.Status != PatternStatusActive {
		t.Errorf("Status = %q, want active", p.Status)
	}
	if p.Confidence != 0.9 {
		t.Errorf("Confidence = %v, want 0.9", p.Confidence)
	}
	if p.UseCount != 7 {
		t.Errorf("UseCount = %d, want 7", p.UseCount)
	}
	if !p.CreatedAt.Equal(wikiFixtureTime) || !p.UpdatedAt.Equal(wikiFixtureTime) {
		t.Errorf("times not round-tripped: created=%v updated=%v", p.CreatedAt, p.UpdatedAt)
	}
	if p.Description != "grep before read" {
		t.Errorf("Description = %q", p.Description)
	}
	if p.Pattern != "grep first" {
		t.Errorf("Pattern = %q", p.Pattern)
	}
	if len(p.Examples) != 2 || p.Examples[0] != "rg TODO" || p.Examples[1] != "git log -S" {
		t.Errorf("Examples = %v", p.Examples)
	}
}

func TestLoadPatterns_EmptyWiki(t *testing.T) {
	ws, _ := newTestWikiStore(t)
	got, err := ws.LoadPatterns()
	if err != nil {
		t.Fatalf("LoadPatterns on empty wiki: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("want 0 patterns, got %d", len(got))
	}
}

func TestLoadPatterns_SkipsCorruptPages(t *testing.T) {
	ws, dir := newTestWikiStore(t)
	mustUpsert(t, ws, &LearnedPattern{
		ID: "pat-ok", Domain: "code", Description: "good page",
		Pattern: "be good", Confidence: 0.5,
		ContentHash: "aaaa0000aaaa0000aaaa0000",
		CreatedAt:   wikiFixtureTime, UpdatedAt: wikiFixtureTime,
	})
	corrupt := filepath.Join(dir, "patterns", "code-badbadbadbad.md")
	if err := os.WriteFile(corrupt, []byte("not a wiki page at all"), 0o600); err != nil {
		t.Fatalf("write corrupt page: %v", err)
	}
	got, err := ws.LoadPatterns()
	if err != nil {
		t.Fatalf("LoadPatterns: %v", err)
	}
	if len(got) != 1 || got[0].ID != "pat-ok" {
		t.Fatalf("want only the good page, got %+v", got)
	}
}

func TestLoadPatterns_RegeneratesMissingID(t *testing.T) {
	ws, dir := newTestWikiStore(t)
	page := filepath.Join(dir, "patterns")
	if err := os.MkdirAll(page, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	content := "---\n" +
		"type: tactic\n" +
		"status: pending\n" +
		"confidence: 0.5\n" +
		"use_count: 1\n" +
		"created_at: 2025-06-15T12:00:00Z\n" +
		"updated_at: 2025-06-15T12:00:00Z\n" +
		"---\n" +
		"# hand written page\n" +
		"## Pattern\n" +
		"check logs first\n" +
		"## Examples\n"
	if err := os.WriteFile(filepath.Join(page, "debug-beef0000beef.md"), []byte(content), 0o600); err != nil {
		t.Fatalf("write page: %v", err)
	}
	got, err := ws.LoadPatterns()
	if err != nil {
		t.Fatalf("LoadPatterns: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 pattern, got %d", len(got))
	}
	if got[0].ID == "" || !strings.HasPrefix(got[0].ID, "pat-") {
		t.Fatalf("missing ID not regenerated: %q", got[0].ID)
	}
	if got[0].Type != PatternTypeTactic || got[0].Pattern != "check logs first" {
		t.Fatalf("page fields not parsed: %+v", got[0])
	}
}

func TestInitialize_ReloadsPatternsFromWiki(t *testing.T) {
	dir := t.TempDir()
	ws := NewWikiStore(dir, slogDiscardLogger())
	p := &LearnedPattern{
		ID: "pat-9", Domain: "debug", Description: "grep before read",
		Pattern: "grep first", Confidence: 0.9, UseCount: 7,
		Status:      PatternStatusActive, // Retrieve() only returns active patterns
		ContentHash: "cafebabecafebabecafebabe",
		CreatedAt:   wikiFixtureTime, UpdatedAt: wikiFixtureTime,
	}
	if _, err := ws.UpsertPattern(p); err != nil {
		t.Fatal(err)
	}
	if err := ws.RebuildIndex(); err != nil {
		t.Fatal(err)
	}

	lp := NewLearningPipeline(DefaultLearningConfig(), nil, t.TempDir(), slogDiscardLogger())
	if err := lp.Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}
	lp.SetWikiStore(ws)
	// Second Initialize path: simulate restart by a fresh pipeline with the
	// same wiki dir.
	lp2 := NewLearningPipeline(DefaultLearningConfig(), nil, t.TempDir(), slogDiscardLogger())
	lp2.SetWikiStore(ws)
	if err := lp2.Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}
	got, err := lp2.Retrieve(context.Background(), "grep before read", "", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) == 0 {
		t.Fatal("pattern not reloaded from wiki after restart")
	}
}

func TestInitialize_NilWikiDoesNotClobber(t *testing.T) {
	// A pipeline initialized BEFORE SetWikiStore must not retro-load wiki
	// state (Initialize is idempotent once initialized).
	ws, _ := newTestWikiStore(t)
	mustUpsert(t, ws, &LearnedPattern{
		ID: "pat-9", Domain: "debug", Description: "grep before read",
		Pattern: "grep first", Confidence: 0.9,
		ContentHash: "cafebabecafebabecafebabe",
		CreatedAt:   wikiFixtureTime, UpdatedAt: wikiFixtureTime,
	})
	lp := NewLearningPipeline(DefaultLearningConfig(), nil, t.TempDir(), slogDiscardLogger())
	if err := lp.Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}
	lp.SetWikiStore(ws)
	got := lp.GetPatterns()
	if len(got) != 0 {
		t.Fatalf("wiki state leaked into already-initialized pipeline: %+v", got)
	}
}

// StorePattern write-through behavior.

func TestStorePattern_WritesThroughToWiki(t *testing.T) {
	dataDir := t.TempDir()
	wikiDir := t.TempDir()
	lp := NewLearningPipeline(DefaultLearningConfig(), nil, dataDir, slogDiscardLogger())
	if err := lp.Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}
	lp.SetWikiStore(NewWikiStore(wikiDir, slogDiscardLogger()))
	p := &LearnedPattern{
		ID: "pat-wt", Type: PatternTypeHeuristic, Status: PatternStatusActive,
		Domain: "code", Description: "write-through works",
		Pattern: "store it twice", Confidence: 0.9,
		ContentHash: "feed0000feed0000feed0000",
		CreatedAt:   wikiFixtureTime, UpdatedAt: wikiFixtureTime,
	}
	if err := lp.StorePattern(context.Background(), p); err != nil {
		t.Fatalf("StorePattern: %v", err)
	}
	page := filepath.Join(wikiDir, "patterns", "code-feed0000feed.md")
	if _, err := os.Stat(page); err != nil {
		t.Fatalf("wiki page not written by StorePattern: %v", err)
	}
	// Guard for learning_test.go: patterns.json must still never be written.
	if _, err := os.Stat(filepath.Join(dataDir, "patterns.json")); !os.IsNotExist(err) {
		t.Fatalf("patterns.json must not exist; err=%v", err)
	}
}

func TestStorePattern_WikiIOErrorDoesNotFail(t *testing.T) {
	tmpDir := t.TempDir()
	// Make the wiki "directory" path a regular file so every wiki write fails.
	wikiPath := filepath.Join(tmpDir, "wikiblock")
	if err := os.WriteFile(wikiPath, []byte("x"), 0o600); err != nil {
		t.Fatalf("setup: %v", err)
	}
	lp := NewLearningPipeline(DefaultLearningConfig(), nil, tmpDir, slogDiscardLogger())
	if err := lp.Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}
	lp.SetWikiStore(NewWikiStore(wikiPath, slogDiscardLogger()))
	p := &LearnedPattern{
		ID: "pat-io", Domain: "code", Description: "survives wiki failure",
		Pattern: "keep going", Confidence: 0.9,
		ContentHash: "beef0000beef0000beef0000",
		CreatedAt:   wikiFixtureTime, UpdatedAt: wikiFixtureTime,
	}
	if err := lp.StorePattern(context.Background(), p); err != nil {
		t.Fatalf("StorePattern must never fail on wiki I/O: %v", err)
	}
	found := false
	for _, got := range lp.GetPatterns() {
		if got.ID == "pat-io" {
			found = true
		}
	}
	if !found {
		t.Fatal("pattern missing from in-memory store after wiki write failure")
	}
}

func TestStorePattern_NilWikiByteIdentical(t *testing.T) {
	// With no wiki store, StorePattern behaves exactly as before: in-memory
	// insert, no patterns.json, no wiki directory.
	tmpDir := t.TempDir()
	lp := NewLearningPipeline(DefaultLearningConfig(), nil, tmpDir, slogDiscardLogger())
	if err := lp.Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}
	p := &LearnedPattern{
		ID: "pat-nil", Domain: "code", Description: "no wiki configured",
		Pattern: "memory only", Confidence: 0.9,
		ContentHash: "c0de0000c0de0000c0de0000",
		CreatedAt:   wikiFixtureTime, UpdatedAt: wikiFixtureTime,
	}
	if err := lp.StorePattern(context.Background(), p); err != nil {
		t.Fatalf("StorePattern: %v", err)
	}
	if _, err := os.Stat(filepath.Join(tmpDir, "patterns.json")); !os.IsNotExist(err) {
		t.Fatalf("patterns.json must not exist; err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(tmpDir, "patterns")); !os.IsNotExist(err) {
		t.Fatalf("patterns dir must not be created without a wiki store; err=%v", err)
	}
}

// TestReadIndex_MissingIsEmptyAndRoundTrip covers the evolver-facing index read:
// a missing index.md is not an error; content written by RebuildIndex comes
// back verbatim.
func TestReadIndex_MissingIsEmptyAndRoundTrip(t *testing.T) {
	ws, _ := newTestWikiStore(t)
	got, err := ws.ReadIndex()
	if err != nil {
		t.Fatalf("missing index should not error: %v", err)
	}
	if got != "" {
		t.Fatalf("missing index should be empty, got %q", got)
	}

	mustUpsert(t, ws, &LearnedPattern{
		ID: "pat-idx-1", Type: PatternTypeStrategy, Status: PatternStatusActive,
		Domain: "code", Description: "index round trip", Pattern: "p",
		CreatedAt: wikiFixtureTime, UpdatedAt: wikiFixtureTime,
		ContentHash: "hashidxround",
	})
	if err := ws.RebuildIndex(); err != nil {
		t.Fatalf("RebuildIndex: %v", err)
	}
	round, err := ws.ReadIndex()
	if err != nil {
		t.Fatalf("ReadIndex after rebuild: %v", err)
	}
	if !strings.Contains(round, "- [code-hashidxround](patterns/code-hashidxround.md): index round trip") {
		t.Fatalf("index round trip lost content: %q", round)
	}
}
