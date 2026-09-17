package bus

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/caimlas/meept/pkg/models"
)

// Topic is a compile-time-checked bus topic with a typed payload.
// Declare topics as package-level vars; reference the same var from
// publisher and subscriber so the compiler enforces payload agreement.
type Topic[T any] struct {
	Name string
}

// NewTopic declares a typed topic. Name must match the legacy string
// topic exactly - typed and raw publishes to the same name interoperate.
func NewTopic[T any](name string) Topic[T] { return Topic[T]{Name: name} }

// typeName returns the generic type parameter's name for diagnostics.
func typeName[T any]() string {
	var zero T
	return fmt.Sprintf("%T", zero)
}

// PublishT marshals payload as T and publishes to t's topic.
//
// Marshal-failure policy: json.Marshal of T fails only for channels,
// funcs, or reference cycles - programmer error, not a runtime condition.
// This library therefore PANICS with a wrapped, descriptive message on
// marshal failure (option (a)); repo precedent for panicking on
// programmer-error conditions exists in internal/llm, internal/security,
// and internal/auth, and no precedent prohibits it in bus-adjacent code.
// The returned int is the delivery count from MessageBus.Publish.
func PublishT[T any](b *MessageBus, t Topic[T], source string, payload T) int {
	data, err := json.Marshal(payload)
	if err != nil {
		panic(fmt.Sprintf("bus: PublishT: marshal payload for topic %q (%s): %v", t.Name, typeName[T](), err))
	}
	msg := &models.BusMessage{
		Type:      models.MessageTypeEvent,
		Source:    source,
		Timestamp: time.Now().UTC(),
		Payload:   data,
	}
	return b.Publish(t.Name, msg)
}

// PublishBlockingT is PublishT with MessageBus.PublishBlocking delivery:
// sends to subscriber channels with a 5-second timeout instead of a
// non-blocking drop-on-full. For per-turn-once events whose loss breaks a
// client state machine (turn.terminal is the canonical case: awaiting
// clients key on it, and the C-09 security-approval class of full-buffer
// drops applies). Marshal policy identical to PublishT.
func PublishBlockingT[T any](b *MessageBus, t Topic[T], source string, payload T) int {
	data, err := json.Marshal(payload)
	if err != nil {
		panic(fmt.Sprintf("bus: PublishBlockingT: marshal payload for topic %q (%s): %v", t.Name, typeName[T](), err))
	}
	msg := &models.BusMessage{
		Type:      models.MessageTypeEvent,
		Source:    source,
		Timestamp: time.Now().UTC(),
		Payload:   data,
	}
	return b.PublishBlocking(t.Name, msg)
}

// SubscribeT subscribes to t and invokes fn with each decoded payload.
//
// Decode failures are logged at warn level (topic + type name) and the
// message is dropped.
//
// Lifetime: SubscribeT starts exactly ONE reader goroutine that runs
// until the subscriber's channel is closed (by MessageBus.Close or
// Unsubscribe). Callers must Unsubscribe the returned Subscriber when
// done, exactly as with the raw Subscribe API.
func SubscribeT[T any](b *MessageBus, id string, t Topic[T], fn func(T)) *Subscriber {
	sub := b.Subscribe(id, t.Name)
	if sub == nil {
		return sub
	}
	// Closed-bus sentinel: bus.Subscribe returns a pre-closed-channel
	// Subscriber when the bus is closed. Detect it via a non-blocking
	// receive (a closed channel reports ok=false immediately) and return
	// it without starting the reader goroutine.
	select {
	case _, ok := <-sub.Channel:
		if !ok {
			return sub
		}
	default:
	}
	go func() {
		for m := range sub.Channel {
			var payload T
			if err := json.Unmarshal(m.Payload, &payload); err != nil {
				b.logger.Warn("bus: SubscribeT: dropping undecodable payload",
					"topic", t.Name,
					"type", typeName[T](),
					"error", err)
				continue
			}
			fn(payload)
		}
	}()
	return sub
}
