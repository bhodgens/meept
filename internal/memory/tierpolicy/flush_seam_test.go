package tierpolicy

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/memory/working"
)

// A3-5 pins: the working-tier eviction queue must have a production
// drain riding the engine tick. The engine exposes it as Deps.Flush,
// invoked (nil-safe) on EVERY tickOnce exit path.

// countingSink records AppendTurns deliveries.
type countingSink struct {
	mu    sync.Mutex
	turns [][]working.Turn
}

func (s *countingSink) AppendTurns(_ context.Context, turns []working.Turn) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.turns = append(s.turns, turns)
	return nil
}

// TestTickOnceDrainsWorkingFlushSeam: engine tick (via the exported
// TickOnceNow, same code path as the background loop) calls Deps.Flush
// even when the pass plans NOTHING (under-ceiling noop). Pre-fix the
// seam did not exist and evicted ring turns queued forever until
// drop-oldest silently ate them.
func TestTickOnceDrainsWorkingFlushSeam(t *testing.T) {
	t.Parallel()
	store, err := working.New(working.Config{MaxTokens: 40, MaxItems: 10, TTL: 0}, chars4Counter{})
	if err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)
	for i := range 8 {
		if err := store.Append(context.Background(), working.Turn{
			ID: "t", SessionID: "s", Role: "user",
			Content: string(rune('A'+i)) + "xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx",
			At:      base.Add(time.Duration(i) * time.Minute),
		}); err != nil {
			t.Fatal(err)
		}
	}
	queued := store.QueuedTurns()
	if queued == 0 {
		t.Fatal("fixture must overflow the ring")
	}

	var flushCalls int
	e := New(enabledCfg(), Deps{
		// Under-ceiling states: evaluate plans nothing, yet the
		// drain must still run (ring pressure is independent of the
		// episodic band posture).
		States: noopStates{},
		Flush: func(ctx context.Context) error {
			flushCalls++
			return store.Flush(ctx, &countingSink{})
		},
	})
	e.TickOnceNow(context.Background())
	if flushCalls != 1 {
		t.Fatalf("flush seam calls = %d, want 1 per tick", flushCalls)
	}
	if store.QueuedTurns() != 0 {
		t.Fatalf("queue = %d after wired flush, want drained", store.QueuedTurns())
	}
}

type noopStates struct{}

func (noopStates) TierStates(context.Context) ([]TierState, error) {
	return []TierState{{Tier: "working", Tokens: 1, Items: 1, Ceiling: 1000}}, nil
}

type chars4Counter struct{}

func (chars4Counter) CountTokens(s string) int { return (len(s) + 3) / 4 }

// TestTickOnceFlushErrorNotFatal: a sink failure logs, never aborts;
// the Flush contract restores the batch for the next tick.
func TestTickOnceFlushErrorNotFatal(t *testing.T) {
	t.Parallel()
	calls := 0
	e := New(enabledCfg(), Deps{
		States: noopStates{},
		Flush: func(context.Context) error {
			calls++
			return errors.New("sink down")
		},
	})
	e.TickOnceNow(context.Background())
	e.TickOnceNow(context.Background())
	if calls != 2 {
		t.Fatalf("flush calls = %d, want one per tick even on error", calls)
	}
}

// TestFlushNowNilSafe: FlushNow on an engine without the seam is a
// nil no-op (typed-nil setter rule posture).
func TestFlushNowNilSafe(t *testing.T) {
	t.Parallel()
	e := New(enabledCfg(), Deps{})
	if err := e.FlushNow(context.Background()); err != nil {
		t.Fatalf("nil seam FlushNow: %v", err)
	}
	var nilEng *Engine
	if err := nilEng.FlushNow(context.Background()); err != nil {
		t.Fatal("nil engine FlushNow must be nil-safe")
	}
	nilEng.TickOnceNow(context.Background())
}
