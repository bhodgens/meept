package worker

// Worker wake-channel tests: an idle worker claims within milliseconds of
// a wake signal instead of waiting out its 1s+ poll timer. A nil WakeCh
// must reproduce legacy poll-only behavior exactly (a nil channel in a
// select blocks forever, so the select case is simply never ready).
// See docs/plans/20261004-agent-routing/01-worker-wakeup.md.

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/queue"
)

// wakeFakeQueue is a Queue stub whose Claim returns ErrNoJobAvailable
// until armed; it logs the wall-clock time of every Claim so tests can
// measure claim latency after a wake signal.
type wakeFakeQueue struct {
	mu         sync.Mutex
	armed      bool
	claimTimes []time.Time
}

func (q *wakeFakeQueue) Enqueue(_ context.Context, _ *queue.Job) error { return nil }

func (q *wakeFakeQueue) Claim(_ context.Context, _ string, _ []string, _ string) (*queue.Job, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.claimTimes = append(q.claimTimes, time.Now())
	if !q.armed {
		return nil, queue.ErrNoJobAvailable
	}
	job, err := queue.NewJob(queue.JobTypeOneOff, map[string]string{"prompt": "wake"})
	if err != nil {
		return nil, fmt.Errorf("NewJob: %w", err)
	}
	return job, nil
}

func (q *wakeFakeQueue) MarkProcessing(context.Context, string) error    { return nil }
func (q *wakeFakeQueue) Complete(context.Context, string, any) error     { return nil }
func (q *wakeFakeQueue) Fail(context.Context, string, error) error       { return nil }
func (q *wakeFakeQueue) Retry(context.Context, string) error             { return nil }
func (q *wakeFakeQueue) Get(context.Context, string) (*queue.Job, error) { return nil, nil }
func (q *wakeFakeQueue) ListByState(context.Context, queue.JobState, int) ([]*queue.Job, error) {
	return nil, nil
}
func (q *wakeFakeQueue) ListByTaskID(context.Context, string) ([]*queue.Job, error) {
	return nil, nil
}

func (q *wakeFakeQueue) Close() error { return nil }

func (q *wakeFakeQueue) Stats(context.Context) (*queue.QueueStats, error) {
	return &queue.QueueStats{}, nil
}

func (q *wakeFakeQueue) RecoverFromDeadLetter(context.Context, string) (*queue.Job, error) {
	return nil, nil
}

func (q *wakeFakeQueue) ListDeadLetter(context.Context, int) ([]*queue.Job, error) {
	return nil, nil
}

func (q *wakeFakeQueue) DeadLetterStats(context.Context) (int, error) {
	return 0, nil
}

func (q *wakeFakeQueue) claimCount() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.claimTimes)
}

func (q *wakeFakeQueue) lastClaimTime() time.Time {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.claimTimes[len(q.claimTimes)-1]
}

func (q *wakeFakeQueue) setArmed(v bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.armed = v
}

type noopProcessor struct{}

func (noopProcessor) Process(_ context.Context, _ *queue.Job) (any, error) {
	return nil, nil
}

// TestWorker_WakeClaimsImmediately: Claim must run within 50ms of the
// wake signal — far below the 1s poll floor.
func TestWorker_WakeClaimsImmediately(t *testing.T) {
	if testing.Short() {
		t.Skip("timing-sensitive")
	}
	q := &wakeFakeQueue{}
	wakeCh := make(chan struct{}, 1)
	w, err := NewWorker(Config{
		ID:        "w-wake",
		Queue:     q,
		Processor: noopProcessor{},
		Logger:    slog.New(slog.DiscardHandler),
		WakeCh:    wakeCh,
	})
	if err != nil {
		t.Fatalf("NewWorker: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := w.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer stopCancel()
		if sErr := w.Stop(stopCtx); sErr != nil && !errors.Is(sErr, context.DeadlineExceeded) {
			t.Logf("worker stop: %v", sErr)
		}
	})

	// Let the worker settle into its poll loop (first Claim happens
	// immediately on loop entry).
	deadline := time.Now().Add(2 * time.Second)
	for q.claimCount() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if q.claimCount() == 0 {
		t.Fatal("worker never claimed")
	}

	// Arm the queue, then send the wake signal. Idle backoff may have
	// grown well past 1s, so without the wake channel this test would
	// time out waiting for the next poll.
	q.setArmed(true)
	wakeStart := time.Now()
	select {
	case wakeCh <- struct{}{}:
	default:
		t.Fatal("wake channel unexpectedly full")
	}

	claimDeadline := wakeStart.Add(50 * time.Millisecond)
	for {
		if q.claimCount() >= 2 {
			break
		}
		if time.Now().After(claimDeadline) {
			t.Fatalf("Claim did not run within 50ms of wake signal (claims=%d)", q.claimCount())
		}
		time.Sleep(1 * time.Millisecond)
	}
	if latency := q.lastClaimTime().Sub(wakeStart); latency > 50*time.Millisecond {
		t.Fatalf("claim latency after wake = %v, want <= 50ms", latency)
	}
}

// TestWorker_NilWakeChIsLegacy: a nil WakeCh must not panic or change
// behavior — the select's nil-channel case is never ready, so the worker
// is a pure timer poller exactly as before.
func TestWorker_NilWakeChIsLegacy(t *testing.T) {
	q := &wakeFakeQueue{}
	w, err := NewWorker(Config{
		ID:        "w-nil",
		Queue:     q,
		Processor: noopProcessor{},
		Logger:    slog.New(slog.DiscardHandler),
		// WakeCh omitted: nil.
	})
	if err != nil {
		t.Fatalf("NewWorker: %v", err)
	}
	if w.wakeCh != nil {
		t.Fatal("WakeCh should be nil when omitted from Config")
	}

	ctx, cancel := context.WithCancel(context.Background())
	if err := w.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	// Give the loop a couple of poll cycles.
	time.Sleep(50 * time.Millisecond)
	cancel()
	stopCtx, stopCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer stopCancel()
	if err := w.Stop(stopCtx); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if q.claimCount() == 0 {
		t.Fatal("legacy (nil WakeCh) worker never polled")
	}
}
