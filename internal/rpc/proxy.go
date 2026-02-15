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
	bus     *bus.MessageBus
	pending sync.Map // map[string]chan *models.BusMessage
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
	server.RegisterHandler("skills.triage", p.makeProxy("skills.triage", "skills.result", 10*time.Second))

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

		// Start goroutine to watch for responses
		go func() {
			for resp := range sub.Channel {
				if resp.ReplyTo == msgID {
					select {
					case respChan <- resp:
					default:
					}
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
