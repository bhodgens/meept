package sms

// 03-inbound-store/00T §T2 — bus topic gate (test-only file).
//
// Executable spec for leaf 03/01 (internal/sms/topics.go), frozen by
// master.md contract §5 (docs/plans/20261110-telnyx-texting/master.md):
//
//	type InboundMessage struct{ EventID, From, To, Text string; At time.Time }
//	var InboundTopic = bus.NewTopic[InboundMessage]("sms.inbound.received")
//	type StateChanged struct{ ProviderMessageID, Status, Err string }
//	var StateTopic = bus.NewTopic[StateChanged]("sms.state.changed")
//
// Topic NAMES are wire contract (the dispatch tree and #132 consume them);
// pin them exactly. Payloads must round-trip through the typed bus
// (PublishT/SubscribeT, internal/bus/topic.go) — the round-trip pattern
// mirrors internal/bus/topic_test.go. Raw publishes to the same string
// names must decode into the typed payloads (interop rule in topic.go's
// NewTopic doc), because daemon arming publishes via PublishT while
// observability tooling may republish raw JSON.

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/pkg/models"
)

// TestInboundTopicName pins the frozen topic string.
func TestInboundTopicName(t *testing.T) {
	if InboundTopic.Name != "sms.inbound.received" {
		t.Fatalf("InboundTopic.Name = %q; want %q", InboundTopic.Name, "sms.inbound.received")
	}
}

// TestStateTopicName pins the frozen topic string.
func TestStateTopicName(t *testing.T) {
	if StateTopic.Name != "sms.state.changed" {
		t.Fatalf("StateTopic.Name = %q; want %q", StateTopic.Name, "sms.state.changed")
	}
}

// TestInboundTopicRoundTrip pins PublishT/SubscribeT decode of the full
// InboundMessage payload, including the time field (JSON round-trip must
// preserve the instant to sub-second precision).
func TestInboundTopicRoundTrip(t *testing.T) {
	b := bus.New(nil, nil)
	defer b.Close()

	at := time.Now().UTC().Truncate(time.Millisecond)
	want := InboundMessage{
		EventID: "evt-topic-1",
		From:    "+15550000101",
		To:      "+15550009999",
		Text:    "are you there",
		At:      at,
	}
	got := make(chan InboundMessage, 1)
	sub := bus.SubscribeT(b, "inbound-rt", InboundTopic, func(p InboundMessage) { got <- p })
	defer b.Unsubscribe(sub)

	if n := bus.PublishT(b, InboundTopic, "test", want); n != 1 {
		t.Fatalf("PublishT delivery count = %d; want 1 (subscriber attached)", n)
	}
	select {
	case p := <-got:
		if p.EventID != want.EventID || p.From != want.From || p.To != want.To || p.Text != want.Text {
			t.Fatalf("decoded = %+v; want %+v", p, want)
		}
		if !p.At.Equal(at) {
			t.Fatalf("At round-trip = %v; want %v", p.At, at)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for typed InboundMessage delivery")
	}
}

// TestStateTopicRoundTrip pins PublishT/SubscribeT decode of StateChanged
// (terminal DLR fan-out: provider message id + status + error text).
func TestStateTopicRoundTrip(t *testing.T) {
	b := bus.New(nil, nil)
	defer b.Close()

	want := StateChanged{
		ProviderMessageID: "msg-topic-1",
		Status:            "delivered",
		Err:               "",
	}
	got := make(chan StateChanged, 1)
	sub := bus.SubscribeT(b, "state-rt", StateTopic, func(p StateChanged) { got <- p })
	defer b.Unsubscribe(sub)

	bus.PublishT(b, StateTopic, "test", want)
	select {
	case p := <-got:
		if p != want {
			t.Fatalf("decoded = %+v; want %+v", p, want)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for typed StateChanged delivery")
	}

	// Failure variant: Err non-empty must survive too (the failed-DLR
	// observability lane daemon arming publishes).
	fail := StateChanged{ProviderMessageID: "msg-topic-2", Status: "failed", Err: "carrier rejected"}
	bus.PublishT(b, StateTopic, "test", fail)
	select {
	case p := <-got:
		if p != fail {
			t.Fatalf("decoded fail variant = %+v; want %+v", p, fail)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for failed-state delivery")
	}
}

// TestTopicsRawInterop pins the string-name interop rule: a raw publish
// (models.NewBusMessage) whose JSON is the typed payload marshalled by
// encoding/json decodes into the typed subscriber — the arming glue and
// any raw observability publisher share one bus lane per name. The JSON
// is produced by marshalling a real InboundMessage (not hand-written
// keys) so the test pins the lane, not the struct-tag shape the contract
// leaves free.
func TestTopicsRawInterop(t *testing.T) {
	b := bus.New(nil, nil)
	defer b.Close()

	got := make(chan InboundMessage, 1)
	sub := bus.SubscribeT(b, "raw-interop", InboundTopic, func(p InboundMessage) { got <- p })
	defer b.Unsubscribe(sub)

	want := InboundMessage{
		EventID: "evt-raw-1",
		From:    "+15550000102",
		To:      "+15550009998",
		Text:    "ping",
		At:      time.Now().UTC().Truncate(time.Millisecond),
	}
	data, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	msg, err := models.NewBusMessage(models.MessageTypeEvent, "raw-test", json.RawMessage(data))
	if err != nil {
		t.Fatalf("NewBusMessage: %v", err)
	}
	b.Publish(InboundTopic.Name, msg)

	select {
	case p := <-got:
		if p.EventID != want.EventID || p.From != want.From || p.Text != want.Text || !p.At.Equal(want.At) {
			t.Fatalf("raw-decoded payload = %+v; want %+v", p, want)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("raw publish to the topic name did not reach the typed subscriber")
	}
}
