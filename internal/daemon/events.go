// Package daemon provides the daemon-side notification event system.
package daemon

import (
	"log/slog"
	"sync"
	"time"

	"github.com/caimlas/meept/internal/comm/http"
	"github.com/google/uuid"
)

// NotificationType represents the type of notification.
type NotificationType = http.NotificationType

const (
	NotificationTypeInfo    NotificationType = http.NotificationTypeInfo
	NotificationTypeSuccess NotificationType = http.NotificationTypeSuccess
	NotificationTypeWarning NotificationType = http.NotificationTypeWarning
	NotificationTypeError   NotificationType = http.NotificationTypeError
)

// NotificationEvent represents a notification event sent to clients.
// This is an alias to the http package's NotificationEvent type.
type NotificationEvent = http.NotificationEvent

// EventEmitter manages notification subscriptions and event distribution.
type EventEmitter struct {
	mu          sync.RWMutex
	subscribers []chan *http.NotificationEvent
	buffer      []*http.NotificationEvent
	maxBuffer   int
	logger      *slog.Logger
}

// NewEventEmitter creates a new event emitter with the specified buffer size.
func NewEventEmitter(bufferSize int, logger *slog.Logger) *EventEmitter {
	if logger == nil {
		logger = slog.Default()
	}
	return &EventEmitter{
		subscribers: make([]chan *http.NotificationEvent, 0),
		buffer:      make([]*http.NotificationEvent, 0),
		maxBuffer:   bufferSize,
		logger:      logger,
	}
}

// Ensure EventEmitter implements http.NotificationEmitter interface
var _ http.NotificationEmitter = (*EventEmitter)(nil)

// Subscribe returns a channel that receives notification events.
// The caller must read from the channel to prevent blocking.
func (e *EventEmitter) Subscribe() chan *http.NotificationEvent {
	e.mu.Lock()

	ch := make(chan *http.NotificationEvent, e.maxBuffer)
	e.subscribers = append(e.subscribers, ch)

	// Copy buffer under lock to avoid blocking sends while holding the lock.
	buffer := make([]*http.NotificationEvent, len(e.buffer))
	copy(buffer, e.buffer)

	e.mu.Unlock()

	// Replay buffered events outside the lock using non-blocking sends.
	for _, event := range buffer {
		select {
		case ch <- event:
		default:
			// Drop event if subscriber channel is full
		}
	}

	e.logger.Debug("new notification subscriber added", "buffer_size", e.maxBuffer)
	return ch
}

// Unsubscribe removes a subscriber channel from the emitter.
func (e *EventEmitter) Unsubscribe(ch chan *http.NotificationEvent) {
	e.mu.Lock()
	defer e.mu.Unlock()

	for i, sub := range e.subscribers {
		if sub == ch {
			e.subscribers = append(e.subscribers[:i], e.subscribers[i+1:]...)
			close(ch)
			e.logger.Debug("notification subscriber removed")
			return
		}
	}
}

// Publish sends a notification event to all subscribers.
func (e *EventEmitter) Publish(event *http.NotificationEvent) {
	e.mu.Lock()

	// Add to buffer
	e.buffer = append(e.buffer, event)
	if len(e.buffer) > e.maxBuffer {
		e.buffer = e.buffer[1:]
	}

	subscribers := make([]chan *http.NotificationEvent, len(e.subscribers))
	copy(subscribers, e.subscribers)

	e.mu.Unlock()

	// Send to all subscribers (non-blocking)
	for _, ch := range subscribers {
		func() {
			defer func() {
				if r := recover(); r != nil {
					// Channel was closed by Unsubscribe, that's ok
					e.logger.Debug("notification channel closed during publish", "recover", r)
				}
			}()
			select {
			case ch <- event:
			default:
				// Channel full, skip this subscriber
				e.logger.Warn("notification subscriber channel full, skipping")
			}
		}()
	}
}

// PublishTaskNotification publishes a task-related notification.
func (e *EventEmitter) PublishTaskNotification(taskID, agentID string, notifType NotificationType, title, message string) {
	event := &http.NotificationEvent{
		ID:        uuid.New().String(),
		Timestamp: time.Now().Format(time.RFC3339),
		Type:      notifType,
		Title:     title,
		Message:   message,
		TaskID:    taskID,
		AgentID:   agentID,
	}
	e.Publish(event)
}

// GetEventsSince returns all events since the given timestamp.
func (e *EventEmitter) GetEventsSince(t time.Time) []*http.NotificationEvent {
	e.mu.RLock()
	defer e.mu.RUnlock()

	var events []*http.NotificationEvent
	for _, event := range e.buffer {
		eventTime, err := time.Parse(time.RFC3339, event.Timestamp)
		if err != nil {
			e.logger.Warn("failed to parse event timestamp", "timestamp", event.Timestamp, "error", err)
			continue
		}
		if eventTime.After(t) || eventTime.Equal(t) {
			events = append(events, event)
		}
	}
	return events
}