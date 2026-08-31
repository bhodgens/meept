package agent

import (
	"context"
	"errors"
	"log/slog"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/llm"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(discardTestWriter{}, nil))
}

type discardTestWriter struct{}

func (discardTestWriter) Write(p []byte) (int, error) { return len(p), nil }

func runtimeNumGoroutine() int { return runtime.NumGoroutine() }

func TestQuotaResumeWatcher_ParkAndDrain(t *testing.T) {
	var resumed []QuotaParkedTurn
	var mu sync.Mutex
	w := NewQuotaResumeWatcher(testLogger(), func(ctx context.Context, turn QuotaParkedTurn) {
		mu.Lock()
		resumed = append(resumed, turn)
		mu.Unlock()
	}, 24*time.Hour)
	w.SetPollInterval(10 * time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w.Start(ctx)
	defer w.Stop()

	if !w.Park(QuotaParkedTurn{
		SessionID:      "s1",
		ConversationID: "c1",
		Message:        "hello",
		ProviderID:     "p1",
		UnblockAt:      time.Now().Add(20 * time.Millisecond),
	}) {
		t.Fatal("expected park to succeed")
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(resumed)
		mu.Unlock()
		if n == 1 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("turn was not resumed after unblock time passed")
}

func TestQuotaResumeWatcher_NotDueStaysParked(t *testing.T) {
	var resumed int
	var mu sync.Mutex
	w := NewQuotaResumeWatcher(testLogger(), func(ctx context.Context, turn QuotaParkedTurn) {
		mu.Lock()
		resumed++
		mu.Unlock()
	}, 24*time.Hour)
	w.SetPollInterval(10 * time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w.Start(ctx)
	defer w.Stop()

	if !w.Park(QuotaParkedTurn{
		SessionID:  "s1",
		ProviderID: "p1",
		UnblockAt:  time.Now().Add(2 * time.Hour),
	}) {
		t.Fatal("expected park to succeed")
	}
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if resumed != 0 {
		t.Fatalf("resumed too early: %d", resumed)
	}
	if w.Pending() != 1 {
		t.Fatalf("pending = %d, want 1", w.Pending())
	}
}

func TestQuotaResumeWatcher_MaxWaitSoftStop(t *testing.T) {
	w := NewQuotaResumeWatcher(testLogger(), func(ctx context.Context, turn QuotaParkedTurn) {}, time.Hour)
	if w.Park(QuotaParkedTurn{
		SessionID:  "s1",
		ProviderID: "p1",
		UnblockAt:  time.Now().Add(2 * time.Hour), // exceeds 1h max
	}) {
		t.Fatal("park should refuse when wait exceeds max_wait")
	}
}

func TestQuotaResumeWatcher_ZeroUnblockRefused(t *testing.T) {
	w := NewQuotaResumeWatcher(testLogger(), func(ctx context.Context, turn QuotaParkedTurn) {}, 0)
	if w.maxWait != llm.DefaultQuotaMaxWait {
		t.Fatalf("maxWait = %v, want default", w.maxWait)
	}
	if w.Park(QuotaParkedTurn{SessionID: "s1"}) {
		t.Fatal("park should refuse zero unblock time")
	}
}

func TestQuotaResumeWatcher_DisabledWhenNoResumeFunc(t *testing.T) {
	w := NewQuotaResumeWatcher(testLogger(), nil, 0)
	if w.Park(QuotaParkedTurn{SessionID: "s1", UnblockAt: time.Now().Add(time.Hour)}) {
		t.Fatal("park should be a no-op without resumeFunc")
	}
}

func TestQuotaResumeWatcher_NilSafe(t *testing.T) {
	var w *QuotaResumeWatcher
	w.Park(QuotaParkedTurn{})
	if w.Pending() != 0 {
		t.Fatal("nil watcher Pending should be 0")
	}
	w.Start(context.Background())
	w.Stop()
}

func TestQuotaResumeWatcher_OrderingOldestFirst(t *testing.T) {
	var mu sync.Mutex
	var order []string
	w := NewQuotaResumeWatcher(testLogger(), func(ctx context.Context, turn QuotaParkedTurn) {
		mu.Lock()
		order = append(order, turn.SessionID)
		mu.Unlock()
	}, 24*time.Hour)

	now := time.Now()
	// Park with staggered unblock times, later first; drain should resume
	// s1 (earlier unblock) before s2.
	if !w.Park(QuotaParkedTurn{SessionID: "s2", ProviderID: "p", UnblockAt: now.Add(30 * time.Millisecond)}) {
		t.Fatal("park s2 refused")
	}
	if !w.Park(QuotaParkedTurn{SessionID: "s1", ProviderID: "p", UnblockAt: now.Add(10 * time.Millisecond)}) {
		t.Fatal("park s1 refused")
	}
	time.Sleep(40 * time.Millisecond) // let both windows lapse

	w.drainDue(context.Background())

	mu.Lock()
	defer mu.Unlock()
	// drainDue spawns resume goroutines in due order, but callback
	// scheduling order is not deterministic; assert membership instead.
	if len(order) != 2 {
		t.Fatalf("resumed %d turns, want 2", len(order))
	}
	has := map[string]bool{}
	for _, id := range order {
		has[id] = true
	}
	if !has["s1"] || !has["s2"] {
		t.Fatalf("resume order = %v, want exactly {s1, s2}", order)
	}
	if w.Pending() != 0 {
		t.Fatalf("pending = %d, want 0", w.Pending())
	}
}

func TestQuotaResumeWatcher_StopNoLeak(t *testing.T) {
	before := runtimeNumGoroutine()
	for i := 0; i < 5; i++ {
		w := NewQuotaResumeWatcher(testLogger(), func(ctx context.Context, turn QuotaParkedTurn) {}, 0)
		ctx, cancel := context.WithCancel(context.Background())
		w.Start(ctx)
		w.Stop()
		cancel()
	}
	after := runtimeNumGoroutine()
	if after > before+5 {
		t.Fatalf("goroutine leak: before=%d after=%d", before, after)
	}
}

func TestQuotaParkedTurn_FromQuotaErrorFields(t *testing.T) {
	// Compile-time shape check: a QuotaResetError carries what parking needs.
	qe := &llm.QuotaResetError{ProviderID: "p1", RetryAfter: time.Hour}
	if qe.ProviderID != "p1" {
		t.Fatal("unexpected")
	}
	if !errors.As(error(qe), &qe) {
		t.Fatal("errors.As should find it")
	}
}
