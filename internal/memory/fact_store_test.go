package memory

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newTestFactStore(t *testing.T) *FactStore {
	t.Helper()
	s, err := NewFactStore(filepath.Join(t.TempDir(), "facts.db"))
	if err != nil {
		t.Fatalf("NewFactStore: %v", err)
	}
	t.Cleanup(func() { s.Close() }) //nolint:errcheck // cleanup
	return s
}

func fixedTime(y, m, d int) time.Time {
	return time.Date(y, time.Month(m), d, 12, 0, 0, 0, time.UTC)
}

func TestFactStore_UpsertConflictClosesPrevious(t *testing.T) {
	s := newTestFactStore(t)
	ctx := context.Background()

	first := MemoryFact{OwnerID: "", Kind: FactPreference, Key: "seat", Value: "aisle", UpdatedAt: fixedTime(2025, 6, 1)}
	if err := s.Upsert(ctx, first); err != nil {
		t.Fatalf("upsert 1: %v", err)
	}
	second := MemoryFact{OwnerID: "", Kind: FactPreference, Key: "seat", Value: "window", UpdatedAt: fixedTime(2025, 6, 2)}
	if err := s.Upsert(ctx, second); err != nil {
		t.Fatalf("upsert 2: %v", err)
	}

	active, err := s.GetActive(ctx, "", fixedTime(2025, 6, 3))
	if err != nil {
		t.Fatalf("GetActive: %v", err)
	}
	if len(active) != 1 {
		t.Fatalf("active count = %d, want 1 (facts: %+v)", len(active), active)
	}
	if active[0].Value != "window" {
		t.Fatalf("value = %q, want window (last-write-wins)", active[0].Value)
	}
	if active[0].ValidUntil != nil {
		t.Fatalf("open row must have nil ValidUntil, got %v", *active[0].ValidUntil)
	}
}

func TestFactStore_TemporalWindows(t *testing.T) {
	s := newTestFactStore(t)
	ctx := context.Background()
	at := fixedTime(2025, 6, 15)

	from := fixedTime(2025, 6, 10)
	until := fixedTime(2025, 6, 20)
	f := MemoryFact{OwnerID: "", Kind: FactTemporal, Key: "trip", Value: "Tokyo", ValidFrom: &from, ValidUntil: &until, UpdatedAt: at}
	if err := s.Upsert(ctx, f); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	// Inside the window.
	got, err := s.GetActive(ctx, "", fixedTime(2025, 6, 15))
	if err != nil || len(got) != 1 {
		t.Fatalf("mid-window: n=%d err=%v, want 1", len(got), err)
	}
	// Before ValidFrom: excluded.
	got, err = s.GetActive(ctx, "", fixedTime(2025, 6, 9))
	if err != nil || len(got) != 0 {
		t.Fatalf("before-window: n=%d err=%v, want 0", len(got), err)
	}
	// At ValidFrom boundary: INCLUDED (valid_from <= at).
	got, err = s.GetActive(ctx, "", from)
	if err != nil || len(got) != 1 {
		t.Fatalf("at-valid-from: n=%d err=%v, want 1", len(got), err)
	}
	// At ValidUntil boundary: EXCLUDED (valid_until > at is required).
	got, err = s.GetActive(ctx, "", until)
	if err != nil || len(got) != 0 {
		t.Fatalf("at-valid-until: n=%d err=%v, want 0 (boundary excluded)", len(got), err)
	}
	// After window: excluded.
	got, err = s.GetActive(ctx, "", fixedTime(2025, 6, 21))
	if err != nil || len(got) != 0 {
		t.Fatalf("after-window: n=%d err=%v, want 0", len(got), err)
	}
}

func TestFactStore_MultiuserOffEmptyOwner(t *testing.T) {
	s := newTestFactStore(t)
	ctx := context.Background()

	if err := s.Upsert(ctx, MemoryFact{OwnerID: "", Kind: FactAccount, Key: "united", Value: "12345678", UpdatedAt: fixedTime(2025, 1, 1)}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	// A fact owned by another owner must NOT leak into the empty-owner view.
	if err := s.Upsert(ctx, MemoryFact{OwnerID: "alice", Kind: FactAccount, Key: "delta", Value: "999", UpdatedAt: fixedTime(2025, 1, 1)}); err != nil {
		t.Fatalf("upsert alice: %v", err)
	}

	got, err := s.GetActive(ctx, "", fixedTime(2025, 1, 2))
	if err != nil {
		t.Fatalf("GetActive: %v", err)
	}
	if len(got) != 1 || got[0].Key != "united" {
		t.Fatalf("empty-owner view = %+v, want only the empty-owner fact", got)
	}
}

func TestFactStore_EmptyStoreReturnsEmptyList(t *testing.T) {
	s := newTestFactStore(t)
	got, err := s.GetActive(context.Background(), "", time.Now())
	if err != nil {
		t.Fatalf("GetActive: %v", err)
	}
	if got == nil || len(got) != 0 {
		t.Fatalf("empty store: got %#v, want empty non-nil slice", got)
	}
}

func TestFactStore_SearchFilter(t *testing.T) {
	s := newTestFactStore(t)
	ctx := context.Background()
	t0 := fixedTime(2025, 1, 1)
	for _, f := range []MemoryFact{
		{OwnerID: "", Kind: FactPreference, Key: "seat", Value: "window seat", UpdatedAt: t0},
		{OwnerID: "", Kind: FactRestriction, Key: "dietary", Value: "vegetarian meals", UpdatedAt: t0},
		{OwnerID: "", Kind: FactAccount, Key: "united mileageplus", Value: "12345678", UpdatedAt: t0},
	} {
		if err := s.Upsert(ctx, f); err != nil {
			t.Fatalf("upsert: %v", err)
		}
	}

	// Substring on value.
	got, err := s.Search(ctx, "", "window", "")
	if err != nil || len(got) != 1 || got[0].Key != "seat" {
		t.Fatalf("search window: %+v err=%v", got, err)
	}
	// Substring on key, case-insensitive.
	got, err = s.Search(ctx, "", "MILEAGE", "")
	if err != nil || len(got) != 1 {
		t.Fatalf("search MILEAGE: %+v err=%v", got, err)
	}
	// Kind filter alone.
	got, err = s.Search(ctx, "", "", string(FactRestriction))
	if err != nil || len(got) != 1 || got[0].Kind != FactRestriction {
		t.Fatalf("kind filter: %+v err=%v", got, err)
	}
	// Kind + query mismatch -> empty, not error.
	got, err = s.Search(ctx, "", "window", string(FactAccount))
	if err != nil || len(got) != 0 {
		t.Fatalf("kind+query mismatch: %+v err=%v", got, err)
	}
	// Empty query + empty kind matches everything.
	got, err = s.Search(ctx, "", "", "")
	if err != nil || len(got) != 3 {
		t.Fatalf("match-all: %+v err=%v", got, err)
	}
}

func TestFactStore_RequiresKindAndKey(t *testing.T) {
	s := newTestFactStore(t)
	if err := s.Upsert(context.Background(), MemoryFact{OwnerID: "", Value: "x"}); err == nil {
		t.Fatal("missing kind/key must error, got nil")
	}
}

func TestExtractFactsFromMessages(t *testing.T) {
	msgs := []string{
		"I prefer window seats on long flights.",
		"I'm vegetarian, so I'll need a special meal.",
		"I am allergic to peanuts.",
		"My United MileagePlus number is 12345678.",
		"Next Friday I will fly to Tokyo.",
		"What time is it?",               // no fact
		"The search returned 3 options.", // transient, no fact
	}
	got := ExtractFactsFromMessages(msgs)

	kinds := map[FactKind]int{}
	for _, f := range got {
		kinds[f.Kind]++
		if f.Key == "" || f.Value == "" {
			t.Errorf("fact %+v has empty key or value", f)
		}
	}
	if kinds[FactPreference] != 1 {
		t.Errorf("preference count = %d, want 1", kinds[FactPreference])
	}
	if kinds[FactRestriction] != 2 {
		t.Errorf("restriction count = %d, want 2 (vegetarian + allergy)", kinds[FactRestriction])
	}
	if kinds[FactAccount] != 1 {
		t.Errorf("account count = %d, want 1 (facts: %+v)", kinds[FactAccount], got)
	}
	if kinds[FactTemporal] != 1 {
		t.Errorf("temporal count = %d, want 1", kinds[FactTemporal])
	}
	for _, f := range got {
		if f.Kind == FactAccount && f.Value != "12345678" {
			t.Errorf("account value = %q, want 12345678", f.Value)
		}
		if strings.TrimSpace(string(f.Kind)) == "" {
			t.Errorf("empty kind on %+v", f)
		}
	}
}

func TestStampFacts(t *testing.T) {
	facts := ExtractFactsFromMessages([]string{"I prefer aisle seats."})
	StampFacts(facts, "alice", "session-1", fixedTime(2025, 6, 15))
	if len(facts) != 1 {
		t.Fatalf("n=%d, want 1", len(facts))
	}
	if facts[0].OwnerID != "alice" || facts[0].SourceSession != "session-1" || facts[0].UpdatedAt.IsZero() {
		t.Fatalf("stamp failed: %+v", facts[0])
	}
}
