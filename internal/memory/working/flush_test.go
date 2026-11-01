package working

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/memory"
)

// ---- Task T4: Flush + EpisodicSink adapter ----

func TestFlushDrainsInOrderAndIdempotent(t *testing.T) {
	base := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	s := newTestStore(t, Config{MaxTokens: 100, MaxItems: 10, TTL: 0})

	// 10 x 30-token turns against 100 budget: 7+ evictions.
	for i := 0; i < 10; i++ {
		if err := s.Append(context.Background(),
			mkTurn("f"+string(rune('a'+i)), "sess-f", "user", string(rune('A'+i))+strings.Repeat("z", 119), base.Add(time.Duration(i)*time.Minute))); err != nil {
			t.Fatal(err)
		}
	}

	rec := &recordingSink{}
	if err := s.Flush(context.Background(), rec); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	first := len(rec.turns)
	if first == 0 {
		t.Fatal("expected evicted turns on first Flush")
	}
	for i := 1; i < len(rec.turns); i++ {
		if rec.turns[i-1].At.After(rec.turns[i].At) {
			t.Fatalf("flush order violated at %d", i)
		}
	}

	// Second Flush is idempotent: queue empty, zero turns sent.
	before := len(rec.turns)
	if err := s.Flush(context.Background(), rec); err != nil {
		t.Fatalf("second Flush: %v", err)
	}
	if len(rec.turns) != before {
		t.Errorf("second Flush sent %d turns, want 0", len(rec.turns)-before)
	}
	if s.QueuedTurns() != 0 {
		t.Errorf("queue = %d after two flushes, want 0", s.QueuedTurns())
	}

	// New evictions after a flush queue again and carry the tail.
	if err := s.Append(context.Background(),
		mkTurn("ftail", "sess-f", "user", strings.Repeat("y", 400), base.Add(time.Hour))); err != nil {
		t.Fatal(err)
	}
	if s.QueuedTurns() == 0 {
		t.Error("expected new evictions queued after post-flush appends")
	}
}

func TestFlushErrorRestoresQueue(t *testing.T) {
	base := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	s := newTestStore(t, Config{MaxTokens: 40, MaxItems: 10, TTL: 0})
	for i := 0; i < 5; i++ {
		if err := s.Append(context.Background(),
			mkTurn("e", "s", "user", strings.Repeat("q", 80), base.Add(time.Duration(i)*time.Minute))); err != nil {
			t.Fatal(err)
		}
	}
	queued := s.QueuedTurns()
	if queued == 0 {
		t.Fatal("expected queued evictions")
	}

	failing := &failingSink{err: errors.New("sink down")}
	if err := s.Flush(context.Background(), failing); err == nil {
		t.Fatal("expected Flush error from failing sink")
	}
	// Queue restored for retry: nothing lost on sink failure (P4).
	if got := s.QueuedTurns(); got != queued {
		t.Errorf("queue after failed Flush = %d, want %d (restored)", got, queued)
	}

	good := &recordingSink{}
	if err := s.Flush(context.Background(), good); err != nil {
		t.Fatalf("retry Flush: %v", err)
	}
	if len(good.turns) != queued {
		t.Errorf("retry sent %d turns, want %d", len(good.turns), queued)
	}
}

func TestFlushNilSinkRejected(t *testing.T) {
	s := newTestStore(t, Config{MaxTokens: 100, MaxItems: 10, TTL: 0})
	var nilSink EpisodicSink
	if err := s.Flush(context.Background(), nilSink); err == nil {
		t.Fatal("expected error flushing to nil sink")
	}
}

// TestFlushAdapterCompilesAgainstRealEpisodicStore exercises the adapter
// against the real FTS5 episodic store (task T4 PASS state: "adapter
// compiles against real episodic store" and lands turns via the existing
// Add path).
func TestFlushAdapterCompilesAgainstRealEpisodicStore(t *testing.T) {
	tmpDir := t.TempDir()
	mem, err := memory.NewEpisodicMemory(memory.EpisodicConfig{
		DataDir: filepath.Join(tmpDir, "episodic"),
	})
	if err != nil {
		t.Fatalf("NewEpisodicMemory: %v", err)
	}
	ctx := context.Background()
	if err := mem.Initialize(ctx); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	defer mem.Close()

	sink := NewEpisodicSink(mem)
	base := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	turns := []Turn{
		mkTurn("a1", "sess-a", "user", "alpha question about deploys", base),
		mkTurn("a2", "sess-a", "assistant", "alpha answer with steps", base.Add(time.Minute)),
	}
	if err := sink.AppendTurns(ctx, turns); err != nil {
		t.Fatalf("AppendTurns: %v", err)
	}

	// The turns are now searchable through the episodic store's own path.
	results, err := mem.Search(ctx, "alpha", 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("episodic search found %d results, want 2", len(results))
	}

	// End-to-end: store evictions flush into the real episodic store.
	s := newTestStore(t, Config{MaxTokens: 40, MaxItems: 10, TTL: 0})
	for i := 0; i < 6; i++ {
		if err := s.Append(ctx, mkTurn("w", "sess-a", "user", strings.Repeat("m", 80), base.Add(time.Duration(i)*time.Second))); err != nil {
			t.Fatal(err)
		}
	}
	if s.QueuedTurns() == 0 {
		t.Fatal("expected evictions queued")
	}
	if err := s.Flush(ctx, sink); err != nil {
		t.Fatalf("Flush to real episodic store: %v", err)
	}
	if s.QueuedTurns() != 0 {
		t.Errorf("queue not drained: %d", s.QueuedTurns())
	}
}

// failingSink always errors (Flush restore-path test).
type failingSink struct{ err error }

func (f *failingSink) AppendTurns(_ context.Context, _ []Turn) error { return f.err }
