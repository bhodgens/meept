package rpc

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
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

	// Memory methods — memory.query, memory.recent, memory.export and
	// memory.vector.search are covered by direct handlers registered in
	// internal/daemon/memory_rpc.go (daemon.go) or live bus subscribers
	// (internal/memory/handler.go subscribes memory.query/memory.recent).
	// Dead proxies for memory.vector.stats and memory.export were removed:
	// nothing subscribed to "memory.result" for them, so calls timed out
	// instead of returning method-not-found. memory.vector.stats now has a
	// direct handler wrapping MemoryService.VectorStats.
	server.RegisterHandler("memory.query", p.makeProxy("memory.query", "memory.result", 30*time.Second))
	server.RegisterHandler("memory.recent", p.makeProxy("memory.recent", "memory.result", 10*time.Second))

	// Scheduler methods — the full scheduler surface (list_jobs, add_job,
	// status, etc.) is registered as direct Go handlers by
	// scheduler.RegisterRPCHandlers (internal/scheduler/rpc.go, daemon.go).
	// The dead scheduler.* proxies (list_jobs/add_job/schedule_agent_task)
	// were removed: they were fully shadowed by the direct handlers, and
	// schedule_agent_task additionally proxied to the WRONG request topic
	// (published "scheduler.add_job" on the bus, which has zero bus
	// subscribers) — any caller would have blocked 10s then timed out.
	// "scheduler.result" has no bus subscriber. If the schedule_agent_task
	// API name is wanted, register it as a direct alias for handler.AddJob
	// in internal/scheduler/rpc.go.

	// Config methods
	server.RegisterHandler("config.reload", p.makeFireAndForget("config.reload"))

	// Security methods — query_log/get_stats/record_override/approve_action
	// proxies were removed: no component subscribes to "security.result" (or
	// handles approve_action), so every call timed out for 10s and then
	// failed. security.Engine (the would-be backend) has no callers, so
	// there is nothing real to wire these to; delete them rather than leave
	// 10s-timeout traps. Direct check methods are registered by
	// SecurityHandler.RegisterSecurityMethods (internal/rpc/security.go).

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
	server.RegisterHandler("sessions.designated", p.makeProxy("sessions.designated", "session.result", 10*time.Second))
	server.RegisterHandler("session.get_most_recent", p.makeProxy("session.get_most_recent", "session.result", 10*time.Second))
	server.RegisterHandler("session.stop", p.makeProxy("session.stop", "session.result", 10*time.Second))
	server.RegisterHandler("session.refresh_title", p.makeProxy("session.refresh_title", "session.result", 60*time.Second))
	server.RegisterHandler("session.set_nofence", p.makeProxy("session.set_nofence", "session.result", 10*time.Second))
	server.RegisterHandler("session.set_foreground", p.makeProxy("session.set_foreground", "session.result", 10*time.Second))
	server.RegisterHandler("session.set_last_user_message", p.makeProxy("session.set_last_user_message", "session.result", 10*time.Second))
	server.RegisterHandler("session.get_child_tasks", p.makeProxy("session.get_child_tasks", "session.result", 10*time.Second))
	server.RegisterHandler("session.branch.navigate", p.makeProxy("session.branch.navigate", "session.result", 10*time.Second))
	server.RegisterHandler("session.branches.list", p.makeProxy("session.branches.list", "session.result", 10*time.Second))
	server.RegisterHandler("session.fork", p.makeProxy("session.fork", "session.result", 20*time.Second))
	server.RegisterHandler("session.tree.get", p.makeProxy("session.tree.get", "session.result", 10*time.Second))

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
	server.RegisterHandler("queue.recover", p.makeProxy("queue.recover", "queue.result", 30*time.Second))
	server.RegisterHandler("queue.dead_letter", p.makeProxy("queue.dead_letter", "queue.result", 10*time.Second))
	server.RegisterHandler("queue.dead_stats", p.makeProxy("queue.dead_stats", "queue.result", 10*time.Second))

	// Worker methods
	server.RegisterHandler("worker.add", p.makeProxy("worker.add", "worker.result", 10*time.Second))
	server.RegisterHandler("worker.remove", p.makeProxy("worker.remove", "worker.result", 10*time.Second))
	server.RegisterHandler("worker.list", p.makeProxy("worker.list", "worker.result", 10*time.Second))
	server.RegisterHandler("worker.stats", p.makeProxy("worker.stats", "worker.result", 10*time.Second))
	server.RegisterHandler("worker.scale", p.makeProxy("worker.scale", "worker.result", 10*time.Second))

	// Pipeline methods — the pipeline.status proxy was removed: nothing
	// subscribes to "pipeline.result" and no component handles the request,
	// so callers blocked 10s and then got a timeout error. The CLI learning
	// status command (cmd/meept/learning.go) reads local files directly and
	// never called this method. services.PipelineService.Status exists but is
	// not constructed anywhere, so there is nothing real to wire to.
	// Re-add only together with a real pipeline status backend.

	// Cache methods — direct handlers in internal/rpc/cache.go are
	// registered unconditionally in daemon.go (RegisterCacheMethods), so
	// these proxies were fully shadowed and unreachable. The dead proxy
	// registrations were removed; "cache.result" has no bus subscriber.
	// With the proxies gone a nil TokenCache yields "method not found"
	// from handleStats rather than a 10s timeout.

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

// isCriticalTopic returns true for topics whose messages must never be
// silently dropped due to a full subscriber buffer.
// C-09 FIX: security approvals and related events are routed via
// makeFireAndForget; without this guard they hit the non-blocking Publish
// path and are silently discarded when buffers fill.
func isCriticalTopic(topic string) bool {
	return strings.HasPrefix(topic, "security.") ||
		strings.HasPrefix(topic, "approval.")
}

// makeFireAndForget creates a handler that publishes to a topic without waiting.
// For security-critical topic prefixes (security.*, approval.*), it uses
// PublishBlocking to guarantee delivery even when subscriber buffers are full.
// C-09 FIX: previously all fire-and-forget calls used non-blocking Publish,
// causing security approval messages to be silently dropped on full buffers.
func (p *ProxyHandler) makeFireAndForget(topic string) Handler {
	return func(ctx context.Context, params json.RawMessage) (any, error) {
		msg := &models.BusMessage{
			ID:      id.Generate("fire-"),
			Type:    models.MessageTypeEvent,
			Topic:   topic,
			Source:  "rpc.proxy",
			Payload: params,
		}
		// C-09 FIX: route critical topics through PublishBlocking so they
		// are not silently dropped when subscriber buffers are full.
		var delivered int
		if isCriticalTopic(topic) {
			delivered = p.bus.PublishBlocking(topic, msg)
		} else {
			delivered = p.bus.Publish(topic, msg)
		}
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

	// Subscriptions must outlive the subscribe RPC that created them. When
	// the connection-done channel is available we watch ONLY that channel:
	// ctx (opCtx) is request-scoped and is cancelled the moment this handler
	// returns, so selecting on it here killed every subscription immediately
	// after bus.subscribe returned — bus.poll then found no subscription
	// ("subscription not found") and bench/TUI event traces stayed empty.
	// Without connDoneCh (direct proxy calls, not via server.dispatch) we
	// have no disconnect signal, so fall back to ctx.Done() as before.
	if connDoneCh != nil {
		go func() {
			<-connDoneCh
			cancelFunc()
		}()
	} else {
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
	// This goroutine monitors context cancellation for cleanup.
	// Note: handleBusUnsubscribe also performs synchronous cleanup, so this
	// goroutine acts as a fallback for client disconnects where the explicit
	// unsubscribe call is never made.
	go func() {
		// Wait for context cancellation (client disconnect)
		<-subCtx.Done()

		// Unsubscribe the combined subscriber (tui.sub.<subID>)
		if sub.Subscriber != nil {
			p.bus.Unsubscribe(sub.Subscriber)
			sub.Subscriber = nil
		}

		// Unsubscribe all topic subscriptions
		sub.mu.Lock()
		for _, ts := range sub.TopicSubs {
			p.bus.Unsubscribe(ts)
		}
		sub.TopicSubs = nil
		sub.mu.Unlock()

		// Unsubscribe the combined subscriber (tui.sub.X) as well so it
		// doesn't leak on the bus when the client disconnects without
		// calling bus.unsubscribe. handleBusUnsubscribe already does
		// this explicitly on the synchronous path; this covers the
		// async disconnect path.
		if sub.Subscriber != nil {
			p.bus.Unsubscribe(sub.Subscriber)
			sub.Subscriber = nil
		}

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
						if err := json.Unmarshal(msg.Payload, &payload); err == nil { //nolint:mutexio // unmarshal into local; mutex guards sub.Events append
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
	if sub.cancelFunc != nil {
		sub.cancelFunc()
	}

	// Unsubscribe the combined subscriber
	if sub.Subscriber != nil {
		p.bus.Unsubscribe(sub.Subscriber)
	}

	// Synchronously remove topic subscriptions and delete from the map
	// so that after this call returns, the subscription is fully gone.
	sub.mu.Lock()
	for _, ts := range sub.TopicSubs {
		p.bus.Unsubscribe(ts)
	}
	sub.TopicSubs = nil
	sub.mu.Unlock()

	p.subscriptions.Delete(req.SubscriptionID)

	return map[string]any{
		RPCKeyStatus: "unsubscribed",
	}, nil
}
