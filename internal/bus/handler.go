package bus

import (
	"context"
	"log/slog"
	"sync"
)

// MessageCallback is invoked when a message arrives on a subscribed topic.
type MessageCallback func(ctx context.Context, topic string, msg interface{})

// SubscriptionHandler manages bus subscriptions with automatic lifecycle
// management. It handles subscribe/dispatch/teardown to eliminate
// duplicated handler boilerplate.
type SubscriptionHandler struct {
	bus       *MessageBus
	callbacks map[string]MessageCallback  // topic → callback
	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup
	logger    *slog.Logger
}

// NewSubscriptionHandler creates a new handler
func NewSubscriptionHandler(bus *MessageBus, logger *slog.Logger) *SubscriptionHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &SubscriptionHandler{
		bus:       bus,
		callbacks: make(map[string]MessageCallback),
		logger:    logger,
	}
}

// Subscribe adds a topic→callback mapping
func (h *SubscriptionHandler) Subscribe(topic string, callback MessageCallback) {
	h.callbacks[topic] = callback
}

// Start begins listening to all subscribed topics
func (h *SubscriptionHandler) Start(parentCtx context.Context) {
	h.ctx, h.cancel = context.WithCancel(parentCtx)

	for topic := range h.callbacks {
		sub := h.bus.Subscribe("handler-"+topic, topic)
		h.wg.Add(1)
		go h.handleTopic(h.ctx, sub, topic)
	}

	h.logger.Debug("Subscription handler started", "topics", len(h.callbacks))
}

// Stop gracefully shuts down all subscription goroutines
func (h *SubscriptionHandler) Stop() {
	if h.cancel != nil {
		h.cancel()
	}
	h.wg.Wait()
	h.logger.Debug("Subscription handler stopped")
}

// handleTopic runs the subscription loop for a single topic
func (h *SubscriptionHandler) handleTopic(ctx context.Context, sub *Subscriber, topic string) {
	defer h.wg.Done()
	// Ensure unsubscribe is called in all exit paths
	defer h.bus.Unsubscribe(sub)

	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-sub.Channel:
			if !ok {
				// Channel closed by bus
				return
			}
			callback := h.callbacks[topic]
			if callback != nil {
				callback(ctx, topic, msg)
			}
		}
	}
}
