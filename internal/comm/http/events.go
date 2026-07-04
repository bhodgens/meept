// Package http provides the HTTP-side notification event emitter.
package http

import (
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
)

// subscriber wraps a notification channel with a close-once guard.
// Using sync.Once here makes Unsubscribe idempotent: a channel that
// Subscribe returned on a closed emitter (and was therefore already
// closed at issue time) will not panic on the second close attempt.
type subscriber struct {
	ch       chan *NotificationEvent
	closeOnce sync.Once
}

func (s *subscriber) close() {
	s.closeOnce.Do(func() { close(s.ch) })
}

// EventEmitter broadcasts notification events to connected clients.
type EventEmitter struct {
	mu          sync.RWMutex
	subscribers []*subscriber
	buffer      []*NotificationEvent
	maxBuffer   int
	logger      *slog.Logger
	closed      bool
}

// NewEventEmitter creates a new event emitter with the specified buffer size.
func NewEventEmitter(bufferSize int, logger *slog.Logger) *EventEmitter {
	if logger == nil {
		logger = slog.Default()
	}
	return &EventEmitter{
		subscribers: make([]*subscriber, 0),
		buffer:      make([]*NotificationEvent, 0, bufferSize),
		maxBuffer:   bufferSize,
		logger:      logger,
	}
}

// Subscribe adds a new subscriber channel and immediately sends buffered
// events. The returned channel is buffered to prevent blocking the emitter.
//
// If the emitter is already closed, Subscribe returns an already-closed
// channel. That matches the downstream consumer's expected semantics for a
// dead emitter (range loop terminates immediately) and the channel is still
// safe to pass to Unsubscribe, which uses sync.Once to make close idempotent.
func (e *EventEmitter) Subscribe() chan *NotificationEvent {
	e.mu.Lock()
	defer e.mu.Unlock()

	ch := make(chan *NotificationEvent, 100)
	// Wrap in a *subscriber so Unsubscribe's close is idempotent even when
	// Subscribe ran on a closed emitter (we pre-close the channel below).
	sub := &subscriber{ch: ch}

	if e.closed {
		// Pre-close so the consumer's range terminates. sync.Once in
		// sub.close() guarantees Unsubscribe won't double-close.
		sub.close()
		return ch
	}
	e.subscribers = append(e.subscribers, sub)

	// Replay buffered events while still holding the lock. This closes the
	// race where Close() ran between releasing the lock and sending on ch
	// (which would close the channel mid-replay and panic the send). Sends
	// are non-blocking, so the lock is held only for bounded, fast work.
	dropped := 0
	for _, event := range e.buffer {
		select {
		case ch <- event:
		default:
			dropped++
		}
	}
	if dropped > 0 {
		slog.Warn("EventEmitter: dropped events during replay",
			"dropped", dropped,
			"buffer_size", len(e.buffer),
		)
	}

	return ch
}

// Unsubscribe removes a subscriber channel and closes it.
// Removes from the slice BEFORE closing to prevent concurrent write races.
//
// If the channel is not in the active subscriber slice (because Close() or a
// prior Unsubscribe already removed it), Unsubscribe is a no-op — Close()
// will have already closed the channel via the subscriber wrapper, so we
// must not attempt a second close.
func (e *EventEmitter) Unsubscribe(ch chan *NotificationEvent) {
	e.mu.Lock()
	defer e.mu.Unlock()

	for i, sub := range e.subscribers {
		if sub.ch == ch {
			e.subscribers = append(e.subscribers[:i], e.subscribers[i+1:]...)
			sub.close()
			return
		}
	}
	// Not found: Close() already drained and closed the channel, or
	// Unsubscribe was called twice. Either way, nothing to do.
}

// Publish sends a notification event to all subscribers and retains it
// in the buffer for late subscribers. If the emitter has been closed,
// the event is silently dropped.
func (e *EventEmitter) Publish(event *NotificationEvent) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.closed {
		return
	}

	// Add to buffer
	e.buffer = append(e.buffer, event)
	if len(e.buffer) > e.maxBuffer {
		e.buffer = e.buffer[1:]
	}

	// Broadcast to subscribers (non-blocking)
	for _, sub := range e.subscribers {
		select {
		case sub.ch <- event:
		default:
			e.logger.Warn("notification subscriber not consuming", "event", event.Title)
		}
	}

	e.logger.Debug("notification published", "type", event.Type, "title", event.Title)
}

// Close gracefully shuts down the emitter, closing all subscriber channels.
// Publish calls after Close are silently dropped.
func (e *EventEmitter) Close() {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.closed = true
	for _, sub := range e.subscribers {
		sub.close()
	}
	e.subscribers = nil
}

// GetEventsSince returns events from the buffer that occurred at or after t.
func (e *EventEmitter) GetEventsSince(t time.Time) []*NotificationEvent {
	e.mu.RLock()
	defer e.mu.RUnlock()

	var result []*NotificationEvent
	for _, event := range e.buffer {
		eventTime, err := time.Parse(time.RFC3339, event.Timestamp)
		if err != nil {
			continue
		}
		if eventTime.After(t) || eventTime.Equal(t) {
			result = append(result, event)
		}
	}
	return result
}

// generateUUID generates a unique identifier using github.com/google/uuid.
func generateUUID() string {
	return uuid.New().String()
}

// PublishTaskNotification creates and publishes a task-related notification.
func (e *EventEmitter) PublishTaskNotification(taskID, agentID string, notifType NotificationType, title, message string) {
	event := &NotificationEvent{
		ID:        generateUUID(),
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Type:      notifType,
		Title:     title,
		Message:   message,
		Data: map[string]interface{}{
			"task_id":  taskID,
			"agent_id": agentID,
		},
		TaskID:  taskID,
		AgentID: agentID,
	}
	e.Publish(event)
}
