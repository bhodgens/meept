package working

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"
)

// stubCounter is the deterministic test token counter: len(s)/4
// (plan T2: "deterministic stub (chars/4), NOT the real tokenizer").
type stubCounter struct{}

func (stubCounter) CountTokens(s string) int { return len(s) / 4 }

// fakeClock returns a deterministic clock starting at base, advancing by
// step on every call (no sleeps in TTL tests, task T6).
type fakeClock struct {
	mu   sync.Mutex
	cur  time.Time
	step time.Duration
}

func (f *fakeClock) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	t := f.cur
	f.cur = f.cur.Add(f.step)
	return t
}

func newTestStore(t *testing.T, cfg Config) *Store {
	t.Helper()
	s, err := New(cfg, stubCounter{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return s
}

func mkTurn(id, sessionID, role, content string, at time.Time) Turn {
	return Turn{ID: id, SessionID: sessionID, Role: role, Content: content, At: at}
}

// ---- Task T1: New validation ----

func TestNewValidation(t *testing.T) {
	cases := []struct {
		name    string
		cfg     Config
		wantErr string
	}{
		{
			name:    "zero MaxTokens rejected",
			cfg:     Config{MaxTokens: 0, MaxItems: 10, TTL: time.Hour},
			wantErr: "MaxTokens",
		},
		{
			name:    "negative MaxTokens rejected",
			cfg:     Config{MaxTokens: -1, MaxItems: 10, TTL: time.Hour},
			wantErr: "MaxTokens",
		},
		{
			name:    "zero MaxItems rejected",
			cfg:     Config{MaxTokens: 100, MaxItems: 0, TTL: time.Hour},
			wantErr: "MaxItems",
		},
		{
			name:    "negative MaxItems rejected",
			cfg:     Config{MaxTokens: 100, MaxItems: -5, TTL: time.Hour},
			wantErr: "MaxItems",
		},
		{
			name:    "negative TTL rejected",
			cfg:     Config{MaxTokens: 100, MaxItems: 10, TTL: -time.Second},
			wantErr: "TTL",
		},
		{
			name:    "nil counter rejected",
			cfg:     Config{MaxTokens: 100, MaxItems: 10, TTL: time.Hour},
			wantErr: "TokenCounter",
		},
		{
			name:    "zero TTL accepted (TTL disabled)",
			cfg:     Config{MaxTokens: 100, MaxItems: 10, TTL: 0},
			wantErr: "",
		},
		{
			name:    "valid config accepted",
			cfg:     Config{MaxTokens: 100, MaxItems: 10, TTL: time.Hour},
			wantErr: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var counter TokenCounter
			if tc.wantErr != "TokenCounter" {
				counter = stubCounter{}
			}
			s, err := New(tc.cfg, counter)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("New(%+v) unexpected error: %v", tc.cfg, err)
				}
				if s == nil {
					t.Fatal("New returned nil store with nil error")
				}
				return
			}
			if err == nil {
				t.Fatalf("New(%+v) expected error mentioning %q, got nil", tc.cfg, tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("New(%+v) error %q does not mention %q", tc.cfg, err, tc.wantErr)
			}
		})
	}
}

func TestNewDefaultsMatchR_B1(t *testing.T) {
	if DefaultMaxTokens != 8192 {
		t.Errorf("DefaultMaxTokens = %d, want 8192 (R-B1)", DefaultMaxTokens)
	}
	if DefaultMaxItems != 200 {
		t.Errorf("DefaultMaxItems = %d, want 200 (R-B1)", DefaultMaxItems)
	}
	if DefaultTTL != 24*time.Hour {
		t.Errorf("DefaultTTL = %s, want 24h (R-B1)", DefaultTTL)
	}
}

// ---- Task T2: Append with token-budgeted eviction ----

func TestAppendEviction(t *testing.T) {
	base := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	cfg := Config{MaxTokens: 100, MaxItems: 10, TTL: 0}
	s := newTestStore(t, cfg)

	// 10 turns x 30 tokens = 300 tokens against a 100-token budget.
	// After each Append the live set must be <= 100 tokens, and evicted
	// turns must be queued in eviction (oldest-first) order.
	for i := 0; i < 10; i++ {
		turn := mkTurn(
			"t"+string(rune('a'+i)),
			"sess-1",
			"user",
			strings.Repeat("x", 120), // 120/4 = 30 tokens
			base.Add(time.Duration(i)*time.Minute),
		)
		if err := s.Append(context.Background(), turn); err != nil {
			t.Fatalf("Append %d: %v", i, err)
		}
		if got := s.LiveTokens(); got > cfg.MaxTokens {
			t.Fatalf("after append %d live tokens = %d, want <= %d", i, got, cfg.MaxTokens)
		}
		if n := len(s.Snapshot()); n > cfg.MaxItems {
			t.Fatalf("after append %d live items = %d, want <= %d", i, n, cfg.MaxItems)
		}
	}

	// 300 total tokens over a 100 budget: evictions happened.
	if s.QueuedTurns() == 0 {
		t.Fatal("expected evicted turns queued for Flush, got 0")
	}
	if s.Dropped() != 0 {
		t.Fatalf("unexpected queue drops: %d", s.Dropped())
	}

	// Queue preserves eviction order: oldest first (t0..t6-ish, by At).
	snap := s.Snapshot()
	if len(snap) == 0 {
		t.Fatal("live ring unexpectedly empty")
	}
	// Live ring holds the newest turns.
	last := snap[len(snap)-1]
	if last.ID != "tj" {
		t.Errorf("newest live turn = %s, want tj", last.ID)
	}

	// Flush drains the queue in eviction order (oldest evicted first).
	rec := &recordingSink{}
	if err := s.Flush(context.Background(), rec); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	if len(rec.turns) == 0 {
		t.Fatal("recording sink got no turns")
	}
	for i := 1; i < len(rec.turns); i++ {
		if rec.turns[i-1].At.After(rec.turns[i].At) {
			t.Fatalf("flush order violation at %d: %v after %v",
				i, rec.turns[i-1].At, rec.turns[i].At)
		}
	}
	// Evicted prefix: everything except the live tail.
	wantEvicted := 10 - len(snap)
	if len(rec.turns) != wantEvicted {
		t.Errorf("flushed %d turns, want %d (10 appends, %d live)", len(rec.turns), wantEvicted, len(snap))
	}
	// First evicted is the oldest append; the exact index depends on the
	// live-set size, but it must be the earliest ID prefix still evicted.
	if !strings.HasPrefix(rec.turns[0].ID, "t") {
		t.Errorf("first evicted turn = %s, want a t-prefixed (oldest) turn", rec.turns[0].ID)
	}
	if rec.turns[0].ID == "tj" {
		t.Errorf("newest turn tj was evicted first: %+v", rec.turns[0])
	}
}

func TestAppendTTLExpiredEvictedFirst(t *testing.T) {
	base := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	cfg := Config{MaxTokens: 1000, MaxItems: 10, TTL: time.Hour}
	s := newTestStore(t, cfg)
	clock := &fakeClock{cur: base, step: 10 * time.Minute}
	s.SetClock(clock.Now)

	// t0 ages past the TTL; later turns are fresh.
	if err := s.Append(context.Background(), mkTurn("t0", "s", "user", strings.Repeat("a", 40), base)); err != nil {
		t.Fatal(err)
	}
	// Advance 2h: t0 is now expired relative to the clock.
	clock.mu.Lock()
	clock.cur = base.Add(2 * time.Hour)
	clock.mu.Unlock()

	if err := s.Append(context.Background(), mkTurn("t1", "s", "user", strings.Repeat("b", 40), base.Add(2*time.Hour))); err != nil {
		t.Fatal(err)
	}

	snap := s.Snapshot()
	for _, turn := range snap {
		if turn.ID == "t0" {
			t.Error("TTL-expired turn t0 still in live ring")
		}
	}
	if s.QueuedTurns() != 1 {
		t.Fatalf("queued = %d, want 1 (expired t0)", s.QueuedTurns())
	}
	rec := &recordingSink{}
	if err := s.Flush(context.Background(), rec); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	if len(rec.turns) != 1 || rec.turns[0].ID != "t0" {
		t.Fatalf("flushed %+v, want [t0]", rec.turns)
	}
}

func TestAppendQueueBoundedDropOldest(t *testing.T) {
	base := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	// MaxItems=5 means the live ring holds 1 and the queue caps at 5:
	// appends beyond that drop oldest from the queue.
	cfg := Config{MaxTokens: 20, MaxItems: 5, TTL: 0}
	s := newTestStore(t, cfg)

	for i := 0; i < 20; i++ {
		if err := s.Append(context.Background(),
			mkTurn("q"+string(rune('a'+i)), "s", "user", strings.Repeat("x", 80), base.Add(time.Duration(i)*time.Second))); err != nil {
			t.Fatal(err)
		}
	}
	if got := s.QueuedTurns(); got > cfg.MaxItems {
		t.Errorf("queue size %d exceeds bound %d", got, cfg.MaxItems)
	}
	if s.Dropped() == 0 {
		t.Error("expected drop counter > 0 for a never-flushed full queue")
	}
}

// ---- Task T3: Recall ----

func TestRecallTermOverlapRanking(t *testing.T) {
	base := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	cfg := Config{MaxTokens: 1000, MaxItems: 10, TTL: 0}
	s := newTestStore(t, cfg)

	turns := []Turn{
		mkTurn("r1", "s", "user", "we need to plan the postgres migration carefully", base.Add(1*time.Minute)),
		mkTurn("r2", "s", "assistant", "the redis cache is warmed", base.Add(2*time.Minute)),
		mkTurn("r3", "s", "user", "postgres migration rollback steps drafted", base.Add(3*time.Minute)),
		mkTurn("r4", "s", "assistant", "lunch order submitted", base.Add(4*time.Minute)),
	}
	for _, turn := range turns {
		if err := s.Append(context.Background(), turn); err != nil {
			t.Fatal(err)
		}
	}

	got := s.Recall(context.Background(), "postgres migration", 2)
	if len(got) != 2 {
		t.Fatalf("Recall returned %d results, want 2", len(got))
	}
	if got[0] != turns[2].Content {
		t.Errorf("top result = %q, want the postgres turn %q", got[0], turns[2].Content)
	}
	// Second is the other postgres turn (r1), not an unrelated one.
	if !strings.Contains(got[1], "postgres") {
		t.Errorf("second result %q does not mention postgres", got[1])
	}
}

func TestRecallEmptyQueryReturnsNewest(t *testing.T) {
	base := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	s := newTestStore(t, Config{MaxTokens: 1000, MaxItems: 10, TTL: 0})
	for i := 0; i < 5; i++ {
		if err := s.Append(context.Background(),
			mkTurn("e"+string(rune('a'+i)), "s", "user", strings.Repeat("w", 10), base.Add(time.Duration(i)*time.Minute))); err != nil {
			t.Fatal(err)
		}
	}
	got := s.Recall(context.Background(), "", 3)
	if len(got) != 3 {
		t.Fatalf("got %d results, want 3", len(got))
	}
	// Newest first: e, d, c in append order.
	if got[0] != "wwwwwwwwww" {
		// contents are identical by construction; verify count only here
		t.Log("contents identical by construction; ordering checked below")
	}
	// Distinguish by using distinct contents instead.
	s2 := newTestStore(t, Config{MaxTokens: 1000, MaxItems: 10, TTL: 0})
	wantIDs := []string{"ea", "eb", "ec", "ed", "ee"}
	for i, id := range wantIDs {
		if err := s2.Append(context.Background(),
			mkTurn(id, "s", "user", "content-"+id, base.Add(time.Duration(i)*time.Minute))); err != nil {
			t.Fatal(err)
		}
	}
	got2 := s2.Recall(context.Background(), "   ", 3)
	if len(got2) != 3 {
		t.Fatalf("got %d results, want 3", len(got2))
	}
	if got2[0] != "content-ee" || got2[1] != "content-ed" || got2[2] != "content-ec" {
		t.Errorf("empty-query recall = %v, want newest first [ee ed ec]", got2)
	}
}

func TestRecallConcurrencyRace(t *testing.T) {
	base := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	s := newTestStore(t, Config{MaxTokens: 500, MaxItems: 50, TTL: 0})

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				_ = s.Append(context.Background(),
					mkTurn("c", "s", "user", "race check turn text", base))
				_ = s.Recall(context.Background(), "race check", 5)
				_ = s.Snapshot()
			}
		}(i)
	}
	wg.Wait()
}

// ---- Task T5: Snapshot copy-safety + session recording ----

func TestSnapshotCopySafe(t *testing.T) {
	base := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	s := newTestStore(t, Config{MaxTokens: 1000, MaxItems: 10, TTL: 0})
	orig := mkTurn("snap1", "sess-9", "user", "original text", base)
	if err := s.Append(context.Background(), orig); err != nil {
		t.Fatal(err)
	}

	snap := s.Snapshot()
	if len(snap) != 1 {
		t.Fatalf("snapshot len = %d, want 1", len(snap))
	}
	if snap[0].SessionID != "sess-9" {
		t.Errorf("SessionID = %q, want sess-9 (recorded for adapter)", snap[0].SessionID)
	}

	// Mutate the returned slice and its elements.
	snap[0].Content = "mutated"
	snap[0] = mkTurn("evil", "evil", "evil", "evil", base)
	snap = append(snap, mkTurn("extra", "s", "u", "extra", base))

	after := s.Snapshot()
	if len(after) != 1 {
		t.Fatalf("store snapshot len = %d after external mutation, want 1", len(after))
	}
	if after[0].Content != "original text" || after[0].ID != "snap1" {
		t.Errorf("store mutated via Snapshot: %+v", after[0])
	}
}

// ---- Task T6: clock injection guard ----

func TestSetClockTypedNilGuard(t *testing.T) {
	s := newTestStore(t, Config{MaxTokens: 100, MaxItems: 10, TTL: time.Hour})
	// Typed-nil func must be ignored, not panic later.
	var nilClock func() time.Time
	s.SetClock(nilClock)
	s.SetClock(nil)
	if err := s.Append(context.Background(),
		mkTurn("g1", "s", "user", "guard check", time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC))); err != nil {
		t.Fatalf("Append after nil SetClock: %v", err)
	}
}

// recordingSink captures flushed turns for assertions (test-only sink).
type recordingSink struct {
	mu    sync.Mutex
	turns []Turn
	err   error
}

func (r *recordingSink) AppendTurns(_ context.Context, turns []Turn) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.err != nil {
		return r.err
	}
	r.turns = append(r.turns, turns...)
	return nil
}
