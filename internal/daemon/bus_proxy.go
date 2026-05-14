// Package daemon provides the main daemon lifecycle management.
package daemon

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/pkg/models"
)

// BusProxyAdapter implements http.BusProxy using the message bus directly.
// It maps RPC method names to bus topics the same way ProxyHandler does.
type BusProxyAdapter struct {
	bus     *bus.MessageBus
	pending sync.Map
}

// methodToTopics maps RPC method names to (requestTopic, responseTopic).
var methodToTopics = map[string][2]string{
	"chat":                         {"chat.request", "chat.response"},
	"status":                       {"status.request", "status.response"},
	"memory.query":                 {"memory.query", "memory.result"},
	"memory.recent":                {"memory.recent", "memory.result"},
	"memory.export":                {"memory.export", "memory.result"},
	"scheduler.list_jobs":          {"scheduler.list_jobs", "scheduler.result"},
	"scheduler.add_job":            {"scheduler.add_job", "scheduler.result"},
	"config.reload":                {"config.reload", "config.result"},
	"security.query_log":           {"security.query_log", "security.result"},
	"security.get_stats":           {"security.get_stats", "security.result"},
	"security.record_override":     {"security.record_override", "security.result"},
	"skills.list":                  {"skills.list", "skills.result"},
	"skills.get":                   {"skills.get", "skills.result"},
	"skills.execute":               {"skills.execute", "skills.result"},
	"skills.triage":                {"skills.triage", "skills.result"},
	"agent.workers.list":           {"agent.workers.list", "agent.workers.result"},
	"session.create":               {"session.create", "session.result"},
	"session.list":                 {"session.list", "session.result"},
	"session.get":                  {"session.get", "session.result"},
	"session.attach":               {"session.attach", "session.result"},
	"session.detach":               {"session.detach", "session.result"},
	"session.delete":               {"session.delete", "session.result"},
	"session.messages.save":        {"session.messages.save", "session.result"},
	"session.messages.get":         {"session.messages.get", "session.result"},
	"session.update_description":   {"session.update_description", "session.result"},
	"session.generate_description": {"session.generate_description", "session.result"},
	"task.create":                  {"task.create", "task.result"},
	"task.get":                     {"task.get", "task.result"},
	"task.list":                    {"task.list", "task.result"},
	"task.list_extended":           {"task.list_extended", "task.result"},
	"task.update":                  {"task.update", "task.result"},
	"task.cancel":                  {"task.cancel", "task.result"},
	"task.delete":                  {"task.delete", "task.result"},
	"task.link":                    {"task.link", "task.result"},
	"task.unlink":                  {"task.unlink", "task.result"},
	"task.steps":                   {"task.steps", "task.result"},
	"queue.enqueue":                {"queue.enqueue", "queue.result"},
	"queue.claim":                  {"queue.claim", "queue.result"},
	"queue.complete":               {"queue.complete", "queue.result"},
	"queue.fail":                   {"queue.fail", "queue.result"},
	"queue.retry":                  {"queue.retry", "queue.result"},
	"queue.get":                    {"queue.get", "queue.result"},
	"queue.list":                   {"queue.list", "queue.result"},
	"queue.stats":                  {"queue.stats", "queue.result"},
	"worker.add":                   {"worker.add", "worker.result"},
	"worker.remove":                {"worker.remove", "worker.result"},
	"worker.list":                  {"worker.list", "worker.result"},
	"worker.stats":                 {"worker.stats", "worker.result"},
	"worker.scale":                 {"worker.scale", "worker.result"},
	"pipeline.status":              {"pipeline.status", "pipeline.result"},
	"cache.stats":                  {"cache.stats", "cache.result"},
	"cache.clear":                  {"cache.clear", "cache.result"},
	"cache.invalidate":             {"cache.invalidate", "cache.result"},
	"selfimprove.detect":           {"selfimprove.detect", "selfimprove.result"},
	"selfimprove.analyze":          {"selfimprove.analyze", "selfimprove.result"},
	"selfimprove.generate":         {"selfimprove.generate", "selfimprove.result"},
	"selfimprove.validate":         {"selfimprove.validate", "selfimprove.result"},
	"selfimprove.apply":            {"selfimprove.apply", "selfimprove.result"},
	"selfimprove.reject":           {"selfimprove.reject", "selfimprove.result"},
	"selfimprove.status":           {"selfimprove.status", "selfimprove.result"},
	"selfimprove.cycle":            {"selfimprove.cycle", "selfimprove.result"},
}

// NewBusProxyAdapter creates a new bus proxy adapter.
func NewBusProxyAdapter(msgBus *bus.MessageBus) *BusProxyAdapter {
	return &BusProxyAdapter{bus: msgBus}
}

// Call sends a method call to the bus and waits for a response.
func (b *BusProxyAdapter) Call(method string, params json.RawMessage) (json.RawMessage, error) {
	topics, ok := methodToTopics[method]
	if !ok {
		return nil, fmt.Errorf("unknown method: %s", method)
	}

	requestTopic := topics[0]
	responseTopic := topics[1]
	timeout := 10 * time.Second
	if method == "chat" {
		timeout = 120 * time.Second
	} else if strings.Contains(method, "session.generate_description") {
		timeout = 20 * time.Second
	}

	msgID := fmt.Sprintf("http-%d", time.Now().UnixNano())
	msg := &models.BusMessage{
		ID:      msgID,
		Type:    models.MessageTypeRequest,
		Topic:   requestTopic,
		Source:  "http.proxy",
		Payload: params,
		ReplyTo: responseTopic,
	}

	respChan := make(chan *models.BusMessage, 1)
	b.pending.Store(msgID, respChan)
	defer b.pending.Delete(msgID)

	sub := b.bus.Subscribe(msgID, responseTopic)
	defer b.bus.Unsubscribe(sub)

	done := make(chan struct{})
	defer close(done)

	go func() {
		for {
			select {
			case resp, ok := <-sub.Channel:
				if !ok {
					return
				}
				if resp.ReplyTo == msgID {
					select {
					case respChan <- resp:
					default:
					}
					return
				}
			case <-done:
				return
			}
		}
	}()

	b.bus.Publish(requestTopic, msg)

	select {
	case resp := <-respChan:
		return resp.Payload, nil
	case <-time.After(timeout):
		return nil, fmt.Errorf("timeout waiting for response on %s", responseTopic)
	}
}
