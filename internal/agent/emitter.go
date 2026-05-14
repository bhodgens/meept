package agent

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/pkg/models"
)

// EventListener is a callback for typed agent events.
type EventListener func(ctx context.Context, event AgentEvent)

// listenerEntry holds a registered listener with its name and async flag.
type listenerEntry struct {
	name     string
	callback EventListener
	async    bool
}

// EventEmitter publishes typed agent events to in-process listeners
// and bridges them to the message bus for system-wide subscribers.
//
// Typed events are the source of truth for agent lifecycle concerns.
// The bus bridge translates them into bus messages so existing subscribers
// continue to work during migration.
type EventEmitter struct {
	mu           sync.RWMutex
	listeners    map[AgentEventType][]listenerEntry
	allListeners []listenerEntry
	bus          *bus.MessageBus
	agentID      string
	logger       *slog.Logger

	// Settlement tracking for async listeners
	pending sync.WaitGroup
}

// NewEventEmitter creates a new EventEmitter for the given agent.
// If bus is nil, the emitter operates without bus bridging (typed listeners only).
func NewEventEmitter(agentID string, b *bus.MessageBus, logger *slog.Logger) *EventEmitter {
	if logger == nil {
		logger = slog.Default()
	}
	return &EventEmitter{
		listeners: make(map[AgentEventType][]listenerEntry),
		bus:       b,
		agentID:   agentID,
		logger:    logger.With("component", "event-emitter", "agent_id", agentID),
	}
}

// On registers a synchronous listener for a specific event type.
// The listener is called inline during Emit. Name must be unique for removal.
func (e *EventEmitter) On(eventType AgentEventType, name string, listener EventListener) {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.listeners[eventType] = append(e.listeners[eventType], listenerEntry{
		name:     name,
		callback: listener,
		async:    false,
	})
}

// OnAsync registers an asynchronous listener for a specific event type.
// The listener runs in a goroutine tracked by the settlement WaitGroup.
// Name must be unique for removal.
func (e *EventEmitter) OnAsync(eventType AgentEventType, name string, listener EventListener) {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.listeners[eventType] = append(e.listeners[eventType], listenerEntry{
		name:     name,
		callback: listener,
		async:    true,
	})
}

// OnAll registers a listener that receives all events.
// The listener is called synchronously during Emit.
func (e *EventEmitter) OnAll(name string, listener EventListener) {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.allListeners = append(e.allListeners, listenerEntry{
		name:     name,
		callback: listener,
		async:    false,
	})
}

// Emit publishes a typed event to all matching listeners and bridges to the bus.
// Sync listeners run inline. Async listeners run in goroutines tracked for settlement.
func (e *EventEmitter) Emit(ctx context.Context, eventType AgentEventType, data AgentEventData) {
	event := AgentEvent{
		Type:      eventType,
		Timestamp: time.Now().UTC(),
		AgentID:   e.agentID,
		Data:      data,
	}
	e.EmitWithFields(ctx, event)
}

// EmitWithFields publishes an event with explicit metadata fields.
// This is used when the caller needs to set ConversationID or Iteration.
func (e *EventEmitter) EmitWithFields(ctx context.Context, event AgentEvent) {
	// Ensure timestamp is set
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now().UTC()
	}
	if event.AgentID == "" {
		event.AgentID = e.agentID
	}

	// Snapshot listeners under read lock
	e.mu.RLock()
	typeListeners := make([]listenerEntry, len(e.listeners[event.Type]))
	copy(typeListeners, e.listeners[event.Type])
	allListeners := make([]listenerEntry, len(e.allListeners))
	copy(allListeners, e.allListeners)
	e.mu.RUnlock()

	// Dispatch to typed listeners
	for _, entry := range typeListeners {
		if entry.async {
			e.pending.Add(1)
			go func(cb EventListener) {
				defer e.pending.Done()
				cb(ctx, event)
			}(entry.callback)
		} else {
			entry.callback(ctx, event)
		}
	}

	// Dispatch to all-event listeners
	for _, entry := range allListeners {
		if entry.async {
			e.pending.Add(1)
			go func(cb EventListener) {
				defer e.pending.Done()
				cb(ctx, event)
			}(entry.callback)
		} else {
			entry.callback(ctx, event)
		}
	}

	// Bridge to bus
	if e.bus != nil {
		e.bridgeToBus(event)
	}
}

// WaitForIdle blocks until all async listeners from recent Emit calls
// have finished processing. Returns ctx.Err() if the context expires.
func (e *EventEmitter) WaitForIdle(ctx context.Context) error {
	done := make(chan struct{})
	go func() {
		e.pending.Wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Off removes a listener by name from all event types and the all-listeners list.
func (e *EventEmitter) Off(name string) {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Remove from per-type listeners
	for eventType, entries := range e.listeners {
		filtered := make([]listenerEntry, 0, len(entries))
		for _, entry := range entries {
			if entry.name != name {
				filtered = append(filtered, entry)
			}
		}
		e.listeners[eventType] = filtered
	}

	// Remove from all-listeners
	filtered := make([]listenerEntry, 0, len(e.allListeners))
	for _, entry := range e.allListeners {
		if entry.name != name {
			filtered = append(filtered, entry)
		}
	}
	e.allListeners = filtered
}

// BusTopic returns the bus topic for a given agent event type.
// Convention: "agent.event.<type>"
func BusTopic(eventType AgentEventType) string {
	return "agent.event." + string(eventType)
}

// bridgeToBus serializes the event and publishes it to the message bus.
func (e *EventEmitter) bridgeToBus(event AgentEvent) {
	payload, err := json.Marshal(event)
	if err != nil {
		e.logger.Warn("failed to marshal event for bus bridge",
			"error", err,
			"type", event.Type,
		)
		return
	}

	msg := &models.BusMessage{
		ID:        generateEventID(),
		Type:      models.MessageTypeEvent,
		Source:    "agent:" + e.agentID,
		Timestamp: event.Timestamp,
		Payload:   payload,
	}

	topic := BusTopic(event.Type)
	delivered := e.bus.Publish(topic, msg)

	// Also publish to legacy topic if mapped
	if legacyTopic, ok := legacyTopicMap[event.Type]; ok {
		e.bus.Publish(legacyTopic, msg)
	}

	if delivered == 0 {
		e.logger.Debug("event published to bus (no subscribers)",
			"topic", topic,
		)
	}
}

// legacyTopicMap maps typed event types to legacy bus topics for backward compatibility.
var legacyTopicMap = map[AgentEventType]string{
	AgentEventTurnStart:           "agent.progress",
	AgentEventToolExecutionStart:  "agent.action",
	AgentEventToolExecutionEnd:    "agent.result",
	AgentEventAfterProviderResponse: "llm.tokens.used",
}

// generateEventID creates a unique event ID.
func generateEventID() string {
	return time.Now().UTC().Format("20060102150405.000000000")
}
