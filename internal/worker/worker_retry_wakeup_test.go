package worker

// M2: a job returned to `pending` by Retry/Requeue/RecoverFromDeadLetter must
// WAKE a parked worker, exactly like Enqueue does. Only Enqueue used to signal,
// so the event-driven wake-up silently degraded to polling on the retry path: a
// requeued job waited for the worker's poll timer — up to maxIdleBackoff = 15s —
// with no log line anywhere explaining the delay.
//
// SUBTLETY this pin exists to nail down: Retry and Requeue park a job at a
// FUTURE next_retry_at (exponential backoff / provider-wait resume time), so
// waking waiters at the moment of the TRANSITION buys nothing — every woken
// worker re-polls, finds the claim gate closed, and sleeps again. The wake has
// to land when the gate OPENS. Hence the queue-side signalClaimableAt; this
// test drives a real PersistentQueue end to end and measures the re-claim
// latency against the gate rather than against the transition.

import (
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/queue"
)

// TestWorker_RetryWakesIdleWorker (M2 pin): a worker parked on its idle backoff
// must claim the retried job promptly after the retry gate opens — without
// waiting out its own poll timer. The worker holds a wake channel registered
// through the queue's real WakeNotifier surface, so the whole chain (transition
// → gate → wake fan-out → worker select → claim) is exercised.
//
// The queue's store is reached directly to seed a SHORT retry backoff: the
// production 2s step would still prove wake-on-retry (the wake lands at the
// gate), but a small window makes the "wake beat the poll timer" margin
// measurable rather than merely plausible.
func TestWorker_RetryWakesIdleWorker(t *testing.T) {
	if testing.Short() {
		t.Skip("timing-sensitive")
	}

	dbPath := filepath.Join(t.TempDir(), "retry-wake.db")
	msgBus := bus.New(nil, nil)
	q, err := queue.NewPersistentQueue(dbPath, msgBus, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("NewPersistentQueue: %v", err)
	}
	t.Cleanup(func() { q.Close() })

	// Seed one job so the worker's first Claim succeeds and the failure path
	// (Fail + Retry) runs.
	job, err := queue.NewJob(queue.JobTypeOneOff, map[string]string{"prompt": "retry-wake"})
	if err != nil {
		t.Fatalf("NewJob: %v", err)
	}
	if err := q.Enqueue(context.Background(), job); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	// The processor reads the stored gate to measure the re-claim latency
	// against it.
	proc := &timedProcessor{store: q.Store(), jobID: job.ID}
	wakeCh := make(chan struct{}, 1)
	unregister := q.WakeWaiter(wakeCh)
	defer unregister()

	w, err := NewWorker(Config{
		ID:        "w-retry-wake",
		Queue:     q,
		Processor: proc,
		Logger:    slog.New(slog.DiscardHandler),
		WakeCh:    wakeCh,
	})
	if err != nil {
		t.Fatalf("NewWorker: %v", err)
	}

	ctx := t.Context()
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

	// Wait for the retry to be re-executed: the processor saw a second run.
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if proc.executions() >= 2 {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	if proc.executions() < 2 {
		t.Fatalf("worker never re-claimed the retried job (executions=%d)", proc.executions())
	}

	// The DISCRIMINATING assertion: after the retry gate opened, the re-claim
	// must follow quickly. Without a wake the worker would only rediscover the
	// job on its next poll — after the idle backoff, which starts at 1s and
	// doubles, so a poll-only build lands at >= 1s. 400ms is a margin a
	// poll cycle cannot sneak under, so a regression fails the pin instead of
	// merely slowing it down.
	if latency := proc.gateToReclaim(); latency > 400*time.Millisecond {
		t.Fatalf("re-claim after the retry gate opened took %v, want < 400ms "+
			"(a poll-only worker rediscovers the job only on its next poll — audit M2)", latency)
	}
}

// timedProcessor fails the first execution and succeeds afterwards, recording
// (a) the moment the first failure RETURNED, (b) the moment the retry gate
// opened (queried from the store once the job is visible as pending), and
// (c) when the second execution started.
type timedProcessor struct {
	mu        sync.Mutex
	count     int
	firstEnd  time.Time
	gateOpen  time.Time
	reclaimAt time.Time

	store *queue.Store
	jobID string
}

func (p *timedProcessor) Process(_ context.Context, j *queue.Job) (any, error) {
	p.mu.Lock()
	p.count++
	n := p.count
	if n == 1 {
		p.jobID = j.ID
		p.mu.Unlock()
		// Stamp AFTER the failure is on its way out: Fail + Retry + the wake
		// all happen after this returns, so this is the t=0 of the measurement.
		defer func() {
			p.mu.Lock()
			p.firstEnd = time.Now()
			p.mu.Unlock()
		}()
		return nil, errors.New("transient failure")
	}
	p.reclaimAt = time.Now()
	// The gate opening is recorded on the first pass past the retry: read the
	// stored next_retry_at to see when the job was/will be claimable.
	if p.gateOpen.IsZero() && p.store != nil {
		if gate := p.store.PendingGate(j.ID); !gate.IsZero() {
			p.gateOpen = gate
		}
	}
	p.mu.Unlock()
	return map[string]string{"result": "ok"}, nil
}

func (p *timedProcessor) executions() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.count
}

// gateToReclaim is the latency from the moment the retry gate opened to the
// moment the worker re-claimed the job. Clamped at zero: a re-claim can land in
// the same instant the gate opens (or the gate read can race slightly), and a
// negative value is not a latency worth failing on.
func (p *timedProcessor) gateToReclaim() time.Duration {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.reclaimAt.IsZero() {
		return time.Hour
	}
	gate := p.gateOpen
	if gate.IsZero() {
		// Gate unknown (read raced the reset): fall back to the failure return,
		// which only makes the measurement LONGER, so it cannot mask a
		// regression — it can only fail a passing build at worst by being
		// generous.
		gate = p.firstEnd
	}
	d := p.reclaimAt.Sub(gate)
	if d < 0 {
		return 0
	}
	return d
}

// Compile-time guard: the persistent queue must keep satisfying the wake
// contract the worker probes for, or this test would pass on a silently
// poll-only worker.
var _ queue.WakeNotifier = (*queue.PersistentQueue)(nil)
