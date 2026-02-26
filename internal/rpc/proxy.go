package rpc

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/caimlas/meept/internal/bus"
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
	Events     []*busEventRecord
	MaxEvents  int
	mu         sync.Mutex
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
	server.RegisterHandler("memory.export", p.makeProxy("memory.export", "memory.result", 10*time.Second))

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

	// Skills methods
	server.RegisterHandler("skills.list", p.makeProxy("skills.list", "skills.result", 10*time.Second))
	server.RegisterHandler("skills.get", p.makeProxy("skills.get", "skills.result", 10*time.Second))
	server.RegisterHandler("skills.execute", p.makeProxy("skills.execute", "skills.result", 120*time.Second))
	server.RegisterHandler("skills.triage", p.makeProxy("skills.triage", "skills.result", 10*time.Second))

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
	server.RegisterHandler("task.update", p.makeProxy("task.update", "task.result", 10*time.Second))
	server.RegisterHandler("task.delete", p.makeProxy("task.delete", "task.result", 10*time.Second))
	server.RegisterHandler("task.link", p.makeProxy("task.link", "task.result", 10*time.Second))
	server.RegisterHandler("task.unlink", p.makeProxy("task.unlink", "task.result", 10*time.Second))

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

	// Self-improvement methods
	server.RegisterHandler("selfimprove.detect", p.makeProxy("selfimprove.detect", "selfimprove.result", 60*time.Second))
	server.RegisterHandler("selfimprove.analyze", p.makeProxy("selfimprove.analyze", "selfimprove.result", 120*time.Second))
	server.RegisterHandler("selfimprove.generate", p.makeProxy("selfimprove.generate", "selfimprove.result", 120*time.Second))
	server.RegisterHandler("selfimprove.validate", p.makeProxy("selfimprove.validate", "selfimprove.result", 300*time.Second))
	server.RegisterHandler("selfimprove.apply", p.makeProxy("selfimprove.apply", "selfimprove.result", 60*time.Second))
	server.RegisterHandler("selfimprove.status", p.makeProxy("selfimprove.status", "selfimprove.result", 10*time.Second))
	server.RegisterHandler("selfimprove.cycle", p.makeProxy("selfimprove.cycle", "selfimprove.result", 600*time.Second))

	// Bus subscription methods for TUI event streaming
	server.RegisterHandler("bus.subscribe", p.handleBusSubscribe)
	server.RegisterHandler("bus.poll", p.handleBusPoll)
	server.RegisterHandler("bus.unsubscribe", p.handleBusUnsubscribe)
}

// makeProxy creates a handler that forwards to requestTopic and waits on responseTopic.
func (p *ProxyHandler) makeProxy(requestTopic, responseTopic string, timeout time.Duration) Handler {
	return func(ctx context.Context, params json.RawMessage) (any, error) {
		// Create request message
		msgID := fmt.Sprintf("proxy-%d", time.Now().UnixNano())
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
		select {
		case resp := <-respChan:
			var result any
			if err := json.Unmarshal(resp.Payload, &result); err != nil {
				return resp.Payload, nil // Return raw if can't unmarshal
			}
			return result, nil
		case <-time.After(timeout):
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
			ID:      fmt.Sprintf("fire-%d", time.Now().UnixNano()),
			Type:    models.MessageTypeEvent,
			Topic:   topic,
			Source:  "rpc.proxy",
			Payload: params,
		}
		delivered := p.bus.Publish(topic, msg)
		return map[string]any{
			"status":    "published",
			"topic":     topic,
			"delivered": delivered,
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
	subID := fmt.Sprintf("sub-%d", time.Now().UnixNano())

	// Create internal subscription state
	sub := &busSubscription{
		ID:        subID,
		Topics:    req.Topics,
		Events:    make([]*busEventRecord, 0),
		MaxEvents: 100, // Keep last 100 events
	}

	// Subscribe to all topics (using wildcard support)
	// We use a combined subscriber that receives all matching topics
	combinedTopic := "tui.sub." + subID
	subscriber := p.bus.Subscribe(subID, combinedTopic)
	sub.Subscriber = subscriber

	// Start goroutine to collect events from all topics
	go func() {
		for _, topic := range req.Topics {
			topicSub := p.bus.Subscribe(subID+"-"+topic, topic)
			go func(ts *bus.Subscriber) {
				for msg := range ts.Channel {
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
					// Trim to max size
					if len(sub.Events) > sub.MaxEvents {
						sub.Events = sub.Events[len(sub.Events)-sub.MaxEvents:]
					}
					sub.mu.Unlock()
				}
			}(topicSub)
		}
	}()

	p.subscriptions.Store(subID, sub)

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
	if sub.Subscriber != nil {
		p.bus.Unsubscribe(sub.Subscriber)
	}

	p.subscriptions.Delete(req.SubscriptionID)

	return map[string]any{
		"status": "unsubscribed",
	}, nil
}
