package bus

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/caimlas/meept/pkg/models"
	"github.com/stretchr/testify/assert"
)

// TurnTerminalTestPayload is a small struct used by the typed-topic tests.
type TurnTerminalTestPayload struct {
	TurnID string `json:"turn_id"`
	Status string `json:"status"`
}

func TestNewTopic_DeclaresName(t *testing.T) {
	tp := NewTopic[TurnTerminalTestPayload]("turn.terminal")
	if tp.Name != "turn.terminal" {
		t.Fatalf("Topic.Name = %q, want %q", tp.Name, "turn.terminal")
	}
}

func TestSubscribeT_DecodesPayload(t *testing.T) {
	bus := New(nil, nil)
	defer bus.Close()

	got := make(chan TurnTerminalTestPayload, 1)
	topic := NewTopic[TurnTerminalTestPayload]("turn.terminal")
	sub := SubscribeT(bus, "test-sub", topic, func(p TurnTerminalTestPayload) { got <- p })
	defer bus.Unsubscribe(sub)

	PublishT(bus, topic, "test", TurnTerminalTestPayload{TurnID: "t1", Status: "ok"})

	select {
	case p := <-got:
		if p.TurnID != "t1" || p.Status != "ok" {
			t.Fatalf("decoded payload = %+v, want TurnID=t1 Status=ok", p)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for typed delivery")
	}
}

func TestSubscribeT_DropsUndecodablePayload(t *testing.T) {
	bus := New(nil, nil)
	defer bus.Close()

	called := make(chan struct{}, 1)
	topic := NewTopic[TurnTerminalTestPayload]("turn.terminal")
	sub := SubscribeT(bus, "test-sub", topic, func(TurnTerminalTestPayload) { called <- struct{}{} })
	defer bus.Unsubscribe(sub)

	// Raw publish of wrong-shape JSON to the same topic name: an array
	// cannot unmarshal into a struct, so T decode must fail.
	msg, err := models.NewBusMessage(models.MessageTypeEvent, "raw", []string{"wrong", "shape"})
	assert.NoError(t, err)
	bus.Publish(topic.Name, msg)

	select {
	case <-called:
		t.Fatal("fn must not be invoked for undecodable payload")
	case <-time.After(200 * time.Millisecond):
		// expected: payload dropped, no panic, test completes
	}
}

func TestPublishT_RoundTrip(t *testing.T) {
	bus := New(nil, nil)
	defer bus.Close()

	want := TurnTerminalTestPayload{TurnID: "t2", Status: "done"}
	got := make(chan TurnTerminalTestPayload, 1)
	topic := NewTopic[TurnTerminalTestPayload]("turn.terminal")
	sub := SubscribeT(bus, "rt-sub", topic, func(p TurnTerminalTestPayload) { got <- p })
	defer bus.Unsubscribe(sub)

	n := PublishT(bus, topic, "test", want)
	assert.Equal(t, 1, n)

	select {
	case p := <-got:
		assert.True(t, reflect.DeepEqual(want, p), "round-trip payload = %+v, want %+v", p, want)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for round-trip delivery")
	}
}

func TestPublishT_InteropsWithRawPublish(t *testing.T) {
	t.Run("raw publish received by typed subscriber", func(t *testing.T) {
		bus := New(nil, nil)
		defer bus.Close()

		topic := NewTopic[TurnTerminalTestPayload]("turn.terminal")
		got := make(chan TurnTerminalTestPayload, 1)
		sub := SubscribeT(bus, "typed-sub", topic, func(p TurnTerminalTestPayload) { got <- p })
		defer bus.Unsubscribe(sub)

		msg, err := models.NewBusMessage(models.MessageTypeEvent, "raw", TurnTerminalTestPayload{TurnID: "r1", Status: "ok"})
		assert.NoError(t, err)
		bus.Publish(topic.Name, msg)

		select {
		case p := <-got:
			assert.Equal(t, "r1", p.TurnID)
			assert.Equal(t, "ok", p.Status)
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for typed delivery of raw publish")
		}
	})

	t.Run("typed publish received by raw subscriber", func(t *testing.T) {
		bus := New(nil, nil)
		defer bus.Close()

		topic := NewTopic[TurnTerminalTestPayload]("turn.terminal")
		sub := bus.Subscribe("raw-sub", topic.Name)
		defer bus.Unsubscribe(sub)

		PublishT(bus, topic, "typed", TurnTerminalTestPayload{TurnID: "r2", Status: "sent"})

		select {
		case m := <-sub.Channel:
			var p TurnTerminalTestPayload
			assert.NoError(t, json.Unmarshal(m.Payload, &p))
			assert.Equal(t, "r2", p.TurnID)
			assert.Equal(t, "sent", p.Status)
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for raw delivery of typed publish")
		}
	})
}

func TestPublishT_PanicsOnUnmarshalablePayload(t *testing.T) {
	bus := New(nil, nil)
	defer bus.Close()

	topic := NewTopic[PayloadWithChan]("turn.chan")
	assert.PanicsWithValue(t,
		`bus: PublishT: marshal payload for topic "turn.chan" (bus.PayloadWithChan): json: unsupported type: chan int`,
		func() { PublishT(bus, topic, "test", PayloadWithChan{Ch: make(chan int)}) })
}

func TestPublishT_TypedPublishReachesWildcardRawSubscriber(t *testing.T) {
	bus := New(nil, nil)
	defer bus.Close()

	sub := bus.Subscribe("wild-sub", "turn.*")
	defer bus.Unsubscribe(sub)

	topic := NewTopic[TurnTerminalTestPayload]("turn.terminal")
	PublishT(bus, topic, "typed", TurnTerminalTestPayload{TurnID: "w1", Status: "ok"})

	select {
	case m := <-sub.Channel:
		assert.Equal(t, "turn.terminal", m.Topic)
		var p TurnTerminalTestPayload
		assert.NoError(t, json.Unmarshal(m.Payload, &p))
		assert.Equal(t, "w1", p.TurnID)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for wildcard delivery")
	}
}

// PayloadWithChan is unmarshalable by design (chan field) for the panic test.
type PayloadWithChan struct {
	Ch chan int
}
