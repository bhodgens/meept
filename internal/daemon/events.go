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

// RateLimiter controls the maximum number of notifications sent per minute,
// per type. It uses a sliding window: timestamps older than 1 minute are
// pruned on every Allow() call. If the remaining count is below maxPerMinute,
// the request is allowed and its timestamp is recorded.
type RateLimiter struct {
	mu           sync.Mutex
	notifications map[string][]time.Time
	maxPerMinute  int
}

// Allow reports whether a notification of the given type should be emitted
// under the current rate limit. Returns false when the limit for that type
// has been reached within the sliding one-minute window.
func (r *RateLimiter) Allow(notifType string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	windowStart := now.Add(-time.Minute)

	// Prune timestamps outside the sliding window.
	times := r.notifications[notifType]
	idx := 0
	for idx < len(times) && times[idx].Before(windowStart) {
		idx++
	}
	if idx > 0 {
		r.notifications[notifType] = times[idx:]
		times = r.notifications[notifType]
	}

	if len(times) >= r.maxPerMinute {
		return false
	}

	r.notifications[notifType] = append(times, now)
	return true
}

// EventEmitter manages notification subscriptions and event distribution.
type EventEmitter struct {
	mu            sync.RWMutex
	subscribers   []*subscriberSlot
	buffer        []*http.NotificationEvent
	maxBuffer     int
	logger        *slog.Logger
	rateLimiter   *RateLimiter
	doNotDisturb  bool
}

// subscriberSlot bundles a subscriber channel with a closed flag so that
// Publish can skip closed subscribers without relying on a panic recover
// (S6-12). All access to a slot's fields must be performed while holding
// the parent EventEmitter's mu.
type subscriberSlot struct {
	ch     chan *http.NotificationEvent
	closed bool
}

// NewRateLimiter creates a rate limiter with the given per-type maximum
// notifications per minute. A maxPerMinute of 0 or less disables rate limiting.
func NewRateLimiter(maxPerMinute int) *RateLimiter {
	if maxPerMinute <= 0 {
		maxPerMinute = 60 // default: 60 per minute
	}
	return &RateLimiter{
		notifications: make(map[string][]time.Time),
		maxPerMinute:  maxPerMinute,
	}
}

// NewEventEmitter creates a new event emitter with the specified buffer size and rate limit.
func NewEventEmitter(bufferSize int, maxRatePerMinute int, logger *slog.Logger) *EventEmitter {
	if logger == nil {
		logger = slog.Default()
	}
	return &EventEmitter{
		subscribers: make([]*subscriberSlot, 0),
		buffer:      make([]*http.NotificationEvent, 0),
		maxBuffer:   bufferSize,
		logger:      logger,
		rateLimiter: NewRateLimiter(maxRatePerMinute),
	}
}

// Ensure EventEmitter implements http.NotificationEmitter interface
var _ http.NotificationEmitter = (*EventEmitter)(nil)

// Subscribe returns a channel that receives notification events.
// The caller must read from the channel to prevent blocking.
func (e *EventEmitter) Subscribe() chan *http.NotificationEvent {
	e.mu.Lock()

	ch := make(chan *http.NotificationEvent, e.maxBuffer)
	slot := &subscriberSlot{ch: ch}
	e.subscribers = append(e.subscribers, slot)

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
		if sub.ch == ch {
			// Mark the slot as closed BEFORE removing the slot. Publish
			// reads this flag under the lock and skips closed slots.
			// The channel is NOT closed here to avoid a race with
			// Publish's send loop, which takes a snapshot under the lock
			// and sends outside the lock. If we closed the channel here,
			// Publish could panic on send-to-closed-channel between the
			// snapshot and the send.
			sub.closed = true
			e.subscribers = append(e.subscribers[:i], e.subscribers[i+1:]...)
			e.logger.Debug("notification subscriber removed")
			return
		}
	}
}

// Publish sends a notification event to all subscribers after rate limiting.
// Rate-limited notifications are silently dropped with a debug log and also
// excluded from the buffer so they do not accumulate.
func (e *EventEmitter) Publish(event *http.NotificationEvent) {
	// Do Not Disturb: short-circuit all notifications. Read the flag
	// without taking the lock — it is a plain bool written only by
	// SetDoNotDisturb from the daemon config path.
	if e.doNotDisturb {
		e.logger.Debug("notification suppressed (do not disturb)", "type", event.Type)
		return
	}

	// Rate limiting: check before acquiring any lock. RateLimiter uses
	// its own internal mutex and never performs I/O under the EventEmitter's lock.
	if e.rateLimiter != nil && !e.rateLimiter.Allow(string(event.Type)) {
		e.logger.Debug("notification rate-limited", "type", event.Type)
		return
	}

	e.mu.Lock()

	// Add to buffer
	e.buffer = append(e.buffer, event)
	if len(e.buffer) > e.maxBuffer {
		// Shift left and null out the dropped slot to allow GC of the
		// pointer (the underlying array is retained by the slice header
		// otherwise).
		copy(e.buffer, e.buffer[1:])
		e.buffer[len(e.buffer)-1] = nil
		e.buffer = e.buffer[:len(e.buffer)-1]
	}

	// Snapshot the subscriber channels and their closed flags under the
	// lock, then send outside the lock. A closed slot is skipped, which
	// removes the previous reliance on recover() for send-on-closed-
	// channel panic safety (S6-12).
	type sendTarget struct {
		ch     chan *http.NotificationEvent
		closed bool
	}
	targets := make([]sendTarget, len(e.subscribers))
	for i, sub := range e.subscribers {
		targets[i] = sendTarget{ch: sub.ch, closed: sub.closed}
	}

	e.mu.Unlock()

	// Send to all subscribers (non-blocking). Skips closed slots.
	for _, t := range targets {
		if t.closed {
			continue
		}
		select {
		case t.ch <- event:
		default:
			// Channel full, skip this subscriber
			e.logger.Warn("notification subscriber channel full, skipping")
		}
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

// SetRateLimit updates the rate limiter's per-type maximum notifications per minute.
// Pass 0 to reset to the default (60). Pass -1 to disable rate limiting entirely.
func (e *EventEmitter) SetRateLimit(maxPerMinute int) {
	if maxPerMinute < 0 {
		e.rateLimiter = nil
	} else {
		e.rateLimiter = NewRateLimiter(maxPerMinute)
	}
}

// SetDoNotDisturb toggles global notification suppression. When true, all
// notifications are silently dropped at the dispatch layer regardless of type,
// priority, or source. The flag is read without locking in Publish because it
// is only written from the daemon config path (single-writer).
func (e *EventEmitter) SetDoNotDisturb(dnd bool) {
	e.mu.Lock()
	e.doNotDisturb = dnd
	e.mu.Unlock()
	if dnd {
		e.logger.Info("do not disturb mode enabled — all notifications suppressed")
	} else {
		e.logger.Info("do not disturb mode disabled — notifications resumed")
	}
}

// IsDoNotDisturb returns the current DND state.
func (e *EventEmitter) IsDoNotDisturb() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.doNotDisturb
}

// PublishNotification publishes a notification with full control over the event fields
// (e.g., session ID, custom data). This is the primary method for session-designation
// notifications such as "waiting_human", "bot_finished", etc.
func (e *EventEmitter) PublishNotification(sessionID string, agentID string, notifType NotificationType, title, message string) {
	event := &http.NotificationEvent{
		ID:        uuid.New().String(),
		Timestamp: time.Now().Format(time.RFC3339),
		Type:      notifType,
		Title:     title,
		Message:   message,
		SessionID: sessionID,
		AgentID:   agentID,
	}
	e.Publish(event)
}
