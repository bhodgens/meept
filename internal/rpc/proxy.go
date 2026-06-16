package rpc

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/pkg/id"
	"github.com/caimlas/meept/pkg/models"
)

// ProxyHandler forwards RPC requests to the message bus and waits for responses.
// This enables Python agents to handle RPC methods by subscribing to bus topics.
type ProxyHandler struct {
	bus           *bus.MessageBus
	pending       sync.Map // map[string]chan *models.BusMessage
	subscriptions sync.Map // map[string]*busSubscription for TUI event streaming
}

// busSubscription holds state for a bus subscription.
type busSubscription struct {
	ID         string
	Topics     []string
	Subscriber *bus.Subscriber
	TopicSubs  []*bus.Subscriber // per-topic subscribers
	Events     []*busEventRecord
	MaxEvents  int
	mu         sync.Mutex
	cancelFunc context.CancelFunc // cancels goroutines when client disconnects
}

// busEventRecord is an event captured for polling.
type busEventRecord struct {
	Topic     string    `json:"topic"`
	Type      string    `json:"type"`
	Source    string    `json:"source"`
	Timestamp time.Time `json:"timestamp"`
	Payload   any       `json:"payload"`
}

// NewProxyHandler creates a new proxy handler.
func NewProxyHandler(msgBus *bus.MessageBus) *ProxyHandler {
	return &ProxyHandler{bus: msgBus}
}

// RegisterProxyMethods registers all proxy methods that forward to the bus.
func (p *ProxyHandler) RegisterProxyMethods(server *Server) {
	// Chat methods
	server.RegisterHandler("chat", p.makeProxy("chat.request", "chat.response", 120*time.Second))

	// Status methods
	server.RegisterHandler("status", p.makeProxy("status.request", "status.response", 10*time.Second))

	// Memory methods
	server.RegisterHandler("memory.query", p.makeProxy("memory.query", "memory.result", 30*time.Second))
	server.RegisterHandler("memory.recent", p.makeProxy("memory.recent", "memory.result", 10*time.Second))
	server.RegisterHandler("memory.export", p.makeProxy("memory.export", "memory.result", 10*time.Second))
	server.RegisterHandler("memory.vector.search", p.makeProxy("memory.vector.search", "memory.result", 30*time.Second))
	server.RegisterHandler("memory.vector.stats", p.makeProxy("memory.vector.stats", "memory.result", 10*time.Second))

	// Scheduler methods
	server.RegisterHandler("scheduler.list_jobs", p.makeProxy("scheduler.list_jobs", "scheduler.result", 10*time.Second))
	server.RegisterHandler("scheduler.add_job", p.makeProxy("scheduler.add_job", "scheduler.result", 10*time.Second))
	server.RegisterHandler("scheduler.schedule_agent_task", p.makeProxy("scheduler.add_job", "scheduler.result", 10*time.Second))

	// Config methods
	server.RegisterHandler("config.reload", p.makeFireAndForget("config.reload"))

	// Security methods
	server.RegisterHandler("security.query_log", p.makeProxy("security.query_log", "security.result", 10*time.Second))
	server.RegisterHandler("security.get_stats", p.makeProxy("security.get_stats", "security.result", 10*time.Second))
	server.RegisterHandler("security.record_override", p.makeProxy("security.record_override", "security.result", 10*time.Second))
	server.RegisterHandler("security.approve_action", p.makeFireAndForget("security.approve_action"))

	// Note: skills methods are NOT proxied here.
	// Direct RPC handlers are registered by RegisterSkillsHandlers (internal/rpc/skills.go)
	// in daemon.go when skills are enabled. When disabled, no handler is registered,
	// and the RPC server will return "method not found" instead of timing out.

	// Agent/Worker methods
	server.RegisterHandler("agent.workers.list", p.makeProxy("agent.workers.list", "agent.workers.result", 10*time.Second))

	// Session methods
	server.RegisterHandler("session.create", p.makeProxy("session.create", "session.result", 10*time.Second))
	server.RegisterHandler("session.list", p.makeProxy("session.list", "session.result", 10*time.Second))
	server.RegisterHandler("session.get", p.makeProxy("session.get", "session.result", 10*time.Second))
	server.RegisterHandler("session.attach", p.makeProxy("session.attach", "session.result", 10*time.Second))
	server.RegisterHandler("session.detach", p.makeProxy("session.detach", "session.result", 10*time.Second))
	server.RegisterHandler("session.delete", p.makeProxy("session.delete", "session.result", 10*time.Second))
	server.RegisterHandler("session.messages.save", p.makeProxy("session.messages.save", "session.result", 10*time.Second))
	server.RegisterHandler("session.messages.get", p.makeProxy("session.messages.get", "session.result", 10*time.Second))
	server.RegisterHandler("session.update_description", p.makeProxy("session.update_description", "session.result", 10*time.Second))
	server.RegisterHandler("session.generate_description", p.makeProxy("session.generate_description", "session.result", 20*time.Second))

	// Task methods
	server.RegisterHandler("task.create", p.makeProxy("task.create", "task.result", 10*time.Second))
	server.RegisterHandler("task.get", p.makeProxy("task.get", "task.result", 10*time.Second))
	server.RegisterHandler("task.list", p.makeProxy("task.list", "task.result", 10*time.Second))
	server.RegisterHandler("task.list_extended", p.makeProxy("task.list_extended", "task.result", 10*time.Second))
	server.RegisterHandler("task.update", p.makeProxy("task.update", "task.result", 10*time.Second))
	server.RegisterHandler("task.cancel", p.makeProxy("task.cancel", "task.result", 10*time.Second))
	server.RegisterHandler("task.delete", p.makeProxy("task.delete", "task.result", 10*time.Second))
	server.RegisterHandler("task.link", p.makeProxy("task.link", "task.result", 10*time.Second))
	server.RegisterHandler("task.unlink", p.makeProxy("task.unlink", "task.result", 10*time.Second))
	server.RegisterHandler("task.steps", p.makeProxy("task.steps", "task.result", 10*time.Second))

	// Queue methods
	server.RegisterHandler("queue.enqueue", p.makeProxy("queue.enqueue", "queue.result", 10*time.Second))
	server.RegisterHandler("queue.claim", p.makeProxy("queue.claim", "queue.result", 10*time.Second))
	server.RegisterHandler("queue.complete", p.makeProxy("queue.complete", "queue.result", 10*time.Second))
	server.RegisterHandler("queue.fail", p.makeProxy("queue.fail", "queue.result", 10*time.Second))
	server.RegisterHandler("queue.retry", p.makeProxy("queue.retry", "queue.result", 10*time.Second))
	server.RegisterHandler("queue.get", p.makeProxy("queue.get", "queue.result", 10*time.Second))
	server.RegisterHandler("queue.list", p.makeProxy("queue.list", "queue.result", 10*time.Second))
	server.RegisterHandler("queue.stats", p.makeProxy("queue.stats", "queue.result", 10*time.Second))

	// Worker methods
	server.RegisterHandler("worker.add", p.makeProxy("worker.add", "worker.result", 10*time.Second))
	server.RegisterHandler("worker.remove", p.makeProxy("worker.remove", "worker.result", 10*time.Second))
	server.RegisterHandler("worker.list", p.makeProxy("worker.list", "worker.result", 10*time.Second))
	server.RegisterHandler("worker.stats", p.makeProxy("worker.stats", "worker.result", 10*time.Second))
	server.RegisterHandler("worker.scale", p.makeProxy("worker.scale", "worker.result", 10*time.Second))

	// Pipeline methods
	server.RegisterHandler("pipeline.status", p.makeProxy("pipeline.status", "pipeline.result", 10*time.Second))

	// Cache methods
	server.RegisterHandler("cache.stats", p.makeProxy("cache.stats", "cache.result", 10*time.Second))
	server.RegisterHandler("cache.clear", p.makeProxy("cache.clear", "cache.result", 10*time.Second))
	server.RegisterHandler("cache.invalidate", p.makeProxy("cache.invalidate", "cache.result", 10*time.Second))

	// Self-improvement methods are registered as native Go handlers by
	// SelfImproveHandler (see selfimprove.go) because the Controller lives
	// inside the Go daemon and does not need a bus proxy round-trip.

	// Bus subscription methods for TUI event streaming
	server.RegisterHandler("bus.subscribe", p.handleBusSubscribe)
	server.RegisterHandler("bus.poll", p.handleBusPoll)
	server.RegisterHandler("bus.unsubscribe", p.handleBusUnsubscribe)
}

// makeProxy creates a handler that forwards to requestTopic and waits on responseTopic.
func (p *ProxyHandler) makeProxy(requestTopic, responseTopic string, timeout time.Duration) Handler {
	return func(ctx context.Context, params json.RawMessage) (any, error) {
		// Create request message
		msgID := id.Generate("proxy-")
		msg := &models.BusMessage{
			ID:      msgID,
			Type:    models.MessageTypeRequest,
			Topic:   requestTopic,
			Source:  "rpc.proxy",
			Payload: params,
			ReplyTo: responseTopic,
		}

		// Create response channel
		respChan := make(chan *models.BusMessage, 1)
		p.pending.Store(msgID, respChan)
		defer p.pending.Delete(msgID)

		// Subscribe to response topic
		sub := p.bus.Subscribe(msgID, responseTopic)
		defer p.bus.Unsubscribe(sub)

		// Done channel signals watcher goroutine to exit
		done := make(chan struct{})
		defer close(done)

		// Start goroutine to watch for responses
		// This goroutine is context-aware and will exit when:
		// 1. A matching response is received
		// 2. The subscription channel is closed
		// 3. The context is cancelled (client disconnected)
		// 4. The done channel is closed (function returns)
		go func() {
			for {
				select {
				case resp, ok := <-sub.Channel:
					if !ok {
						// Subscription channel closed
						return
					}
					if resp.ReplyTo == msgID {
						// FIX #0038: Validate response topic to prevent cross-talk
						if resp.Topic != "" && resp.Topic != responseTopic {
							slog.Debug("proxy: discarding response from wrong topic",
								"expected", responseTopic,
								"actual", resp.Topic,
								"msgID", msgID,
							)
							continue
						}
						select {
						case respChan <- resp:
						default:
						}
						return
					}
				case <-ctx.Done():
					// Context cancelled (client disconnected)
					return
				case <-done:
					// Parent function returning
					return
				}
			}
		}()

		// Publish request
		p.bus.Publish(requestTopic, msg)

		// Wait for response
		timer := time.NewTimer(timeout)
		defer timer.Stop()

		select {
		case resp := <-respChan:
			var result any
			if err := json.Unmarshal(resp.Payload, &result); err != nil {
				return resp.Payload, nil // Return raw if can't unmarshal
			}
			return result, nil
		case <-timer.C:
			return nil, fmt.Errorf("timeout waiting for response on %s", responseTopic)
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

// makeFireAndForget creates a handler that publishes to a topic without waiting.
func (p *ProxyHandler) makeFireAndForget(topic string) Handler {
	return func(ctx context.Context, params json.RawMessage) (any, error) {
		msg := &models.BusMessage{
			ID:      id.Generate("fire-"),
			Type:    models.MessageTypeEvent,
			Topic:   topic,
			Source:  "rpc.proxy",
			Payload: params,
		}
		delivered := p.bus.Publish(topic, msg)
		status := "published"
		if delivered == 0 {
			status = "dropped"
		}
		return map[string]any{
			RPCKeyStatus: status,
			"topic":      topic,
			"delivered":  delivered,
		}, nil
	}
}

// handleBusSubscribe creates a subscription to one or more bus topics.
func (p *ProxyHandler) handleBusSubscribe(ctx context.Context, params json.RawMessage) (any, error) {
	var req struct {
		Topics []string `json:"topics"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}

	if len(req.Topics) == 0 {
		return nil, fmt.Errorf("no topics specified")
	}

	// Create subscription ID
	subID := id.Generate("sub-")

	// Extract the connection-scoped done channel injected by server.dispatch.
	// This allows us to create a subscription context that is cancelled when
	// the client disconnects, preventing subscription leaks (Bug C8).
	connDoneCh, _ := ctx.Value(connectionDoneKey{}).(<-chan struct{})

	subCtx, cancelFunc := context.WithCancel(context.Background())

	// When the client disconnects, connDoneCh fires → cancel the subscription
	// context → cleanup goroutine stops all subscriber goroutines and removes
	// the subscription from the map.  We also listen on ctx.Done() (the
	// request-timeout context) as a fallback: if the request handler returns
	// for any reason, cancelFunc fires so we don't leave dangling goroutines.
	if connDoneCh != nil {
		go func() {
			select {
			case <-connDoneCh:
				cancelFunc()
			case <-ctx.Done():
				cancelFunc()
			}
		}()
	} else {
		// Fallback for direct proxy calls (not via server.dispatch): cancel on
		// request context done.
		go func() {
			<-ctx.Done()
			cancelFunc()
		}()
	}

	// Create internal subscription state
	sub := &busSubscription{
		ID:         subID,
		Topics:     req.Topics,
		Events:     make([]*busEventRecord, 0),
		MaxEvents:  100, // Keep last 100 events
		TopicSubs:  make([]*bus.Subscriber, 0, len(req.Topics)),
		cancelFunc: cancelFunc,
	}

	// Subscribe to all topics (using wildcard support)
	// We use a combined subscriber that receives all matching topics
	combinedTopic := "tui.sub." + subID
	subscriber := p.bus.Subscribe(subID, combinedTopic)
	sub.Subscriber = subscriber

	// Start goroutine to collect events from all topics
	// This goroutine monitors context cancellation for cleanup
	go func() {
		// Wait for context cancellation (client disconnect)
		<-subCtx.Done()

		// Unsubscribe all topic subscriptions
		sub.mu.Lock()
		for _, ts := range sub.TopicSubs {
			p.bus.Unsubscribe(ts)
		}
		sub.TopicSubs = nil
		sub.mu.Unlock()

		// Remove from subscriptions map
		p.subscriptions.Delete(subID)
		slog.Debug("Cleaned up subscription on context cancellation", "subscription_id", subID)
	}()

	// Start collector goroutines for each topic
	for _, topic := range req.Topics {
		slog.Debug("Creating bus subscription for TUI", "subscription_id", subID, "topic", topic)
		topicSub := p.bus.Subscribe(subID+"-"+topic, topic)

		sub.mu.Lock()
		sub.TopicSubs = append(sub.TopicSubs, topicSub)
		sub.mu.Unlock()

		go func(ts *bus.Subscriber, topicName string) {
			slog.Debug("Started event collector for topic", "topic", topicName)
			for {
				select {
				case <-subCtx.Done():
					slog.Debug("Event collector stopped by context", "topic", topicName)
					return
				case msg, ok := <-ts.Channel:
					if !ok {
						slog.Debug("Event collector stopped for topic", "topic", topicName)
						return
					}
					slog.Debug("TUI subscription received event",
						"subscription_id", subID,
						"subscribed_topic", topicName,
						"msg_topic", msg.Topic,
						"msg_source", msg.Source,
					)
					sub.mu.Lock()
					event := &busEventRecord{
						Topic:     msg.Topic,
						Type:      string(msg.Type),
						Source:    msg.Source,
						Timestamp: time.Now(),
					}
					// Parse payload
					if msg.Payload != nil {
						var payload any
						if err := json.Unmarshal(msg.Payload, &payload); err == nil {
							event.Payload = payload
						}
					}
					sub.Events = append(sub.Events, event)
					slog.Debug("Added event to subscription buffer",
						"subscription_id", subID,
						"event_count", len(sub.Events),
					)
					// Trim to max size
					if len(sub.Events) > sub.MaxEvents {
						sub.Events = sub.Events[len(sub.Events)-sub.MaxEvents:]
					}
					sub.mu.Unlock()
				}
			}
		}(topicSub, topic)
	}

	p.subscriptions.Store(subID, sub)

	slog.Info("Created TUI event subscription",
		"subscription_id", subID,
		"topics", req.Topics,
	)

	return map[string]any{
		"subscription_id": subID,
		"topics":          req.Topics,
	}, nil
}

// handleBusPoll returns events since the last poll.
func (p *ProxyHandler) handleBusPoll(ctx context.Context, params json.RawMessage) (any, error) {
	var req struct {
		SubscriptionID string `json:"subscription_id"`
		Since          string `json:"since"` // RFC3339 timestamp
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}

	subVal, ok := p.subscriptions.Load(req.SubscriptionID)
	if !ok {
		slog.Debug("Poll for unknown subscription", "subscription_id", req.SubscriptionID)
		return nil, fmt.Errorf("subscription not found: %s", req.SubscriptionID)
	}

	sub := subVal.(*busSubscription)

	// Parse since timestamp
	var since time.Time
	if req.Since != "" {
		var err error
		since, err = time.Parse(time.RFC3339Nano, req.Since)
		if err != nil {
			since = time.Time{} // Return all events if parsing fails
		}
	}

	// Collect events since timestamp
	sub.mu.Lock()
	defer sub.mu.Unlock()

	events := make([]*busEventRecord, 0)
	for _, e := range sub.Events {
		if e.Timestamp.After(since) {
			events = append(events, e)
		}
	}

	if len(events) > 0 {
		slog.Debug("Poll returning events",
			"subscription_id", req.SubscriptionID,
			"total_buffered", len(sub.Events),
			"events_returned", len(events),
		)
	}

	return map[string]any{
		"events": events,
	}, nil
}

// handleBusUnsubscribe removes a subscription.
func (p *ProxyHandler) handleBusUnsubscribe(ctx context.Context, params json.RawMessage) (any, error) {
	var req struct {
		SubscriptionID string `json:"subscription_id"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}

	subVal, ok := p.subscriptions.Load(req.SubscriptionID)
	if !ok {
		return nil, fmt.Errorf("subscription not found: %s", req.SubscriptionID)
	}

	sub := subVal.(*busSubscription)

	// Cancel the context to trigger cleanup of goroutines.
	// The cleanup goroutine handles unsubscribing topic subs and deleting
	// from the map, so we only need to cancel here + unsubscribe the
	// combined subscriber.
	if sub.cancelFunc != nil {
		sub.cancelFunc()
	}

	// Unsubscribe the combined subscriber
	if sub.Subscriber != nil {
		p.bus.Unsubscribe(sub.Subscriber)
	}

	return map[string]any{
		RPCKeyStatus: "unsubscribed",
	}, nil
}
