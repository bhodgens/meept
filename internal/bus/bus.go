// Package bus provides a channel-based pub/sub message bus.
package bus

import (
	"context"
	"fmt"
	"log/slog"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/caimlas/meept/pkg/models"
)

// Agent lifecycle event topics.
const (
	EventAgentStarted   = "agent.lifecycle.started"
	EventAgentEnded     = "agent.lifecycle.ended"
	EventAgentIteration = "agent.iteration.completed"

	// EventAgentStateChanged is published when an agent transitions between
	// formal states (see internal/agent/agent_state.go). Payload is
	// agent.StateTransition.
	EventAgentStateChanged = "agent.state.changed"
)

// Session lifecycle event topics.
const (
	EventSessionStart = "session.lifecycle.start"
	EventSessionEnd   = "session.lifecycle.end"
)

// Queue event topics.
const (
	EventQueueSteerAdded       = "agent.queue.steer.added"
	EventQueueFollowUpAdded    = "agent.queue.followup.added"
	EventQueueSteerInjected    = "agent.queue.steer.injected"
	EventQueueFollowUpInjected = "agent.queue.followup.injected"
	EventQueueFollowUpRestored = "agent.queue.followup.restored"
	EventQueuePersisted        = "agent.queue.persisted"
	EventQueueStatus           = "agent.queue.status"
)

// Subscriber represents a channel that receives messages.
type Subscriber struct {
	ID      string
	Topic   string
	Channel chan *models.BusMessage
}

// SubMetadata tracks subscription metadata for debugging dead subscriptions.
type SubMetadata struct {
	Topic     string
	CreatedAt time.Time
	Caller    string // runtime.Caller(2) - where Subscribe was called
}

// MessageBus implements a channel-based publish/subscribe message bus.
type MessageBus struct {
	mu          sync.RWMutex
	subscribers map[string][]*Subscriber
	bufferSize  int
	closed      bool
	logger      *slog.Logger
	messagesSent atomic.Int64

	// Subscription tracking for debugging dead subscriptions (Phase 1)
	subMeta map[string]SubMetadata // key = channel address (%p format)
}

// panicOnUndrainedSubscription enables error-level logging mode for tests.
// When true, Publish() will log at Error level if called on a topic with no subscribers.
var panicOnUndrainedSubscription = false

// SetPanicOnUndrainedSubscription enables/disables error-level logging mode for testing.
// When enabled, Publish() logs at Error level if no subscribers exist for the topic.
func SetPanicOnUndrainedSubscription(enabled bool) {
	panicOnUndrainedSubscription = enabled
}

// Config holds MessageBus configuration.
type Config struct {
	BufferSize int
}

// DefaultConfig returns the default configuration.
func DefaultConfig() *Config {
	return &Config{
		BufferSize: 100,
	}
}

// New creates a new MessageBus.
func New(cfg *Config, logger *slog.Logger) *MessageBus {
	if cfg == nil {
		cfg = DefaultConfig()
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &MessageBus{
		subscribers: make(map[string][]*Subscriber),
		bufferSize:  cfg.BufferSize,
		logger:      logger,
		subMeta:     make(map[string]SubMetadata),
	}
}

// Publish sends a message to all subscribers of the topic.
// It also publishes to wildcard subscribers (e.g., "agent.*" matches "agent.status").
// A nil msg is silently dropped to protect subscriber goroutines from
// nil-pointer dereferences when reading the message.
func (b *MessageBus) Publish(topic string, msg *models.BusMessage) int {
	return b.publish(topic, msg, false, false)
}

// PublishBlocking sends a message to all subscribers, blocking until the
// message is enqueued in every subscriber's channel or a 5-second timeout
// elapses. Use this for security-critical events where message drops are
// unacceptable (e.g., approval requests). Returns the number of subscribers
// the message was delivered to.
func (b *MessageBus) PublishBlocking(topic string, msg *models.BusMessage) int {
	return b.publish(topic, msg, false, true)
}

// PublishExternalOnly is like Publish but downgrades the "no subscribers" log
// from WARN to DEBUG. Use this for fire-and-forget event topics that are
// informational and expected to have no subscriber when no TUI/MCP client is
// connected (e.g., worker.*, chat.message.received).
// The panicOnUndrainedSubscription behavior is unchanged — it now logs at
// Error level instead of panicking, so tests still catch missing wiring.
func (b *MessageBus) PublishExternalOnly(topic string, msg *models.BusMessage) int {
	return b.publish(topic, msg, true, false)
}

// publish is the shared core for Publish, PublishBlocking, and PublishExternalOnly.
// When suppressWarning is true, the "no subscribers" log is downgraded from
// WARN to DEBUG. When blocking is true, sends to subscriber channels use a
// 5-second timeout instead of non-blocking select-default.
func (b *MessageBus) publish(topic string, msg *models.BusMessage, suppressWarning, blocking bool) int {
	if msg == nil {
		return 0
	}

	b.mu.RLock()
	subs := b.subscribers[topic]

	// Check for zero subscribers - warn (and optionally panic for tests)
	if len(subs) == 0 {
		// Check wildcard subscribers too
		hasWildcardSubs := false
		for pattern, subList := range b.subscribers {
			if pattern != topic && matchWildcard(pattern, topic) {
				if len(subList) > 0 {
					hasWildcardSubs = true
					break
				}
			}
		}

		if !hasWildcardSubs {
			b.mu.RUnlock()
			if suppressWarning {
				b.logger.Debug("bus: Publish with no subscribers (external-only topic)",
					"topic", topic,
					"source", msg.Source,
					"msg_id", msg.ID,
				)
			} else {
				b.logger.Warn("bus: Publish with no subscribers",
					"topic", topic,
					"source", msg.Source,
					"msg_id", msg.ID,
				)
			}
			if panicOnUndrainedSubscription {
				b.logger.Error("bus: Publish with no subscribers (panic mode enabled)",
					"topic", topic,
					"source", msg.Source,
					"msg_id", msg.ID,
				)
			}
			return 0
		}
	}

	// Check for near-full subscriber buffers (early warning)
	for _, sub := range subs {
		if cap(sub.Channel) > 0 && len(sub.Channel) > cap(sub.Channel)*9/10 {
			utilization := float64(len(sub.Channel)) / float64(cap(sub.Channel))
			b.logger.Warn("bus: subscriber buffer near full",
				"topic", topic,
				"subscriber", sub.ID,
				"utilization", utilization,
			)
		}
	}

	b.mu.RUnlock()

	b.mu.RLock()
	defer b.mu.RUnlock()

	if b.closed {
		return 0
	}

	msg.Topic = topic
	delivered := 0
	b.messagesSent.Add(1)

	// Direct topic subscribers
	for _, sub := range b.subscribers[topic] {
		if blocking {
			select {
			case sub.Channel <- msg:
				delivered++
			case <-time.After(5 * time.Second):
				b.logger.Error("bus: blocking publish timed out (buffer full)",
					"topic", topic,
					"subscriber", sub.ID,
				)
			}
		} else {
			select {
			case sub.Channel <- msg:
				delivered++
			default:
				b.logger.Warn("bus: dropped message (buffer full)",
					"topic", topic,
					"subscriber", sub.ID,
				)
			}
		}
	}

	// Wildcard subscribers (e.g., "agent.*")
	for pattern, subs := range b.subscribers {
		if pattern != topic && matchWildcard(pattern, topic) {
			for _, sub := range subs {
				if blocking {
					select {
					case sub.Channel <- msg:
						delivered++
					case <-time.After(5 * time.Second):
						b.logger.Error("bus: blocking publish timed out (buffer full)",
							"topic", topic,
							"subscriber", sub.ID,
						)
					}
				} else {
					select {
					case sub.Channel <- msg:
						delivered++
					default:
						b.logger.Warn("bus: dropped message (buffer full)",
							"topic", topic,
							"subscriber", sub.ID,
						)
					}
				}
			}
		}
	}

	return delivered
}

// Subscribe creates a subscription to a topic.
// The topic can contain wildcards: "agent.*" matches "agent.status", "agent.error".
func (b *MessageBus) Subscribe(id, topic string) *Subscriber {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		// Return a subscriber with a closed channel so callers' defer Unsubscribe works.
		sub := &Subscriber{ID: id, Topic: topic, Channel: make(chan *models.BusMessage, 1)}
		close(sub.Channel)
		return sub
	}

	sub := &Subscriber{
		ID:      id,
		Topic:   topic,
		Channel: make(chan *models.BusMessage, b.bufferSize),
	}

	b.subscribers[topic] = append(b.subscribers[topic], sub)

	// Track subscription metadata for debugging
	chAddr := fmt.Sprintf("%p", sub.Channel)
	b.subMeta[chAddr] = SubMetadata{
		Topic:     topic,
		CreatedAt: time.Now(),
		Caller:    callerName(2),
	}

	b.logger.Debug("bus: new subscriber", "id", id, "topic", topic)
	return sub
}

// callerName returns the function name from runtime.Caller at the given depth.
func callerName(depth int) string {
	pc, _, _, ok := runtime.Caller(depth)
	if !ok {
		return "unknown"
	}
	fn := runtime.FuncForPC(pc)
	if fn == nil {
		return "unknown"
	}
	return fn.Name()
}

// Unsubscribe removes a subscription.
func (b *MessageBus) Unsubscribe(sub *Subscriber) {
	b.mu.Lock()
	defer b.mu.Unlock()

	subs := b.subscribers[sub.Topic]
	for i, s := range subs {
		if s.ID == sub.ID {
			b.subscribers[sub.Topic] = append(subs[:i], subs[i+1:]...)

			// Clean up subscription metadata
			chAddr := fmt.Sprintf("%p", sub.Channel)
			delete(b.subMeta, chAddr)

			close(sub.Channel)
			b.logger.Debug("bus: unsubscribed", "id", sub.ID, "topic", sub.Topic)
			return
		}
	}
}

// Request sends a message and waits for a reply.
func (b *MessageBus) Request(ctx context.Context, topic string, msg *models.BusMessage) (*models.BusMessage, error) {
	// Create a reply channel
	replyTopic := "reply." + msg.ID
	replySub := b.Subscribe(msg.ID, replyTopic)
	defer b.Unsubscribe(replySub)

	msg.ReplyTo = replyTopic
	b.Publish(topic, msg)

	select {
	case reply := <-replySub.Channel:
		return reply, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// Close shuts down the message bus and closes all subscriber channels.
func (b *MessageBus) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		return
	}
	b.closed = true

	for _, subs := range b.subscribers {
		for _, sub := range subs {
			close(sub.Channel)
		}
	}
	b.subscribers = nil
	b.logger.Info("bus: closed")
}

// Stats returns current bus statistics.
func (b *MessageBus) Stats() map[string]int {
	b.mu.RLock()
	defer b.mu.RUnlock()

	stats := make(map[string]int)
	total := 0
	queued := 0
	for topic, subs := range b.subscribers {
		stats[topic] = len(subs)
		total += len(subs)
		for _, sub := range subs {
			queued += len(sub.Channel)
		}
	}
	stats["_total"] = total
	stats["_messages_sent"] = int(b.messagesSent.Load())
	stats["_queued"] = queued
	return stats
}

// HasSubscribers reports whether any subscriber (direct or wildcard) is
// registered for the given topic. Use this to skip expensive publishes when
// nobody is listening (e.g., SSE heartbeats in non-streaming mode).
func (b *MessageBus) HasSubscribers(topic string) bool {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if len(b.subscribers[topic]) > 0 {
		return true
	}
	for pattern, subs := range b.subscribers {
		if pattern != topic && len(subs) > 0 && matchWildcard(pattern, topic) {
			return true
		}
	}
	return false
}

// matchWildcard checks if a pattern matches a topic.
// Pattern "agent.*" matches "agent.status" but not "agent.sub.topic".
func matchWildcard(pattern, topic string) bool {
	if !strings.Contains(pattern, "*") {
		return pattern == topic
	}

	parts := strings.Split(pattern, ".")
	topicParts := strings.Split(topic, ".")

	if len(parts) != len(topicParts) {
		return false
	}

	for i, part := range parts {
		if part != "*" && part != topicParts[i] {
			return false
		}
	}
	return true
}
