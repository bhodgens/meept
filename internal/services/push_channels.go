// Package services provides push notification channel routing.
package services

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/pkg/models"
)

// PushMessage represents a formatted push notification.
type PushMessage struct {
	ID       string            `json:"id"`
	Type     PushType          `json:"type"`
	Priority PushPriority      `json:"priority"`
	Source   string            `json:"source"`
	Content  string            `json:"content"`
	Meta     map[string]string `json:"meta,omitempty"`
}

// PushChannel defines the interface for notification delivery channels.
type PushChannel interface {
	// Name returns the channel identifier
	Name() string
	// CanReceive checks if this channel can deliver to the given session
	CanReceive(sessionID string) bool
	// Push delivers a message to the channel
	Push(ctx context.Context, sessionID string, msg *PushMessage) error
}

// ChannelRegistry manages push notification channels.
type ChannelRegistry struct {
	mu       sync.RWMutex
	channels map[string]PushChannel
}

// NewChannelRegistry creates a new channel registry.
func NewChannelRegistry() *ChannelRegistry {
	return &ChannelRegistry{
		channels: make(map[string]PushChannel),
	}
}

// Register adds a channel to the registry.
func (r *ChannelRegistry) Register(channel PushChannel) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.channels[channel.Name()] = channel
}

// Unregister removes a channel from the registry.
func (r *ChannelRegistry) Unregister(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.channels, name)
}

// Get returns a channel by name.
func (r *ChannelRegistry) Get(name string) PushChannel {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.channels[name]
}

// Broadcast sends a message to all applicable channels.
func (r *ChannelRegistry) Broadcast(ctx context.Context, sessionID string, msg *PushMessage) error {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var lastErr error
	for _, ch := range r.channels {
		if ch.CanReceive(sessionID) {
			if err := ch.Push(ctx, sessionID, msg); err != nil {
				lastErr = err
			}
		}
	}
	return lastErr
}

// BusPushChannel publishes push notifications via the message bus.
type BusPushChannel struct {
	bus    *bus.MessageBus
	logger *slog.Logger
}

// NewBusPushChannel creates a bus-based push channel.
func NewBusPushChannel(bus *bus.MessageBus, logger *slog.Logger) *BusPushChannel {
	return &BusPushChannel{
		bus:    bus,
		logger: logger,
	}
}

// Name implements PushChannel.
func (c *BusPushChannel) Name() string { return "bus" }

// CanReceive implements PushChannel - bus can always receive.
func (c *BusPushChannel) CanReceive(sessionID string) bool { return true }

// Push implements PushChannel - publishes to session-specific topic.
func (c *BusPushChannel) Push(ctx context.Context, sessionID string, msg *PushMessage) error {
	payload := map[string]any{
		"session_id": sessionID,
		"message":    msg,
	}
	busMsg, err := models.NewBusMessage(models.MessageType(fmt.Sprintf("push.%s", sessionID)), "push-service", payload)
	if err != nil {
		c.logger.Debug("failed to build push notification", "session", sessionID, "error", err)
		return err
	}
	delivered := c.bus.Publish(fmt.Sprintf("push.%s", sessionID), busMsg)
	if delivered == 0 {
		c.logger.Debug("push notification had no subscribers", "session", sessionID)
	}
	return nil
}
