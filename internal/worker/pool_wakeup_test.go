package worker

// Pool wake-wiring tests: AddWorker registers a wake channel with a
// WakeNotifier queue, RemoveWorker/Stop unregister it (no leak), and a
// queue WITHOUT WakeNotifier still constructs workers fine (nil channel =
// legacy poll-only). See docs/plans/20261004-agent-routing/01-worker-wakeup.md.

import (
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"
)

// notifierFakeQueue is wakeFakeQueue plus WakeNotifier, exposing its
// registered-waiter count for lifecycle assertions.
type notifierFakeQueue struct {
	wakeFakeQueue

	mu        sync.Mutex
	wakeChans map[chan<- struct{}]struct{}
}

func (q *notifierFakeQueue) WakeWaiter(ch chan<- struct{}) (unregister func()) {
	q.mu.Lock()
	q.wakeChans[ch] = struct{}{}
	q.mu.Unlock()
	return func() {
		q.mu.Lock()
		delete(q.wakeChans, ch)
		q.mu.Unlock()
	}
}

func (q *notifierFakeQueue) waiterCount() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.wakeChans)
}

// TestPool_AddWorkerRegistersWake: pool over a WakeNotifier queue wires
// each worker with a registered channel; RemoveWorker unregisters it.
func TestPool_AddWorkerRegistersWake(t *testing.T) {
	q := &notifierFakeQueue{wakeChans: make(map[chan<- struct{}]struct{})}
	p, err := NewPool(PoolConfig{
		Queue:     q,
		Processor: noopProcessor{},
		Logger:    slog.New(slog.DiscardHandler),
	})
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}

	w, err := p.AddWorker(nil, "")
	if err != nil {
		t.Fatalf("AddWorker: %v", err)
	}
	if n := q.waiterCount(); n != 1 {
		t.Fatalf("waiter count after AddWorker = %d, want 1", n)
	}
	if w.wakeCh == nil {
		t.Fatal("AddWorker did not wire a wake channel into the worker")
	}

	if err := p.RemoveWorker(w.ID); err != nil {
		t.Fatalf("RemoveWorker: %v", err)
	}
	if n := q.waiterCount(); n != 0 {
		t.Fatalf("waiter count after RemoveWorker = %d, want 0 (leak)", n)
	}
}

// TestPool_StopUnregistersWake: Stop's cleanup unregisters every worker's
// wake channel.
func TestPool_StopUnregistersWake(t *testing.T) {
	q := &notifierFakeQueue{wakeChans: make(map[chan<- struct{}]struct{})}
	p, err := NewPool(PoolConfig{
		Queue:     q,
		Processor: noopProcessor{},
		Logger:    slog.New(slog.DiscardHandler),
	})
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}

	for i := 0; i < 3; i++ {
		if _, err := p.AddWorker(nil, ""); err != nil {
			t.Fatalf("AddWorker %d: %v", i, err)
		}
	}
	if n := q.waiterCount(); n != 3 {
		t.Fatalf("waiter count after 3 AddWorker = %d, want 3", n)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := p.Stop(ctx); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if n := q.waiterCount(); n != 0 {
		t.Fatalf("waiter count after Stop = %d, want 0 (leak)", n)
	}
}

// TestPool_NonNotifierQueueStillWorks: a queue NOT implementing
// WakeNotifier must construct workers with a nil wake channel (legacy
// poll-only behavior).
func TestPool_NonNotifierQueueStillWorks(t *testing.T) {
	q := &wakeFakeQueue{}
	p, err := NewPool(PoolConfig{
		Queue:     q,
		Processor: noopProcessor{},
		Logger:    slog.New(slog.DiscardHandler),
	})
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}

	w, err := p.AddWorker(nil, "")
	if err != nil {
		t.Fatalf("AddWorker: %v", err)
	}
	if w.wakeCh != nil {
		t.Fatal("worker wake channel should be nil for non-WakeNotifier queue")
	}
}
