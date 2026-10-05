package queue

// Wake channel tests: PersistentQueue.Enqueue signals registered waiters
// (non-blocking) so workers can claim immediately instead of polling.
// See docs/plans/20261004-agent-routing/01-worker-wakeup.md.

import (
	"context"
	"testing"
	"time"
)

func mustJob(t *testing.T) *Job {
	t.Helper()
	job, err := NewJob(JobTypeOneOff, map[string]string{"prompt": "wake"})
	if err != nil {
		t.Fatalf("NewJob: %v", err)
	}
	return job
}

// TestPersistentQueue_WakeWaiter: Enqueue signals a registered waiter;
// after unregister the waiter is not signaled again.
func TestPersistentQueue_WakeWaiter(t *testing.T) {
	q := newTestQueue(t)
	ctx := context.Background()

	ch := make(chan struct{}, 1)
	unregister := q.WakeWaiter(ch)

	if err := q.Enqueue(ctx, mustJob(t)); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	select {
	case <-ch:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Enqueue did not signal waiter")
	}

	unregister()
	// Drain so a post-unregister signal would be observable.
	select {
	case <-ch:
	default:
	}
	if err := q.Enqueue(ctx, mustJob(t)); err != nil {
		t.Fatalf("Enqueue after unregister: %v", err)
	}
	select {
	case <-ch:
		t.Fatal("waiter signaled after unregister")
	default:
	}
}

// TestPersistentQueue_WakeMultipleWaiters: every registered waiter gets
// the signal from a single Enqueue.
func TestPersistentQueue_WakeMultipleWaiters(t *testing.T) {
	q := newTestQueue(t)

	ch1 := make(chan struct{}, 1)
	ch2 := make(chan struct{}, 1)
	ur1 := q.WakeWaiter(ch1)
	ur2 := q.WakeWaiter(ch2)
	defer ur1()
	defer ur2()

	if err := q.Enqueue(context.Background(), mustJob(t)); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	for i, ch := range []chan struct{}{ch1, ch2} {
		select {
		case <-ch:
		case <-time.After(100 * time.Millisecond):
			t.Fatalf("waiter %d not signaled", i+1)
		}
	}
}

// TestPersistentQueue_WakeCloseClearsWaiters: Close clears the waiter set
// so no channels outlive the queue.
func TestPersistentQueue_WakeCloseClearsWaiters(t *testing.T) {
	q := newTestQueue(t)

	ch := make(chan struct{}, 1)
	unregister := q.WakeWaiter(ch)
	defer unregister()

	if err := q.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	q.wakeMu.Lock()
	n := len(q.wakeWaiters)
	q.wakeMu.Unlock()
	if n != 0 {
		t.Fatalf("wakeWaiters after Close = %d, want 0", n)
	}
}

// TestPersistentQueue_WakeFullChannelDrops: a buffered-full waiter channel
// must not block Enqueue (drop semantics — worker claims on next loop).
func TestPersistentQueue_WakeFullChannelDrops(t *testing.T) {
	q := newTestQueue(t)

	ch := make(chan struct{}, 1)
	ch <- struct{}{} // full
	unregister := q.WakeWaiter(ch)
	defer unregister()

	done := make(chan error, 1)
	go func() {
		done <- q.Enqueue(context.Background(), mustJob(t))
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Enqueue with full waiter channel: %v", err)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("Enqueue blocked on full waiter channel")
	}
}
