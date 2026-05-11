// Package bus provides a channel-based pub/sub message bus.
package bus

import (
	"context"
	"log/slog"
	"strings"
	"sync"

	"github.com/caimlas/meept/pkg/models"
)

// Agent lifecycle event topics.
const (
	EventAgentStarted   = "agent.lifecycle.started"
	EventAgentEnded     = "agent.lifecycle.ended"
	EventAgentIteration = "agent.iteration.completed"
)

// Queue event topics.
const (
	EventQueueSteerAdded       = "agent.queue.steer.added"
	EventQueueFollowUpAdded    = "agent.queue.followup.added"
	EventQueueSteerInjected    = "agent.queue.steer.injected"
	EventQueueFollowUpInjected = "agent.queue.followup.injected"
	EventQueueFollowUpRestored = "agent.queue.followup.restored"
	EventQueuePersisted        = "agent.queue.persisted"
)

// Subscriber represents a channel that receives messages.
type Subscriber struct {
	ID      string
	Topic   string
	Channel chan *models.BusMessage
}

// MessageBus implements a channel-based publish/subscribe message bus.
type MessageBus struct {
	mu          sync.RWMutex
	subscribers map[string][]*Subscriber
	bufferSize  int
	closed      bool
	logger      *slog.Logger
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
	}
}

// Publish sends a message to all subscribers of the topic.
// It also publishes to wildcard subscribers (e.g., "agent.*" matches "agent.status").
func (b *MessageBus) Publish(topic string, msg *models.BusMessage) int {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if b.closed {
		return 0
	}

	msg.Topic = topic
	delivered := 0

	// Direct topic subscribers
	for _, sub := range b.subscribers[topic] {
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

	// Wildcard subscribers (e.g., "agent.*")
	for pattern, subs := range b.subscribers {
		if pattern != topic && matchWildcard(pattern, topic) {
			for _, sub := range subs {
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

	return delivered
}

// Subscribe creates a subscription to a topic.
// The topic can contain wildcards: "agent.*" matches "agent.status", "agent.error".
func (b *MessageBus) Subscribe(id, topic string) *Subscriber {
	b.mu.Lock()
	defer b.mu.Unlock()

	sub := &Subscriber{
		ID:      id,
		Topic:   topic,
		Channel: make(chan *models.BusMessage, b.bufferSize),
	}

	b.subscribers[topic] = append(b.subscribers[topic], sub)
	b.logger.Debug("bus: new subscriber", "id", id, "topic", topic)
	return sub
}

// Unsubscribe removes a subscription.
func (b *MessageBus) Unsubscribe(sub *Subscriber) {
	b.mu.Lock()
	defer b.mu.Unlock()

	subs := b.subscribers[sub.Topic]
	for i, s := range subs {
		if s.ID == sub.ID {
			b.subscribers[sub.Topic] = append(subs[:i], subs[i+1:]...)
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
	for topic, subs := range b.subscribers {
		stats[topic] = len(subs)
		total += len(subs)
	}
	stats["_total"] = total
	return stats
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
