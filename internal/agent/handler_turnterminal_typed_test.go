package agent

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/pkg/models"
)

// ---------------------------------------------------------------------------
// typed-bus-topics leaf 02: turn.terminal publisher migration tests.
//
// The publisher is the single emission point for the frozen
// TurnTerminalEvent payload. Consumers are the WS relay via its "*"
// wildcard subscription (internal/comm/http/server.go) and the TUI via
// the RPC bus.subscribe event stream (internal/rpc/proxy.go) - there is
// deliberately NO direct bus.Subscribe("turn.terminal") call site, so
// these tests consume through the same wildcard/typed paths those
// consumers use. NOTE: a bare "*" wildcard only matches single-segment
// topics (matchWildcard is segment-counted) — the WS relay carries both
// "*" and "turn.*"; turn.terminal matches the latter.
// ---------------------------------------------------------------------------

// representativeTurnTerminalEvent covers every field class of the frozen
// payload: required keys, omitempty keys set, and omitempty keys left
// unset (their absence is part of the wire contract).
func representativeTurnTerminalEvent() TurnTerminalEvent {
	return TurnTerminalEvent{
		ConversationID: "conv-typedtest",
		SessionID:      "session-typedtest",
		TurnID:         "turn-typedtest-001",
		TaskID:         "task-typedtest-001",
		IntentType:     "task_dispatch",
		AgentID:        "agent-typedtest",
		HandlerCase:    "quickplan",
		Status:         "completed",
		Reply:          "all done",
		DurationMS:     1234,
		ClassifiedBy:   "llm",
		Model:          "openai/gpt-test",
		// Error omitted: non-empty iff Status=="failed" (omitempty wire).
	}
}

// newTypedTestHandler builds a ChatHandler on a real message bus and
// registers a wildcard subscriber mirroring the WS relay's consumption
// shape ("turn.*" — the bus matchWildcard is segment-counted, so the
// relay subscribes both "*" for single-segment topics and "turn.*" for
// two-segment ones; turn.terminal arrives via the latter).
func newTypedTestHandler(t *testing.T) (*ChatHandler, *bus.Subscriber, *bus.MessageBus) {
	t.Helper()
	logger := slogDiscardLogger()
	b := bus.New(nil, logger)
	t.Cleanup(func() { b.Close() })
	h := NewChatHandler(nil, nil, b, logger)
	sub := b.Subscribe("wildcard", "turn.*")
	if sub == nil {
		t.Fatal("bus.Subscribe(wildcard, turn.*) returned nil")
	}
	t.Cleanup(func() { b.Unsubscribe(sub) })
	return h, sub, b
}

// waitOneMessage receives exactly one message from sub, failing on
// timeout or on a nil message.
func waitOneMessage(t *testing.T, sub *bus.Subscriber) *models.BusMessage {
	t.Helper()
	select {
	case msg := <-sub.Channel:
		if msg == nil {
			t.Fatal("received nil BusMessage from wildcard subscriber")
		}
		return msg
	case <-time.After(10 * time.Second):
		t.Fatal("no message received on wildcard subscriber")
		return nil
	}
}

// assertNoFurtherMessage proves exactly-once delivery (no double
// publish on the migrated path).
func assertNoFurtherMessage(t *testing.T, sub *bus.Subscriber) {
	t.Helper()
	select {
	case msg := <-sub.Channel:
		t.Fatalf("unexpected second message: %s", msg.Payload)
	case <-time.After(300 * time.Millisecond):
	}
}

// TestPublishTurnTerminal_TypedDelivery drives the migrated publisher
// through a real MessageBus to a raw wildcard subscriber (the WS relay's
// "turn.*" subscription shape) and asserts the full BusMessage envelope:
// topic name, message type, source, and payload round-trip. The
// handler-side gate for the typed path itself is compile-level:
// handler.go references TopicTurnTerminal
// (bus.PublishT(h.bus, TopicTurnTerminal, ...)).
func TestPublishTurnTerminal_TypedDelivery(t *testing.T) {
	h, sub, _ := newTypedTestHandler(t)
	ev := representativeTurnTerminalEvent()

	h.publishTurnTerminal(ev)

	msg := waitOneMessage(t, sub)

	if msg.Topic != "turn.terminal" {
		t.Errorf("Topic = %q, want %q", msg.Topic, "turn.terminal")
	}
	if msg.Type != models.MessageTypeEvent {
		t.Errorf("Type = %q, want %q", msg.Type, models.MessageTypeEvent)
	}
	if msg.Source != SourceChatHandler {
		t.Errorf("Source = %q, want %q", msg.Source, SourceChatHandler)
	}
	if msg.Timestamp.IsZero() {
		t.Error("Timestamp is zero; expected the bus message to carry a timestamp")
	}

	var got TurnTerminalEvent
	if err := json.Unmarshal(msg.Payload, &got); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if !reflect.DeepEqual(got, ev) {
		t.Errorf("payload round-trip mismatch:\n got %+v\nwant %+v", got, ev)
	}

	assertNoFurtherMessage(t, sub)
}

// TestPublishTurnTerminal_TypedPathWireMatches pins the typed
// declaration to the handler's wire output: publishing the same event
// through bus.PublishT(b, TopicTurnTerminal, ...) must produce
// payload bytes identical to the handler's message (envelope timestamps
// differ by design and are not compared).
func TestPublishTurnTerminal_TypedPathWireMatches(t *testing.T) {
	h, sub, b := newTypedTestHandler(t)
	ev := representativeTurnTerminalEvent()

	h.publishTurnTerminal(ev)
	msg := waitOneMessage(t, sub)

	bus.PublishT(b, TopicTurnTerminal, SourceChatHandler, ev)
	typedMsg := waitOneMessage(t, sub)

	if string(typedMsg.Payload) != string(msg.Payload) {
		t.Errorf("typed-path payload differs from handler payload:\n typed: %s\nhandler: %s",
			typedMsg.Payload, msg.Payload)
	}
	assertNoFurtherMessage(t, sub)
}

// TestPublishTurnTerminal_WireIdentity pins the exact wire bytes: the
// received Payload must EQUAL json.Marshal of the same event directly.
// This locks field tags, omitempty behavior, and the absence of any
// envelope-added keys inside the payload (the event struct carries no
// timestamp field of its own).
func TestPublishTurnTerminal_WireIdentity(t *testing.T) {
	h, sub, _ := newTypedTestHandler(t)
	ev := representativeTurnTerminalEvent()

	h.publishTurnTerminal(ev)
	msg := waitOneMessage(t, sub)

	want, err := json.Marshal(ev)
	if err != nil {
		t.Fatalf("marshal reference event: %v", err)
	}
	if string(msg.Payload) != string(want) {
		t.Errorf("wire identity broken:\n got: %s\nwant: %s", msg.Payload, want)
	}

	// Omitempty contract: fields left zero must not appear on the wire.
	var wire map[string]any
	if err := json.Unmarshal(msg.Payload, &wire); err != nil {
		t.Fatalf("unmarshal payload map: %v", err)
	}
	for _, absent := range []string{"error"} {
		if _, ok := wire[absent]; ok {
			t.Errorf("omitempty field %q unexpectedly present on wire", absent)
		}
	}
	for _, required := range []string{"conversation_id", "turn_id", "handler_case", "status", "reply", "duration_ms"} {
		if _, ok := wire[required]; !ok {
			t.Errorf("required field %q missing from wire", required)
		}
	}

	assertNoFurtherMessage(t, sub)
}

// TestPublishTurnTerminal_NilGuards covers the guard paths. The
// marshal-failure log path itself is unreachable for the closed,
// marshal-safe TurnTerminalEvent field set (only strings and int64) with
// the pre-marshal in place - there is no constructible value of the
// struct that fails json.Marshal - so it cannot be exercised directly;
// the guards (h == nil, h.bus == nil) that sit before it are tested to
// prove both no-op cleanly.
func TestPublishTurnTerminal_NilGuards(t *testing.T) {
	t.Run("nil handler", func(t *testing.T) {
		var h *ChatHandler
		h.publishTurnTerminal(representativeTurnTerminalEvent()) // must not panic
	})
	t.Run("nil bus", func(t *testing.T) {
		h := &ChatHandler{logger: slogDiscardLogger()}
		h.publishTurnTerminal(representativeTurnTerminalEvent()) // must not panic
	})
}

// TestTopicTurnTerminal_Declaration sanity-checks the declaration name
// so a rename of the topic string here fails loudly next to the wire
// identity tests above.
func TestTopicTurnTerminal_Declaration(t *testing.T) {
	if TopicTurnTerminal.Name != "turn.terminal" {
		t.Errorf("TopicTurnTerminal.Name = %q, want %q", TopicTurnTerminal.Name, "turn.terminal")
	}
}
