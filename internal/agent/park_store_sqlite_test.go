package agent

// Tests for parked-turn persistence (park_store_sqlite.go + the parker's
// SetParkPersistence/reArm/persistPark/unpersistPark paths).
//
// The restart scenarios run against the REAL SQLiteParkStore on a temp
// file (same driver the daemon uses), not a fake: the durability contract
// is "a fresh parker over the same file re-arms the rows", which only the
// real store can prove. A database/sql-free fake (mapPersistence) covers
// the store-error paths.

import (
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/llm"
)

// newParkStoreTestLogger silences parker/store logs during tests.
func newParkStoreTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(discardTestWriter{}, nil))
}

// newTestParkStore opens a SQLiteParkStore on a fresh temp file.
func newTestParkStore(t *testing.T) *SQLiteParkStore {
	t.Helper()
	dir := t.TempDir()
	store, err := NewSQLiteParkStore(filepath.Join(dir, "parks.db"), newParkStoreTestLogger())
	if err != nil {
		t.Fatalf("NewSQLiteParkStore: %v", err)
	}
	return store
}

// fakePersistence is an in-memory ParkPersistence for error-path tests.
type fakePersistence struct {
	mu         sync.Mutex
	rows       map[string]ParkedTurnRecord
	saveErr    error
	deleteErr  error
	loadErr    error
	saves      int
	deletes    int
	deleteKeys []string
}

func (f *fakePersistence) Save(_ context.Context, kind ParkKind, key string, rec ParkedTurnRecord) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.saves++
	if f.saveErr != nil {
		return f.saveErr
	}
	if f.rows == nil {
		f.rows = make(map[string]ParkedTurnRecord)
	}
	f.rows[string(kind)+"|"+key] = rec
	return nil
}

func (f *fakePersistence) Delete(_ context.Context, kind ParkKind, key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deletes++
	f.deleteKeys = append(f.deleteKeys, string(kind)+"|"+key)
	if f.deleteErr != nil {
		return f.deleteErr
	}
	delete(f.rows, string(kind)+"|"+key)
	return nil
}

func (f *fakePersistence) Load(_ context.Context, kind ParkKind, now time.Time) ([]ParkedTurnRecord, []string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.loadErr != nil {
		return nil, nil, f.loadErr
	}
	var recs []ParkedTurnRecord
	var keys []string
	for k, rec := range f.rows {
		prefix := string(kind) + "|"
		if len(k) < len(prefix) || k[:len(prefix)] != prefix {
			continue
		}
		if rec.ResumeAt.IsZero() || !rec.ResumeAt.After(now) {
			delete(f.rows, k) // prune at load, like the SQLite store
			continue
		}
		recs = append(recs, rec)
		keys = append(keys, k[len(prefix):])
	}
	return recs, keys, nil
}

// persistedRowCount queries the store directly (test-side verification).
func (s *SQLiteParkStore) persistedRowCount(t *testing.T, kind ParkKind) int {
	t.Helper()
	rows, err := s.db.Query(`SELECT COUNT(*) FROM parked_turns WHERE kind = ?`, string(kind))
	if err != nil {
		t.Fatalf("count query: %v", err)
	}
	defer rows.Close()
	if !rows.Next() {
		t.Fatal("count query returned no rows")
	}
	var n int
	if err := rows.Scan(&n); err != nil {
		t.Fatalf("count scan: %v", err)
	}
	return n
}

// --- (a) Park → simulated restart → re-arm ---------------------------------

func TestTurnParkerPersistence_ReArmAfterRestart(t *testing.T) {
	store := newTestParkStore(t)
	defer func() { _ = store.Close() }()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	resumeAt := time.Now().Add(50 * time.Millisecond)

	// Run 1: park a chat turn with persistence wired, then stop
	// (records parked at Stop stay in the store).
	{
		var mu sync.Mutex
		resumed := 0
		p1 := NewTurnParker(newParkStoreTestLogger(), func(context.Context, ParkedTurnRecord) {
			mu.Lock()
			resumed++
			mu.Unlock()
		}, time.Hour)
		p1.logger = newParkStoreTestLogger()
		p1.SetPollInterval(10 * time.Millisecond)
		p1.SetParkPersistence(store, ParkKindChat)
		p1.Start(ctx)
		if !p1.Park(ParkedTurnRecord{
			SessionID:   "s-restart",
			Class:       llm.FailureQuota,
			ResumeAt:    resumeAt,
			MaxAttempts: 3,
			TurnPayload: []byte(`{"message":"hi"}`),
		}) {
			t.Fatal("expected park to succeed")
		}
		// Simulated restart BEFORE the resume time: nothing drained yet.
		p1.Stop()
		if n := store.persistedRowCount(t, ParkKindChat); n != 1 {
			t.Fatalf("run 1: expected 1 persisted row, got %d", n)
		}
	}

	// Run 2: a FRESH parker over the SAME store re-arms the record and
	// resumes it once its time passes.
	{
		var mu sync.Mutex
		var resumed []ParkedTurnRecord
		p2 := NewTurnParker(newParkStoreTestLogger(), func(_ context.Context, rec ParkedTurnRecord) {
			mu.Lock()
			resumed = append(resumed, rec)
			mu.Unlock()
		}, time.Hour)
		p2.logger = newParkStoreTestLogger()
		p2.SetPollInterval(10 * time.Millisecond)
		p2.SetParkPersistence(store, ParkKindChat)
		p2.Start(ctx)
		defer p2.Stop()

		waitUntil(t, 2*time.Second, func() bool {
			mu.Lock()
			defer mu.Unlock()
			return len(resumed) == 1
		})
		mu.Lock()
		rec := resumed[0]
		mu.Unlock()
		if rec.SessionID != "s-restart" || rec.Class != llm.FailureQuota || rec.MaxAttempts != 3 {
			t.Fatalf("re-armed record mismatch: %+v", rec)
		}
		if string(rec.TurnPayload) != `{"message":"hi"}` {
			t.Fatalf("payload not preserved across restart: %q", string(rec.TurnPayload))
		}
		// Resume deletes the row (no resurrection on a third boot).
		waitUntil(t, 2*time.Second, func() bool {
			return store.persistedRowCount(t, ParkKindChat) == 0
		})
	}
}

// --- (b) Expired records pruned at load -------------------------------------

func TestTurnParkerPersistence_ExpiredPrunedAtLoad(t *testing.T) {
	store := newTestParkStore(t)
	defer func() { _ = store.Close() }()

	// Seed rows directly: one expired, one live.
	expired := ParkedTurnRecord{
		SessionID:   "s-expired",
		Class:       llm.FailureQuota,
		ResumeAt:    time.Now().Add(-time.Hour),
		TurnPayload: []byte(`{"message":"old"}`),
	}
	live := ParkedTurnRecord{
		SessionID:   "s-live",
		Class:       llm.FailureThrottle,
		ResumeAt:    time.Now().Add(time.Hour),
		MaxAttempts: 1,
		TurnPayload: []byte(`{"message":"new"}`),
	}
	ctx := context.Background()
	if err := store.Save(ctx, ParkKindChat, "expired", expired); err != nil {
		t.Fatalf("seed expired: %v", err)
	}
	if err := store.Save(ctx, ParkKindChat, "live", live); err != nil {
		t.Fatalf("seed live: %v", err)
	}

	records, keys, err := store.Load(ctx, ParkKindChat, time.Now())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(records) != 1 || len(keys) != 1 {
		t.Fatalf("expected 1 survivor, got %d records / %d keys", len(records), len(keys))
	}
	if records[0].SessionID != "s-live" || keys[0] != "live" {
		t.Fatalf("wrong survivor: %+v key=%q", records[0], keys[0])
	}
	if n := store.persistedRowCount(t, ParkKindChat); n != 1 {
		t.Fatalf("expired row not pruned from disk: %d rows remain", n)
	}
}

// --- (c) Resume deletes the row (no resurrection) ---------------------------

func TestTurnParkerPersistence_ResumeDeletesRow(t *testing.T) {
	store := newTestParkStore(t)
	defer func() { _ = store.Close() }()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var mu sync.Mutex
	resumed := 0
	p := NewTurnParker(newParkStoreTestLogger(), func(context.Context, ParkedTurnRecord) {
		mu.Lock()
		resumed++
		mu.Unlock()
	}, time.Hour)
	p.logger = newParkStoreTestLogger()
	p.SetPollInterval(10 * time.Millisecond)
	p.SetParkPersistence(store, ParkKindChat)
	p.Start(ctx)
	defer p.Stop()

	if !p.Park(ParkedTurnRecord{
		SessionID: "s-del",
		Class:     llm.FailureQuota,
		ResumeAt:  time.Now().Add(20 * time.Millisecond),
	}) {
		t.Fatal("expected park to succeed")
	}
	if n := store.persistedRowCount(t, ParkKindChat); n != 1 {
		t.Fatalf("expected 1 persisted row after park, got %d", n)
	}
	waitUntil(t, 2*time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return resumed > 0
	})
	if n := store.persistedRowCount(t, ParkKindChat); n != 0 {
		t.Fatalf("row survived resume — would resurrect on restart (%d rows)", n)
	}
}

// --- (d) Memory-only mode unaffected -----------------------------------------

func TestTurnParkerPersistence_MemoryOnlyUnchanged(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	p := NewTurnParker(parkedTurnTestLogger(), func(context.Context, ParkedTurnRecord) {}, time.Hour)
	p.SetPollInterval(10 * time.Millisecond)
	p.Start(ctx)
	defer p.Stop()

	if !p.Park(ParkedTurnRecord{
		SessionID: "s-mem",
		Class:     llm.FailureQuota,
		ResumeAt:  time.Now().Add(time.Hour),
	}) {
		t.Fatal("expected park to succeed without persistence")
	}
	if got := p.Pending(); got != 1 {
		t.Fatalf("pending = %d, want 1", got)
	}
	// Refusals unchanged.
	if p.Park(ParkedTurnRecord{SessionID: "s-zero"}) {
		t.Fatal("zero ResumeAt must be refused")
	}
	if p.Park(ParkedTurnRecord{SessionID: "s-past", ResumeAt: time.Now().Add(-time.Minute)}) {
		t.Fatal("past ResumeAt must be refused")
	}
}

// Persistence must not change Park's in-memory contract: a save failure
// still parks the record in memory (best-effort write-behind).
func TestTurnParkerPersistence_SaveFailureStillParks(t *testing.T) {
	store := &fakePersistence{saveErr: errors.New("disk on fire")}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	p := NewTurnParker(newParkStoreTestLogger(), func(context.Context, ParkedTurnRecord) {}, time.Hour)
	p.logger = newParkStoreTestLogger()
	p.SetPollInterval(10 * time.Millisecond)
	p.SetParkPersistence(store, ParkKindChat)
	p.Start(ctx)
	defer p.Stop()

	if !p.Park(ParkedTurnRecord{
		SessionID: "s-fail",
		Class:     llm.FailureQuota,
		ResumeAt:  time.Now().Add(time.Hour),
	}) {
		t.Fatal("save failure must not refuse the park")
	}
	if got := p.Pending(); got != 1 {
		t.Fatalf("pending = %d, want 1 (record parked in memory despite save failure)", got)
	}
}

// reArm load failure must not wedge Start (records stay for next boot).
func TestTurnParkerPersistence_LoadFailureDoesNotWedgeStart(t *testing.T) {
	store := &fakePersistence{loadErr: errors.New("corrupt page")}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	p := NewTurnParker(newParkStoreTestLogger(), func(context.Context, ParkedTurnRecord) {}, time.Hour)
	p.logger = newParkStoreTestLogger()
	p.SetPollInterval(10 * time.Millisecond)
	p.SetParkPersistence(store, ParkKindChat)
	p.Start(ctx)
	defer p.Stop()

	if got := p.Pending(); got != 0 {
		t.Fatalf("pending = %d, want 0 after failed re-arm", got)
	}
}

// --- SQLiteParkStore unit coverage -------------------------------------------

func TestSQLiteParkStore_KindScoping(t *testing.T) {
	store := newTestParkStore(t)
	defer func() { _ = store.Close() }()
	ctx := context.Background()

	rec := ParkedTurnRecord{
		SessionID:      "s-both",
		ConversationID: "conv-1", // distinct from SessionID by design (AGENTS.md invariant)
		AgentID:        "emp-1",
		Class:          llm.FailureThrottle,
		ResumeAt:       time.Now().Add(time.Hour),
		TurnPayload:    []byte(`{"phase":"assess"}`),
	}
	if err := store.Save(ctx, ParkKindChat, "k1", rec); err != nil {
		t.Fatalf("save chat: %v", err)
	}
	if err := store.Save(ctx, ParkKindEpisode, "k1", rec); err != nil {
		t.Fatalf("save episode: %v", err)
	}
	// Same key, different kinds: both visible only through their own kind.
	chatRecs, _, err := store.Load(ctx, ParkKindChat, time.Now())
	if err != nil || len(chatRecs) != 1 {
		t.Fatalf("chat load: %d records, err %v", len(chatRecs), err)
	}
	epiRecs, _, err := store.Load(ctx, ParkKindEpisode, time.Now())
	if err != nil || len(epiRecs) != 1 {
		t.Fatalf("episode load: %d records, err %v", len(epiRecs), err)
	}
	// Deleting one kind leaves the other.
	if err := store.Delete(ctx, ParkKindChat, "k1"); err != nil {
		t.Fatalf("delete chat: %v", err)
	}
	if chatRecs, _, _ := store.Load(ctx, ParkKindChat, time.Now()); len(chatRecs) != 0 {
		t.Fatal("chat row should be gone")
	}
	if epiRecs, _, _ := store.Load(ctx, ParkKindEpisode, time.Now()); len(epiRecs) != 1 {
		t.Fatal("episode row must survive a chat-kind delete")
	}
}

func TestSQLiteParkStore_UpsertReplacesOnKeyCollision(t *testing.T) {
	store := newTestParkStore(t)
	defer func() { _ = store.Close() }()
	ctx := context.Background()

	key := "quota|emp-1|assess|scheduler|topic"
	first := ParkedTurnRecord{
		SessionID: "e1", AgentID: "emp-1", Class: llm.FailureQuota,
		ResumeAt: time.Now().Add(time.Hour), TurnPayload: []byte(`{"v":1}`),
	}
	second := ParkedTurnRecord{
		SessionID: "e1", AgentID: "emp-1", Class: llm.FailureQuota,
		ResumeAt: time.Now().Add(2 * time.Hour), TurnPayload: []byte(`{"v":2}`),
	}
	if err := store.Save(ctx, ParkKindEpisode, key, first); err != nil {
		t.Fatalf("save 1: %v", err)
	}
	if err := store.Save(ctx, ParkKindEpisode, key, second); err != nil {
		t.Fatalf("save 2: %v", err)
	}
	recs, keys, err := store.Load(ctx, ParkKindEpisode, time.Now())
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(recs) != 1 || len(keys) != 1 {
		t.Fatalf("duplicate rows after key collision: %d records", len(recs))
	}
	if string(recs[0].TurnPayload) != `{"v":2}` {
		t.Fatalf("upsert did not replace the row: %q", string(recs[0].TurnPayload))
	}
}

func TestSQLiteParkStore_DeleteIdempotentAndRoundTrip(t *testing.T) {
	store := newTestParkStore(t)
	defer func() { _ = store.Close() }()
	ctx := context.Background()

	// Delete of an absent row is not an error.
	if err := store.Delete(ctx, ParkKindChat, "nope"); err != nil {
		t.Fatalf("absent delete: %v", err)
	}

	resumeAt := time.Now().Add(90 * time.Minute).Truncate(time.Second)
	rec := ParkedTurnRecord{
		ConversationID: "conv-7",
		SessionID:      "sess-7",
		AgentID:        "agent-9",
		Class:          llm.FailureQuota,
		ResumeAt:       resumeAt,
		Attempt:        2,
		MaxAttempts:    5,
		TurnPayload:    []byte(`{"message":"m","conversation_id":"conv-7"}`),
	}
	if err := store.Save(ctx, ParkKindChat, "rt", rec); err != nil {
		t.Fatalf("save: %v", err)
	}
	recs, keys, err := store.Load(ctx, ParkKindChat, time.Now())
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(recs) != 1 {
		t.Fatalf("expected 1 record, got %d", len(recs))
	}
	got := recs[0]
	// session_id vs conversation_id both round-trip, unswapped.
	if got.SessionID != "sess-7" || got.ConversationID != "conv-7" {
		t.Fatalf("identifier round-trip mismatch: session=%q conversation=%q", got.SessionID, got.ConversationID)
	}
	if got.AgentID != "agent-9" || got.Class != llm.FailureQuota || got.Attempt != 2 || got.MaxAttempts != 5 {
		t.Fatalf("field round-trip mismatch: %+v", got)
	}
	if !got.ResumeAt.Equal(resumeAt) {
		t.Fatalf("resume_at round-trip: got %v want %v", got.ResumeAt, resumeAt)
	}
	if keys[0] != "rt" {
		t.Fatalf("key round-trip: %q", keys[0])
	}
	if err := store.Delete(ctx, ParkKindChat, "rt"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if recs, _, _ := store.Load(ctx, ParkKindChat, time.Now()); len(recs) != 0 {
		t.Fatal("row still present after delete")
	}
}

// Reused key across independent stores/files must not interfere: each
// daemon process has its own parks.db (fresh store = fresh view).
func TestSQLiteParkStore_FreshDBIsEmpty(t *testing.T) {
	store := newTestParkStore(t)
	defer func() { _ = store.Close() }()
	recs, keys, err := store.Load(context.Background(), ParkKindChat, time.Now())
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(recs) != 0 || len(keys) != 0 {
		t.Fatalf("fresh store not empty: %d records", len(recs))
	}
}
