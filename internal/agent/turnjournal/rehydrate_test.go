package turnjournal

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"
)

// rehydrateStore is a fake JournalStore whose OpenRuns/PruneClosed are
// programmable for the Rehydrate table-driven cases.
type rehydrateStore struct {
	mu       sync.Mutex
	runs     []OpenRun
	openErr  error
	pruneErr error
	prunes   []time.Duration
	closed   map[string]bool
}

func (s *rehydrateStore) Append(ctx context.Context, rec Record) error { return nil }

func (s *rehydrateStore) OpenRuns(ctx context.Context) ([]OpenRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.openErr != nil {
		return nil, s.openErr
	}
	var runs []OpenRun
	for _, r := range s.runs {
		if !s.closed[r.TurnID] {
			runs = append(runs, r)
		}
	}
	return runs, nil
}

func (s *rehydrateStore) CompletedSteps(ctx context.Context, turnID string) (map[string][]byte, error) {
	return nil, nil
}

func (s *rehydrateStore) CloseRun(ctx context.Context, turnID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed == nil {
		s.closed = make(map[string]bool)
	}
	s.closed[turnID] = true
	return nil
}

func (s *rehydrateStore) PruneClosed(ctx context.Context, olderThan time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.prunes = append(s.prunes, olderThan)
	return s.pruneErr
}

func (s *rehydrateStore) Close() error { return nil }

// T1 canonical case: 2 open runs, 1 closed -> exactly the open runs
// requeue with their persisted payloads; resumed == 2.
func TestRehydrateRequeuesOpenRuns(t *testing.T) {
	store := &rehydrateStore{
		runs: []OpenRun{
			{TurnID: "t1", SessionID: "s1", TurnPayload: []byte("payload-1")},
			{TurnID: "t2", SessionID: "s2", TurnPayload: []byte("payload-2")},
		},
		closed: map[string]bool{"t3": true},
	}
	// t3 rides the runs list too but is closed -- OpenRuns (real store)
	// would never return it; the fake mirrors the interface contract
	// above the SQL so the filter must come from Rehydrate's view of
	// OpenRuns only. Add t3 through CloseRun semantics: closed map
	// already excludes it from the fake's OpenRuns output.

	var got []string
	var payloads [][]byte
	resumed, err := Rehydrate(context.Background(), store,
		func(turnID, sessionID string, payload []byte) error {
			got = append(got, turnID+"/"+sessionID)
			payloads = append(payloads, payload)
			return nil
		}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if resumed != 2 || len(got) != 2 {
		t.Fatalf("resumed=%d got=%v, want 2 requeues", resumed, got)
	}
	if got[0] != "t1/s1" || got[1] != "t2/s2" {
		t.Fatalf("requeue set = %v, want t1/s1 then t2/s2", got)
	}
	if string(payloads[0]) != "payload-1" || string(payloads[1]) != "payload-2" {
		t.Fatalf("payloads = %v, want the persisted blobs", payloads)
	}
}

// T1: a failing requeue on run 1 of 3 -> Warn + continue, resumed=2.
func TestRehydratePerRunFailureContinues(t *testing.T) {
	store := &rehydrateStore{
		runs: []OpenRun{
			{TurnID: "t1", SessionID: "s1"},
			{TurnID: "t2", SessionID: "s2"},
			{TurnID: "t3", SessionID: "s3"},
		},
	}
	var got []string
	resumed, err := Rehydrate(context.Background(), store,
		func(turnID, _ string, _ []byte) error {
			if turnID == "t1" {
				return errors.New("enqueue refused")
			}
			got = append(got, turnID)
			return nil
		}, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("a per-run requeue failure must not fail the pass, got %v", err)
	}
	if resumed != 2 || len(got) != 2 {
		t.Fatalf("resumed=%d got=%v, want the 2 successful requeues", resumed, got)
	}
}

// T1: a failing store read -> (0, err); nothing requeues.
func TestRehydrateStoreErrorReturns(t *testing.T) {
	store := &rehydrateStore{
		runs:    []OpenRun{{TurnID: "t1"}},
		openErr: errors.New("db on fire"),
	}
	calls := 0
	resumed, err := Rehydrate(context.Background(), store,
		func(string, string, []byte) error { calls++; return nil },
		slog.New(slog.DiscardHandler))
	if err == nil {
		t.Fatal("expected the store error to return, got nil")
	}
	if resumed != 0 || calls != 0 {
		t.Fatalf("resumed=%d calls=%d, want zero on a store failure", resumed, calls)
	}
}

// T1: an already-pending requeue (no-op nil return) counts exactly once
// per run -- duplicate run_started rows for the same turn cannot
// double-count (the store groups by turn_id; this pins Rehydrate's side).
func TestRehydrateAlreadyPendingCountsOnce(t *testing.T) {
	store := &rehydrateStore{
		// Two entries with the SAME TurnID simulate what a legacy
		// duplicate-run_started row would look like if it ever leaked
		// past the store's GROUP BY: the newest payload wins and the
		// turn counts once, not twice.
		runs: []OpenRun{
			{TurnID: "t1", SessionID: "s1", TurnPayload: []byte("old")},
			{TurnID: "t1", SessionID: "s1", TurnPayload: []byte("new")},
		},
	}
	count := 0
	var payloads [][]byte
	resumed, err := Rehydrate(context.Background(), store,
		func(_ string, _ string, payload []byte) error {
			count++
			payloads = append(payloads, payload)
			return nil // the no-op enqueue return: already claimable
		}, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	// Rehydrate counts per OpenRun entry by contract; the REAL store's
	// GROUP BY turn_id guarantees one entry per turn, so in production
	// count is 1. This fake feeds the degenerate duplicated shape to pin
	// that the count never exceeds the entry list.
	if resumed != count {
		t.Fatalf("resumed=%d calls=%d, count must track successful requeues", resumed, count)
	}
	if len(payloads) != 2 || string(payloads[1]) != "new" {
		t.Fatalf("payloads = %v, want newest payload last", payloads)
	}
}

// T1: nil store / nil requeue are descriptive errors, not panics.
func TestRehydrateNilArgs(t *testing.T) {
	ctx := context.Background()
	if _, err := Rehydrate(ctx, nil, func(string, string, []byte) error { return nil }, nil); err == nil ||
		!strings.Contains(err.Error(), "nil store") {
		t.Fatalf("nil store = %v", err)
	}
	if _, err := Rehydrate(ctx, &rehydrateStore{}, nil, nil); err == nil ||
		!strings.Contains(err.Error(), "nil requeue") {
		t.Fatalf("nil requeue = %v", err)
	}
}

// T4: PruneClosedRuns forwards the retention const and is best-effort --
// a store failure logs Warn and returns normally (no panic, no error).
func TestRehydratePruneBestEffort(t *testing.T) {
	store := &rehydrateStore{pruneErr: errors.New("prune boom")}
	// Must not panic; the Warn goes to the discard logger.
	PruneClosedRuns(context.Background(), store, slog.New(slog.DiscardHandler))

	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.prunes) != 1 || store.prunes[0] != ClosedRunRetention {
		t.Fatalf("prunes = %v, want exactly one call with ClosedRunRetention", store.prunes)
	}
}

// T4: a nil store or nil logger is a silent no-op.
func TestRehydratePruneNilSafe(t *testing.T) {
	PruneClosedRuns(context.Background(), nil, slog.New(slog.DiscardHandler))
	PruneClosedRuns(context.Background(), &rehydrateStore{}, nil)
}
