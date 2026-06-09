package bus

import (
	"testing"
	"time"

	"github.com/caimlas/meept/pkg/models"
	"github.com/stretchr/testify/assert"
)

func TestMessageBus_PubSub(t *testing.T) {
	bus := New(nil, nil)
	defer bus.Close()

	// Subscribe
	sub := bus.Subscribe("test-sub", "test.topic")

	// Publish
	msg := &models.BusMessage{
		ID:      "msg-1",
		Type:    models.MessageTypeEvent,
		Source:  "test",
		Payload: []byte(`{"data": "hello"}`),
	}
	delivered := bus.Publish("test.topic", msg)

	assert.Equal(t, 1, delivered)

	// Receive
	select {
	case received := <-sub.Channel:
		if received.ID != "msg-1" {
			t.Errorf("expected msg-1, got %s", received.ID)
		}
		if received.Topic != "test.topic" {
			t.Errorf("expected test.topic, got %s", received.Topic)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("timeout waiting for message")
	}
}

func TestMessageBus_Wildcard(t *testing.T) {
	bus := New(nil, nil)
	defer bus.Close()

	// Subscribe to wildcard
	sub := bus.Subscribe("test-sub", "agent.*")

	// Publish to matching topic
	msg := &models.BusMessage{
		ID:     "msg-1",
		Type:   models.MessageTypeEvent,
		Source: "test",
	}
	delivered := bus.Publish("agent.status", msg)

	assert.Equal(t, 1, delivered)

	// Receive
	select {
	case received := <-sub.Channel:
		if received.Topic != "agent.status" {
			t.Errorf("expected agent.status, got %s", received.Topic)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("timeout waiting for message")
	}
}

func TestMessageBus_Unsubscribe(t *testing.T) {
	bus := New(nil, nil)
	defer bus.Close()

	sub := bus.Subscribe("test-sub", "test.topic")
	bus.Unsubscribe(sub)

	// Verify channel is closed
	select {
	case _, ok := <-sub.Channel:
		assert.False(t, ok, "expected channel to be closed")
	default:
		// Channel closed, as expected
	}
}

func TestMessageBus_Stats(t *testing.T) {
	bus := New(nil, nil)
	defer bus.Close()

	bus.Subscribe("sub1", "topic1")
	bus.Subscribe("sub2", "topic1")
	bus.Subscribe("sub3", "topic2")

	stats := bus.Stats()

	if stats["topic1"] != 2 {
		t.Errorf("expected 2 subscribers for topic1, got %d", stats["topic1"])
	}
	if stats["topic2"] != 1 {
		t.Errorf("expected 1 subscriber for topic2, got %d", stats["topic2"])
	}
	if stats["_total"] != 3 {
		t.Errorf("expected 3 total subscribers, got %d", stats["_total"])
	}
}

func TestMatchWildcard(t *testing.T) {
	tests := []struct {
		pattern string
		topic   string
		want    bool
	}{
		{"agent.*", "agent.status", true},
		{"agent.*", "agent.error", true},
		{"agent.*", "agent", false},
		{"agent.*", "agent.sub.topic", false},
		{"*.status", "agent.status", true},
		{"*.status", "daemon.status", true},
		{"exact.match", "exact.match", true},
		{"exact.match", "other.match", false},
	}

	for _, tt := range tests {
		got := matchWildcard(tt.pattern, tt.topic)
		if got != tt.want {
			t.Errorf("matchWildcard(%q, %q) = %v, want %v",
				tt.pattern, tt.topic, got, tt.want)
		}
	}
}
