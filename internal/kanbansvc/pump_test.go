package kanbansvc

// Tests for the daemon→sidecar board-event pump (the GUI live feed).
// Pattern follows gateway_test.go's fakeRPCCaller: the daemon side of
// bus.subscribe / bus.poll / bus.unsubscribe is scripted in-process.

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeBus is a scripted daemon bus: records every RPC, answers
// bus.subscribe with a subscription id, replays queued events on
// bus.poll with the monotonic seq cursor contract of
// internal/rpc/proxy.go handleBusPoll (events with Seq > since_seq,
// next_seq = high-water mark).
type fakeBus struct {
	mu sync.Mutex

	calls   []string         // method names in call order
	subReqs []map[string]any // decoded bus.subscribe params
	pollSeq []int64          // since_seq of each poll

	nextSeq int64
	queue   []pumpRecord // events the next poll hands out

	subID       string
	failNextSub error // scripted subscribe failure (retry test)
	subCount    int   // successful subscribe count
	forgetSubAt int   // after this many polls answer "subscription not found" (0 = never)
	pollCount   int
}

func newFakeBus() *fakeBus {
	return &fakeBus{subID: "sub-test-1"}
}

func (f *fakeBus) Call(method string, params any) (json.RawMessage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, method)

	switch method {
	case "bus.subscribe":
		if f.failNextSub != nil {
			err := f.failNextSub
			f.failNextSub = nil
			return nil, err
		}
		var req map[string]any
		raw, _ := json.Marshal(params)
		_ = json.Unmarshal(raw, &req) //nolint:mutexio // test fake: in-memory JSON of caller-owned params
		f.subReqs = append(f.subReqs, req)
		f.subCount++
		out, _ := json.Marshal(map[string]any{"subscription_id": f.subID, "topics": req["topics"]})
		return out, nil

	case "bus.poll":
		f.pollCount++
		if f.forgetSubAt > 0 && f.pollCount > f.forgetSubAt {
			return nil, errors.New("subscription not found: " + f.subID)
		}
		var req struct {
			SubscriptionID string `json:"subscription_id"`
			SinceSeq       int64  `json:"since_seq"`
		}
		raw, _ := json.Marshal(params)
		_ = json.Unmarshal(raw, &req) //nolint:mutexio // test fake: in-memory JSON of caller-owned params
		f.pollSeq = append(f.pollSeq, req.SinceSeq)

		deliver := f.queue
		f.queue = nil
		out, _ := json.Marshal(map[string]any{
			"events":   deliver,
			"next_seq": f.nextSeq,
			"dropped":  0,
		})
		return out, nil

	case "bus.unsubscribe":
		out, _ := json.Marshal(map[string]any{"status": "unsubscribed"})
		return out, nil
	}
	return nil, errors.New("method not found: " + method)
}

func (f *fakeBus) Connect() error { return nil }
func (f *fakeBus) Close() error   { return nil }

// enqueue queues one bus event with the daemon's seq semantics.
func (f *fakeBus) enqueue(topic string, payload map[string]any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextSeq++
	raw, _ := json.Marshal(payload)
	f.queue = append(f.queue, pumpRecord{
		Topic:     topic,
		Type:      "message",
		Source:    "test",
		Timestamp: "2026-11-16T00:00:00Z",
		Payload:   raw,
		Seq:       f.nextSeq,
	})
}

func (f *fakeBus) subscribeTopics(t *testing.T) []string {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.subReqs) == 0 {
		t.Fatal("pump never called bus.subscribe")
	}
	raw, _ := json.Marshal(f.subReqs[0]["topics"])
	var topics []string
	_ = json.Unmarshal(raw, &topics) //nolint:mutexio // test fake: in-memory JSON of fixture data
	return topics
}

func (f *fakeBus) callCount(method string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.calls {
		if c == method {
			n++
		}
	}
	return n
}

// fastPump builds a pump with a tight poll loop for tests.
func fastPump(t *testing.T, bus *fakeBus) *BusPump {
	t.Helper()
	p, err := NewBusPump(bus, PumpOptions{PollInterval: 10 * time.Millisecond, RetryBackoff: 30 * time.Millisecond})
	if err != nil {
		t.Fatalf("NewBusPump: %v", err)
	}
	t.Cleanup(func() { _ = p.Close() })
	return p
}

func waitFor(t *testing.T, cond func() bool, msg string) {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for !cond() {
		select {
		case <-deadline:
			t.Fatal(msg)
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}
}

// TestPump_SubscribesTheSevenBoardTopics pins the subscribed topics set:
// the exact seven kanban topics, concrete (NOT wildcard "kanban.*",
// which cannot match the 4-segment kanban.event.*.state topics).
func TestPump_SubscribesTheSevenBoardTopics(t *testing.T) {
	bus := newFakeBus()
	p := fastPump(t, bus)

	ctx := t.Context()
	if _, err := p.Subscribe(ctx); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	waitFor(t, func() bool { return bus.callCount("bus.subscribe") > 0 }, "pump never completed bus.subscribe")

	got := bus.subscribeTopics(t)
	want := []string{
		"kanban.card.moved", "kanban.board.changed",
		"kanban.event.task.state", "kanban.event.job.state",
		"kanban.event.plan.state", "kanban.event.step.state",
		"kanban.event.goal.state",
	}
	if len(got) != len(want) {
		t.Fatalf("topics = %v, want the seven concrete board topics", got)
	}
	asMap := make(map[string]bool, len(got))
	for _, topic := range got {
		asMap[topic] = true
	}
	for _, topic := range want {
		if !asMap[topic] {
			t.Fatalf("topics %v missing %q", got, topic)
		}
	}
	for _, topic := range got {
		if strings.Contains(topic, "*") {
			t.Fatalf("wildcard topic %q cannot match kanban.event.*.state (4 segments)", topic)
		}
	}
}

// TestPump_DeliversFramesAndAdvancesCursor pins the relay path: an event
// queued on the fake daemon reaches the EventSource consumer as a
// board_event envelope, and the poll cursor advances via since_seq.
func TestPump_DeliversFramesAndAdvancesCursor(t *testing.T) {
	bus := newFakeBus()
	p := fastPump(t, bus)

	ctx := t.Context()
	stream, err := p.Subscribe(ctx)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	waitFor(t, func() bool { return bus.callCount("bus.poll") > 0 }, "pump never polled")
	bus.enqueue("kanban.card.moved", map[string]any{"card_id": "c1", "to_column_id": "done"})

	select {
	case frame := <-stream:
		var envelope struct {
			Type string         `json:"type"`
			Data map[string]any `json:"data"`
		}
		if err := json.Unmarshal(frame, &envelope); err != nil {
			t.Fatalf("frame is not JSON: %v", err)
		}
		if envelope.Type != "board_event" {
			t.Fatalf("frame type = %q, want board_event (the daemon WS envelope the Flutter client decodes)", envelope.Type)
		}
		if envelope.Data["card_id"] != "c1" || envelope.Data["source_topic"] != "kanban.card.moved" {
			t.Fatalf("frame data = %v, want the payload plus source_topic", envelope.Data)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no frame delivered to the subscriber")
	}

	// Cursor: a later poll must carry since_seq >= the delivered seq.
	waitFor(t, func() bool {
		bus.mu.Lock()
		defer bus.mu.Unlock()
		for _, s := range bus.pollSeq {
			if s >= 1 {
				return true
			}
		}
		return false
	}, "poll cursor never advanced past 0")
}

// TestPump_ReconnectsWhenTheDaemonForgetsTheSubscription pins the
// bounded-retry path: a "subscription not found" poll answer re-runs
// bus.subscribe and pumping resumes.
func TestPump_ReconnectsWhenTheDaemonForgetsTheSubscription(t *testing.T) {
	bus := newFakeBus()
	bus.forgetSubAt = 2 // polls 3+ answer "subscription not found"
	p := fastPump(t, bus)

	ctx := t.Context()
	if _, err := p.Subscribe(ctx); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	waitFor(t, func() bool { return bus.callCount("bus.subscribe") >= 2 }, "pump never re-subscribed after the daemon forgot the subscription")

	// After re-subscription, events flow again on the new subscription.
	bus.enqueue("kanban.board.changed", map[string]any{"board_id": "b1"})
	// No consumer attached: just assert the pump survived and kept
	// polling (a dead pump would stop both subscribes and polls).
	waitFor(t, func() bool { return bus.callCount("bus.poll") > 3 }, "pump stopped polling after reconnect")
}

// TestPump_ToleratesTransientSubscribeFailure pins bounded retry: the
// first bus.subscribe errors, the pump keeps trying, and it settles once
// the daemon answers.
func TestPump_ToleratesTransientSubscribeFailure(t *testing.T) {
	bus := newFakeBus()
	bus.failNextSub = errors.New("connection refused")
	p := fastPump(t, bus)

	ctx := t.Context()
	if _, err := p.Subscribe(ctx); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	waitFor(t, func() bool {
		return bus.callCount("bus.subscribe") >= 2 // attempt 1 failed, attempt 2 succeeded
	}, "pump never retried the transient subscribe failure")
}

// TestPump_CloseIsIdempotentAndStopsTheGoroutine pins teardown: Close
// twice (second a no-op), the pump goroutine exits (daemon stops
// polling), and subscriber channels close rather than hang.
func TestPump_CloseIsIdempotentAndStopsTheGoroutine(t *testing.T) {
	bus := newFakeBus()
	p := fastPump(t, bus)

	ctx := t.Context()
	stream, err := p.Subscribe(ctx)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	waitFor(t, func() bool { return bus.callCount("bus.poll") > 0 }, "pump never started polling")

	if err := p.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := p.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}

	// Goroutine exited: no bus calls after Close (plus a grace tick).
	time.Sleep(50 * time.Millisecond)
	before := bus.callCount("bus.poll")
	time.Sleep(100 * time.Millisecond)
	if after := bus.callCount("bus.poll"); after != before {
		t.Fatalf("pump kept polling after Close (%d -> %d): orphan goroutine", before, after)
	}

	// Subscriber channel closed, not hung.
	select {
	case frame, ok := <-stream:
		if ok {
			t.Fatalf("unexpected frame after Close: %s", frame)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("subscriber channel still open after Close")
	}

	// best-effort bus.unsubscribe happened.
	if bus.callCount("bus.unsubscribe") != 1 {
		t.Fatalf("bus.unsubscribe calls = %d, want 1", bus.callCount("bus.unsubscribe"))
	}
}

// TestPump_SubscriberCtxEndOnlyDetachesThatSubscriber pins per-relay
// lifecycle: one relay's ctx ending closes its channel while the pump
// keeps serving another.
func TestPump_SubscriberCtxEndOnlyDetachesThatSubscriber(t *testing.T) {
	bus := newFakeBus()
	p := fastPump(t, bus)

	ctx1, cancel1 := context.WithCancel(context.Background())
	stream1, err := p.Subscribe(ctx1)
	if err != nil {
		t.Fatalf("Subscribe 1: %v", err)
	}
	ctx2 := context.Background()
	stream2, err := p.Subscribe(ctx2)
	if err != nil {
		t.Fatalf("Subscribe 2: %v", err)
	}

	waitFor(t, func() bool { return bus.callCount("bus.poll") > 0 }, "pump never started polling")
	cancel1()

	select {
	case _, ok := <-stream1:
		if ok {
			// One in-flight frame is acceptable; keep draining until closed.
			for range stream1 {
			}
		}
	case <-time.After(2 * time.Second):
		t.Fatal("stream1 never closed after its ctx ended")
	}

	bus.enqueue("kanban.card.moved", map[string]any{"card_id": "c2"})
	select {
	case <-stream2:
	case <-time.After(2 * time.Second):
		t.Fatal("stream2 starved after stream1 detached")
	}
}

// TestPump_NilRPCCallerRejected pins the constructor's required dep.
func TestPump_NilRPCCallerRejected(t *testing.T) {
	if _, err := NewBusPump(nil, PumpOptions{}); err == nil {
		t.Fatal("NewBusPump(nil) accepted a nil caller")
	}
}
