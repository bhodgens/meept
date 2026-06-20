package http

import (
	"log/slog"
	"sync"
	"testing"
)

// TestEventEmitter_SubscribeDuringClose_NoPanic is a regression test for a
// "send on closed channel" panic. Previously, Subscribe released the emitter
// lock before replaying buffered events to the freshly-created subscriber
// channel. Close() could run in that window, closing the channel, after
// which the replay's `select { case ch <- event: default: }` would panic.
//
// The fix replays while holding the emitter lock, so Close() cannot
// interleave.
func TestEventEmitter_SubscribeDuringClose_NoPanic(t *testing.T) {
	const iterations = 200
	for i := 0; i < iterations; i++ {
		e := NewEventEmitter(64, slog.Default())
		// Populate the buffer so Subscribe has events to replay.
		for j := 0; j < 8; j++ {
			e.Publish(&NotificationEvent{
				ID:        "evt",
				Timestamp: "2026-01-01T00:00:00Z",
				Type:      NotificationTypeInfo,
				Title:     "test",
				Message:   "x",
			})
		}

		var wg sync.WaitGroup
		wg.Add(2)

		// Subscriber goroutine: races Subscribe against Close.
		var subPanic any
		go func() {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					subPanic = r
				}
			}()
			ch := e.Subscribe()
			// Drain to allow the goroutine to exit; the channel may be
			// closed by Close() which is expected and fine.
			for range ch {
			}
		}()

		// Closer goroutine.
		go func() {
			defer wg.Done()
			e.Close()
		}()

		wg.Wait()
		if subPanic != nil {
			t.Fatalf("iteration %d: Subscribe panicked: %v", i, subPanic)
		}
	}
}

// TestEventEmitter_SubscribeAfterClose_ReturnsClosedChannel verifies that a
// Subscribe call that loses the race entirely (Close already finished)
// returns a closed channel rather than panicking when the caller reads.
func TestEventEmitter_SubscribeAfterClose_ReturnsClosedChannel(t *testing.T) {
	e := NewEventEmitter(8, slog.Default())
	e.Close()

	ch := e.Subscribe()
	_, ok := <-ch
	if ok {
		t.Errorf("expected channel from Subscribe-after-Close to be closed")
	}
}
