// Package http provides the HTTP-side notification event emitter.
package http

import (
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
)

// EventEmitter broadcasts notification events to connected clients.
type EventEmitter struct {
	mu          sync.RWMutex
	subscribers []chan *NotificationEvent
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
		subscribers: make([]chan *NotificationEvent, 0),
		buffer:      make([]*NotificationEvent, 0, bufferSize),
		maxBuffer:   bufferSize,
		logger:      logger,
	}
}

// Subscribe adds a new subscriber channel and immediately sends buffered
// events. The returned channel is buffered to prevent blocking the emitter.
func (e *EventEmitter) Subscribe() chan *NotificationEvent {
	e.mu.Lock()
	ch := make(chan *NotificationEvent, 100)
	// Copy buffer under lock to avoid holding lock during replay
	bufferCopy := make([]*NotificationEvent, len(e.buffer))
	copy(bufferCopy, e.buffer)
	e.subscribers = append(e.subscribers, ch)
	e.mu.Unlock()

	// Send buffered events outside lock using non-blocking sends
	dropped := 0
	for _, event := range bufferCopy {
		select {
		case ch <- event:
		default:
			// Channel full, skip and count dropped
			dropped++
		}
	}
	if dropped > 0 {
		// Log warning if events were dropped during replay
		// Note: using slog directly since emitter may not have logger
		slog.Warn("EventEmitter: dropped events during replay",
			"dropped", dropped,
			"buffer_size", len(bufferCopy),
		)
	}

	return ch
}

// Unsubscribe removes a subscriber channel and closes it.
// Removes from the slice BEFORE closing to prevent concurrent write races.
func (e *EventEmitter) Unsubscribe(ch chan *NotificationEvent) {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Remove from slice FIRST
	for i, sub := range e.subscribers {
		if sub == ch {
			e.subscribers = append(e.subscribers[:i], e.subscribers[i+1:]...)
			break
		}
	}

	// Close AFTER removing to prevent concurrent write to closed channel
	close(ch)
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
		case sub <- event:
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
		close(sub)
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
