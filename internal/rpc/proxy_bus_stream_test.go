package rpc

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/pkg/models"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// busStreamFixture wires a ProxyHandler to a live MessageBus the way the
// daemon does, and drives bus.subscribe / bus.poll through their handlers
// directly (same handler functions the RPC server registers).
type busStreamFixture struct {
	t    *testing.T
	bus  *bus.MessageBus
	px   *ProxyHandler
}

func newBusStreamFixture(t *testing.T) *busStreamFixture {
	t.Helper()
	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	mb := bus.New(nil, quiet)
	return &busStreamFixture{t: t, bus: mb, px: NewProxyHandler(mb)}
}

func (f *busStreamFixture) subscribe(topics ...string) string {
	f.t.Helper()
	params, err := json.Marshal(map[string]any{"topics": topics})
	if err != nil {
		f.t.Fatalf("marshal subscribe params: %v", err)
	}
	res, err := f.px.handleBusSubscribe(f.t.Context(), params)
	if err != nil {
		f.t.Fatalf("bus.subscribe: %v", err)
	}
	m, ok := res.(map[string]any)
	if !ok {
		f.t.Fatalf("bus.subscribe returned %T, want map", res)
	}
	subID, _ := m["subscription_id"].(string)
	if subID == "" {
		f.t.Fatalf("bus.subscribe returned no subscription_id: %v", res)
	}
	return subID
}

// pollResp is the decoded bus.poll response.
type pollResp struct {
	Events  []busEventRecord `json:"events"`
	NextSeq int64            `json:"next_seq"`
	Dropped int64            `json:"dropped"`
}

func (f *busStreamFixture) poll(subID string, sinceSeq int64) pollResp {
	f.t.Helper()
	params, err := json.Marshal(map[string]any{
		"subscription_id": subID,
		"since_seq":       sinceSeq,
	})
	if err != nil {
		f.t.Fatalf("marshal poll params: %v", err)
	}
	res, err := f.px.handleBusPoll(f.t.Context(), params)
	if err != nil {
		f.t.Fatalf("bus.poll: %v", err)
	}
	raw, merr := json.Marshal(res)
	if merr != nil {
		f.t.Fatalf("re-marshal poll response: %v", merr)
	}
	var out pollResp
	if err := json.Unmarshal(raw, &out); err != nil {
		f.t.Fatalf("decode poll response: %v", err)
	}
	return out
}

// publish sends an event on the topic and returns the delivered count.
func (f *busStreamFixture) publish(topic, body string) int {
	msg := &models.BusMessage{
		ID:      "evt-" + body,
		Type:    models.MessageTypeEvent,
		Topic:   topic,
		Source:  "bus-stream-test",
		Payload: json.RawMessage(fmt.Sprintf(`{"body":%q}`, body)),
	}
	return f.bus.Publish(topic, msg)
}

// waitBuffered waits until the subscription's buffer holds at least n events.
func (f *busStreamFixture) waitBuffered(subID string, n int) {
	f.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		resp := f.poll(subID, 0)
		if int64(len(resp.Events)) >= int64(n) {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	f.t.Fatalf("subscription %s never buffered %d events", subID, n)
}

// ---------------------------------------------------------------------------
// deterministic seq-cursor contract tests
// ---------------------------------------------------------------------------

// TestBusPollSeqCursorNeverSkips pins the core contract: with the
// since_seq cursor, events buffered before the client observed its cursor
// are returned by the NEXT poll — no wall-clock race can skip them.
func TestBusPollSeqCursorNeverSkips(t *testing.T) {
	f := newBusStreamFixture(t)
	subID := f.subscribe("t1")

	f.publish("t1", "e1")
	f.publish("t1", "e2")
	f.waitBuffered(subID, 2)

	resp := f.poll(subID, 0)
	if len(resp.Events) != 2 {
		t.Fatalf("first poll returned %d events, want 2: %+v", len(resp.Events), resp.Events)
	}
	if resp.NextSeq != 2 {
		t.Fatalf("next_seq = %d, want 2", resp.NextSeq)
	}
	if resp.Events[0].Seq != 1 || resp.Events[1].Seq != 2 {
		t.Fatalf("seqs = %d,%d, want 1,2 (monotonic)", resp.Events[0].Seq, resp.Events[1].Seq)
	}
	if resp.Events[0].Payload == nil {
		t.Fatal("event payload lost (payload must stay byte-compatible)")
	}

	// Events buffered AFTER the cursor was observed come back on the
	// next poll, and ONLY them.
	f.publish("t1", "e3")
	f.publish("t1", "e4")
	f.waitBuffered(subID, 4)

	resp = f.poll(subID, resp.NextSeq)
	if len(resp.Events) != 2 {
		t.Fatalf("second poll returned %d events, want exactly the 2 new: %+v", len(resp.Events), resp.Events)
	}
	if resp.Events[0].Seq != 3 || resp.Events[1].Seq != 4 {
		t.Fatalf("second-poll seqs = %d,%d, want 3,4", resp.Events[0].Seq, resp.Events[1].Seq)
	}

	// A cursor at the head returns an empty page — but next_seq and the
	// drop accounting are still present.
	resp = f.poll(subID, resp.NextSeq)
	if len(resp.Events) != 0 || resp.NextSeq != 4 {
		t.Fatalf("empty poll: events=%d next_seq=%d, want 0/4", len(resp.Events), resp.NextSeq)
	}
	if resp.Dropped != 0 {
		t.Fatalf("dropped = %d with no overflow, want 0", resp.Dropped)
	}
}

// TestBusPollLegacySinceStillWorks pins backward compatibility: old clients
// that send the RFC3339 `since` cursor keep working (the events array shape
// is unchanged; next_seq/dropped are additive keys).
func TestBusPollLegacySinceStillWorks(t *testing.T) {
	f := newBusStreamFixture(t)
	subID := f.subscribe("t2")

	f.publish("t2", "l1")
	f.waitBuffered(subID, 1)

	params, err := json.Marshal(map[string]any{"subscription_id": subID})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	res, err := f.px.handleBusPoll(t.Context(), params)
	if err != nil {
		t.Fatalf("poll without since: %v", err)
	}
	m := res.(map[string]any)
	events, ok := m["events"].([]*busEventRecord)
	if !ok || len(events) != 1 {
		t.Fatalf("legacy poll returned %#v, want 1 event", m["events"])
	}

	// since = zero time (unparseable / empty) returns everything; that is
	// the pre-existing contract and must not regress.
	legacy, err := json.Marshal(map[string]any{"subscription_id": subID, "since": "not-a-time"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	res, err = f.px.handleBusPoll(t.Context(), legacy)
	if err != nil {
		t.Fatalf("legacy since poll: %v", err)
	}
	events = res.(map[string]any)["events"].([]*busEventRecord)
	if len(events) != 1 {
		t.Fatalf("unparseable since must return all events, got %d", len(events))
	}
}

// TestBusPollDropAccounting pins the never-silent contract: when the
// bounded buffer overflows, the lost records are counted in the poll
// response's `dropped` field instead of vanishing.
func TestBusPollDropAccounting(t *testing.T) {
	f := newBusStreamFixture(t)
	subID := f.subscribe("t3")

	// MaxEvents is 100: burst 300 with the collector draining — the
	// buffer keeps the newest records, the rest are trimmed.
	const burst = 300
	for i := 0; i < burst; i++ {
		f.publish("t3", fmt.Sprintf("b%d", i))
	}

	// Settle: wait until the collection identity balances (the collector
	// may still be draining the channel backlog when the burst returns).
	// published = buffered + dropped(trim + bus-level channel drops) —
	// every published event is in exactly one bucket, never unaccounted.
	deadline := time.Now().Add(5 * time.Second)
	var resp pollResp
	for {
		resp = f.poll(subID, 0)
		if int64(len(resp.Events))+resp.Dropped == burst {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("collection never balanced: buffered=%d dropped=%d published=%d",
				len(resp.Events), resp.Dropped, burst)
		}
		time.Sleep(2 * time.Millisecond)
	}
	if resp.Dropped < 1 {
		t.Fatalf("dropped = %d after a 300-burst into a 100-cap buffer, want overflow counted (never silent)", resp.Dropped)
	}
	// NOTE: with bus-level channel drops the NEWEST events can be the lost
	// ones (never collected), so surviving seqs need not be >= dropped.
	// The binding contract is the collection identity asserted above:
	// buffered + dropped == published, never unaccounted.
}

// ---------------------------------------------------------------------------
// stress tests (the load shape that produced the observed defect)
// ---------------------------------------------------------------------------

// TestBusPollStressFastProducerSlowPoller is the reproducing stress test
// for the observed defect: a fast producer against a slow poller. Every
// published event must be ACCOUNTED: either delivered by a poll (exactly
// once, via the seq cursor) or explicitly counted in `dropped`. Before the
// seq-cursor fix, events were silently skipped (wall-clock cursor) and
// trimmed (unaccounted drop-oldest), so returned+dropped < published.
func TestBusPollStressFastProducerSlowPoller(t *testing.T) {
	f := newBusStreamFixture(t)
	subID := f.subscribe("stress")

	const total = 500
	produced := make(chan struct{})
	go func() {
		defer close(produced)
		for i := 0; i < total; i++ {
			f.publish("stress", fmt.Sprintf("s%d", i))
		}
	}()

	// Slow poller: cursor-based drain, 15ms cadence — far slower than the
	// producer, so the 100-cap buffer overflows repeatedly.
	cursor := int64(0)
	returned := make(map[int64]bool)
	deadlined := time.After(15 * time.Second)
producerLoop:
	for {
		select {
		case <-produced:
			break producerLoop
		case <-deadlined:
			t.Fatalf("stress drain did not settle; cursor=%d", cursor)
		default:
		}
		resp := f.poll(subID, cursor)
		for _, e := range resp.Events {
			if returned[e.Seq] {
				t.Fatalf("event seq %d delivered twice (cursor regression)", e.Seq)
			}
			returned[e.Seq] = true
		}
		if resp.NextSeq > cursor {
			cursor = resp.NextSeq
		}
		time.Sleep(15 * time.Millisecond)
	}
	// Settle: loop until the collection identity balances at the final
	// cursor (the collector may still be draining the channel backlog
	// when the producer returns). Then: every published event is in
	// exactly one bucket — delivered via polls or counted in `dropped`.
	deadline := time.Now().Add(10 * time.Second)
	var resp pollResp
	for {
		resp = f.poll(subID, cursor)
		seen := int64(len(returned)) + int64(len(resp.Events))
		if seen+resp.Dropped == total && cursor == resp.NextSeq {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("UNACCOUNTED EVENTS at settle: delivered=%d dropped=%d published=%d (silent drop)",
				len(returned)+len(resp.Events), resp.Dropped, total)
		}
		for _, e := range resp.Events {
			if returned[e.Seq] {
				t.Fatalf("event seq %d delivered twice (cursor regression)", e.Seq)
			}
			returned[e.Seq] = true
		}
		if resp.NextSeq > cursor {
			cursor = resp.NextSeq
		}
		time.Sleep(5 * time.Millisecond)
	}

	delivered := int64(len(returned))
	if delivered+resp.Dropped != total {
		t.Fatalf("UNACCOUNTED EVENTS: delivered=%d dropped=%d published=%d (silent drop)",
			delivered, resp.Dropped, total)
	}
	if delivered == 0 {
		t.Fatal("poll stream delivered nothing — the original defect shape")
	}
	t.Logf("stress: delivered=%d dropped=%d published=%d", delivered, resp.Dropped, total)
}

// TestBusPollStressPacedProducerZeroDrops drives the drop budget to zero:
// a producer the poller can keep up with must see EVERY event delivered
// exactly once and dropped == 0 (no silent loss anywhere on the path).
func TestBusPollStressPacedProducerZeroDrops(t *testing.T) {
	f := newBusStreamFixture(t)
	subID := f.subscribe("paced")

	const total = 100
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < total; i++ {
			f.publish("paced", fmt.Sprintf("p%d", i))
			time.Sleep(2 * time.Millisecond) // 2ms spacing: 100-cap buffer never overflows
		}
	}()

	cursor := int64(0)
	seen := 0
	var lastDropped int64
	deadlined := time.After(15 * time.Second)
	for {
		select {
		case <-done:
			// Settle: keep polling until every published event is
			// delivered (collector backlog drained). Then zero drops.
			settle := time.Now().Add(10 * time.Second)
			for {
				resp := f.poll(subID, cursor)
				seen += len(resp.Events)
				if resp.NextSeq > cursor {
					cursor = resp.NextSeq
				}
				lastDropped = resp.Dropped
				if seen == total || time.Now().After(settle) {
					break
				}
				time.Sleep(5 * time.Millisecond)
			}
			if seen != total {
				t.Fatalf("delivered %d of %d events (missing %d) with dropped=%d — skip/drop regression",
					seen, total, total-seen, lastDropped)
			}
			if lastDropped != 0 {
				t.Fatalf("dropped=%d under a paced producer, want ZERO (budget driven to zero)", lastDropped)
			}
			return
		case <-deadlined:
			t.Fatal("paced drain did not settle")
		default:
		}
		resp := f.poll(subID, cursor)
		seen += len(resp.Events)
		if resp.NextSeq > cursor {
			cursor = resp.NextSeq
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// ---------------------------------------------------------------------------
// bus-level drop counter
// ---------------------------------------------------------------------------

// TestSubscriberDropCounter pins the bus-side accounting: a subscriber whose
// channel is full counts each non-blocking-publish drop, and the counter is
// nil-safe.
func TestSubscriberDropCounter(t *testing.T) {
	mb := bus.New(&bus.Config{BufferSize: 1}, nil)
	sub := mb.Subscribe("dropctr", "dt")
	// Fill the channel (no reader).
	msg := &models.BusMessage{ID: "m1", Type: models.MessageTypeEvent, Topic: "dt"}
	if got := mb.Publish("dt", msg); got != 1 {
		t.Fatalf("first publish delivered=%d, want 1", got)
	}
	msg2 := &models.BusMessage{ID: "m2", Type: models.MessageTypeEvent, Topic: "dt"}
	if got := mb.Publish("dt", msg2); got != 0 {
		t.Fatalf("publish onto full channel delivered=%d, want 0", got)
	}
	if got := sub.DroppedCount(); got != 1 {
		t.Fatalf("DroppedCount=%d, want 1", got)
	}
	// Nil-safe.
	var nilSub *bus.Subscriber
	if got := nilSub.DroppedCount(); got != 0 {
		t.Fatalf("nil DroppedCount=%d, want 0", got)
	}
}
