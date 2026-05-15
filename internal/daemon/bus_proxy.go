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

// Bus response topic constants.
const (
	TopicSkillsResult      = "skills.result"
	TopicSessionResult     = "session.result"
	TopicTaskResult        = "task.result"
	TopicQueueResult       = "queue.result"
	TopicWorkerResult      = "worker.result"
	TopicSelfImproveResult = "selfimprove.result"
)

// Map key and state value constants.
const (
	KeyStatus    = "status"
	StateRunning = "running"
)

// methodToTopics maps RPC method names to (requestTopic, responseTopic).
var methodToTopics = map[string][2]string{
	"chat":                         {"chat.request", "chat.response"},
	KeyStatus:                     {"status.request", "status.response"},
	"memory.query":                 {"memory.query", "memory.result"},
	"memory.recent":                {"memory.recent", "memory.result"},
	"memory.export":                {"memory.export", "memory.result"},
	"scheduler.list_jobs":          {"scheduler.list_jobs", "scheduler.result"},
	"scheduler.add_job":            {"scheduler.add_job", "scheduler.result"},
	"config.reload":                {"config.reload", "config.result"},
	"security.query_log":           {"security.query_log", "security.result"},
	"security.get_stats":           {"security.get_stats", "security.result"},
	"security.record_override":     {"security.record_override", "security.result"},
	"skills.list":                  {"skills.list", TopicSkillsResult},
	"skills.get":                   {"skills.get", TopicSkillsResult},
	"skills.execute":               {"skills.execute", TopicSkillsResult},
	"skills.triage":                {"skills.triage", TopicSkillsResult},
	"agent.workers.list":           {"agent.workers.list", "agent.workers.result"},
	"session.create":               {"session.create", TopicSessionResult},
	"session.list":                 {"session.list", TopicSessionResult},
	"session.get":                  {"session.get", TopicSessionResult},
	"session.attach":               {"session.attach", TopicSessionResult},
	"session.detach":               {"session.detach", TopicSessionResult},
	"session.delete":               {"session.delete", TopicSessionResult},
	"session.messages.save":        {"session.messages.save", TopicSessionResult},
	"session.messages.get":         {"session.messages.get", TopicSessionResult},
	"session.update_description":   {"session.update_description", TopicSessionResult},
	"session.generate_description": {"session.generate_description", TopicSessionResult},
	"task.create":                  {"task.create", TopicTaskResult},
	"task.get":                     {"task.get", TopicTaskResult},
	"task.list":                    {"task.list", TopicTaskResult},
	"task.list_extended":           {"task.list_extended", TopicTaskResult},
	"task.update":                  {"task.update", TopicTaskResult},
	"task.cancel":                  {"task.cancel", TopicTaskResult},
	"task.delete":                  {"task.delete", TopicTaskResult},
	"task.link":                    {"task.link", TopicTaskResult},
	"task.unlink":                  {"task.unlink", TopicTaskResult},
	"task.steps":                   {"task.steps", TopicTaskResult},
	"queue.enqueue":                {"queue.enqueue", TopicQueueResult},
	"queue.claim":                  {"queue.claim", TopicQueueResult},
	"queue.complete":               {"queue.complete", TopicQueueResult},
	"queue.fail":                   {"queue.fail", TopicQueueResult},
	"queue.retry":                  {"queue.retry", TopicQueueResult},
	"queue.get":                    {"queue.get", TopicQueueResult},
	"queue.list":                   {"queue.list", TopicQueueResult},
	"queue.stats":                  {"queue.stats", TopicQueueResult},
	"worker.add":                   {"worker.add", TopicWorkerResult},
	"worker.remove":                {"worker.remove", TopicWorkerResult},
	"worker.list":                  {"worker.list", TopicWorkerResult},
	"worker.stats":                 {"worker.stats", TopicWorkerResult},
	"worker.scale":                 {"worker.scale", TopicWorkerResult},
	"pipeline.status":              {"pipeline.status", "pipeline.result"},
	"cache.stats":                  {"cache.stats", "cache.result"},
	"cache.clear":                  {"cache.clear", "cache.result"},
	"cache.invalidate":             {"cache.invalidate", "cache.result"},
	"selfimprove.detect":           {"selfimprove.detect", TopicSelfImproveResult},
	"selfimprove.analyze":          {"selfimprove.analyze", TopicSelfImproveResult},
	"selfimprove.generate":         {"selfimprove.generate", TopicSelfImproveResult},
	"selfimprove.validate":         {"selfimprove.validate", TopicSelfImproveResult},
	"selfimprove.apply":            {"selfimprove.apply", TopicSelfImproveResult},
	"selfimprove.reject":           {"selfimprove.reject", TopicSelfImproveResult},
	"selfimprove.status":           {"selfimprove.status", TopicSelfImproveResult},
	"selfimprove.cycle":            {"selfimprove.cycle", TopicSelfImproveResult},
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
