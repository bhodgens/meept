package cluster

import (
	"encoding/json"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/pkg/models"
)

// Pins for retryLoop's MaxRetryAttempts contract (the in-code NOTE said the
// config was silently not honored — d53a4956 landed the attempt tracking, so
// these tests lock the behavior so it cannot regress back to unbounded
// re-publishing):
//
//	MaxRetryAttempts = 2 → an event queued repeatedly is re-published at
//	most 2 times, then dropped (Warn + no further Publish).
//	MaxRetryAttempts <= 0 → backward-compatible default of 3.
//
// The retryQueue's cap is 64, so the test queues a bounded flood and counts
// re-broadcasts by subscribing to the engine's own "cluster.event.broadcast"
// bus topic (retryLoop re-publishes via Publish, which publishes there).
type broadcastCounter struct {
	mu     sync.Mutex
	byID   map[string]int
	stopCh chan struct{}
	doneCh chan struct{}
}

func newBroadcastCounter(b *bus.MessageBus) *broadcastCounter {
	c := &broadcastCounter{
		byID:   make(map[string]int),
		stopCh: make(chan struct{}),
		doneCh: make(chan struct{}),
	}
	sub := b.Subscribe("test-counter", "cluster.event.broadcast")
	go func() {
		defer close(c.doneCh)
		defer b.Unsubscribe(sub)
		for {
			select {
			case <-c.stopCh:
				return
			case msg, ok := <-sub.Channel:
				if !ok {
					return
				}
				var payload struct {
					Event json.RawMessage `json:"event"`
				}
				if err := json.Unmarshal(msg.Payload, &payload); err != nil {
					continue
				}
				var ev models.ClusterEvent
				if err := json.Unmarshal(payload.Event, &ev); err != nil {
					continue
				}
				c.mu.Lock()
				c.byID[ev.EventID]++
				c.mu.Unlock()
			}
		}
	}()
	return c
}

func (c *broadcastCounter) count(id string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.byID[id]
}

func (c *broadcastCounter) stop() {
	close(c.stopCh)
	<-c.doneCh
}

func retryLoopTestEngine(t *testing.T, maxRetry int) (*GossipEngine, *bus.MessageBus) {
	t.Helper()
	b := bus.New(nil, discardLogger())
	cfg := &Config{
		NodeID: "node-retry",
		Gossip: GossipConfig{
			HeartbeatInterval: time.Hour,
			PeerTimeout:       time.Hour,
			EventRetention:    time.Hour,
			MaxRetryAttempts:  maxRetry,
		},
		Security: SecurityConfig{RequireNodeSignatures: false},
	}
	return NewGossipEngine(cfg, "node-retry", b, discardLogger()), b
}

// discardLogger silences engine logs in tests.
func discardLogger() *slog.Logger { return slog.New(slog.DiscardHandler) }

// TestRetryLoop_MaxRetryAttemptsBoundsRepublish drives the REAL retryLoop
// goroutine: an event whose deliveries keep failing is queued again after
// every Publish (mirroring gossip_transport.sendToPeer), so without the
// attempt bound the counter would grow with every tick. With
// MaxRetryAttempts=2 the re-broadcast count must stop at exactly 2.
func TestRetryLoop_MaxRetryAttemptsBoundsRepublish(t *testing.T) {
	engine, b := retryLoopTestEngine(t, 2)
	counter := newBroadcastCounter(b)
	defer counter.stop()

	ctx := t.Context()
	if err := engine.Start(ctx); err != nil {
		t.Fatalf("engine start: %v", err)
	}
	defer func() { _ = engine.Stop() }()

	const eventID = "evt-retry-bound"
	seed := &models.ClusterEvent{EventID: eventID, EventType: models.EventNodeHeartbeat, NodeID: "node-retry"}
	engine.QueueForRetry(seed)

	// Re-queue the event after each re-broadcast, the way a failed
	// transport send would, until the loop should have dropped it.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if counter.count(eventID) >= 2 {
			break
		}
		engine.QueueForRetry(&models.ClusterEvent{EventID: eventID, EventType: models.EventNodeHeartbeat, NodeID: "node-retry"})
		time.Sleep(50 * time.Millisecond)
	}

	// The bound: at most 2 re-publishes, ever. Give the loop one grace
	// tick to overshoot if the cap were broken, then assert.
	time.Sleep(6 * time.Second)
	if got := counter.count(eventID); got > 2 {
		t.Fatalf("event re-published %d times; MaxRetryAttempts=2 must cap re-publishing at 2", got)
	}
	if got := counter.count(eventID); got == 0 {
		t.Fatal("event was never re-published; retry loop did not run")
	}
}

// TestRetryLoop_ZeroKeepsDefaultBound pins the backward-compatible branch:
// MaxRetryAttempts 0 falls back to the default of 3 (bounded, not unbounded).
func TestRetryLoop_ZeroKeepsDefaultBound(t *testing.T) {
	engine, b := retryLoopTestEngine(t, 0)
	counter := newBroadcastCounter(b)
	defer counter.stop()

	ctx := t.Context()
	if err := engine.Start(ctx); err != nil {
		t.Fatalf("engine start: %v", err)
	}
	defer func() { _ = engine.Stop() }()

	const eventID = "evt-retry-zero"
	engine.QueueForRetry(&models.ClusterEvent{EventID: eventID, EventType: models.EventNodeHeartbeat, NodeID: "node-retry"})

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if counter.count(eventID) >= 3 {
			break
		}
		engine.QueueForRetry(&models.ClusterEvent{EventID: eventID, EventType: models.EventNodeHeartbeat, NodeID: "node-retry"})
		time.Sleep(50 * time.Millisecond)
	}

	// The bound: at most 3 re-publishes (the default), ever. Give the
	// loop one grace tick to overshoot if the cap were broken.
	time.Sleep(6 * time.Second)
	if got := counter.count(eventID); got > 3 {
		t.Fatalf("event re-published %d times; MaxRetryAttempts=0 must fall back to the default bound of 3", got)
	}
}
