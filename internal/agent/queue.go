package agent

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/pkg/models"
	"github.com/google/uuid"
)

// QueueType identifies which queue a message belongs to.
type QueueType string

const (
	QueueTypeSteer    QueueType = "steer"
	QueueTypeFollowUp QueueType = "follow_up"
)

// DrainMode controls how a follow-up queue drains its messages.
type DrainMode string

const (
	DrainAll DrainMode = "all"
	DrainOne DrainMode = "one"
)

// Default QueueConfig values.
const (
	DefaultMaxSteering       = 1
	DefaultMaxFollowUp       = 20
	DefaultPersistFollowUp   = true
	DefaultSteeringDrain     = DrainOne
	DefaultFollowUpDrain     = DrainOne
)

// QueuedMessage represents a single message enqueued for the agent loop.
type QueuedMessage struct {
	ID        string    `json:"id"`
	Content   string    `json:"content"`
	QueueType QueueType `json:"queue_type"`
	Timestamp time.Time `json:"timestamp"`
	Source    string    `json:"source"`
}

// QueueConfig holds configuration for both steering and follow-up queues.
type QueueConfig struct {
	// Steering drain mode - always DrainOne, stored for API consistency.
	SteeringDrain DrainMode `json:"steering_drain"`
	// FollowUp drain mode: "one" returns a single message, "all" returns all pending.
	FollowUpDrain DrainMode `json:"follow_up_drain"`
	// MaxSteering is the maximum number of steering messages (enforced: 1).
	MaxSteering int `json:"max_steering"`
	// MaxFollowUp is the maximum number of follow-up messages allowed.
	MaxFollowUp int `json:"max_follow_up"`
	// PersistFollowUp controls whether pending follow-ups are persisted on close.
	PersistFollowUp bool `json:"persist_follow_up"`
	// FlushDelayMs is the write-behind flush delay in milliseconds.
	FlushDelayMs int `json:"flush_delay_ms"`
}

// DefaultQueueConfig returns a QueueConfig with sensible defaults.
func DefaultQueueConfig() QueueConfig {
	return QueueConfig{
		SteeringDrain:     DrainOne,
		FollowUpDrain:     DrainOne,
		MaxSteering:       DefaultMaxSteering,
		MaxFollowUp:       DefaultMaxFollowUp,
		PersistFollowUp:   DefaultPersistFollowUp,
		FlushDelayMs:      5000,
	}
}

// ParseDrainMode converts a config string to DrainMode.
func ParseDrainMode(s string) DrainMode {
	switch s {
	case "all":
		return DrainAll
	default:
		return DrainOne
	}
}

// QueueStatus is a snapshot of the queue's current state.
type QueueStatus struct {
	SteeringDepth int           `json:"steering_depth"`
	FollowUpDepth int           `json:"follow_up_depth"`
	IsActive      bool          `json:"is_active"`
	Generation    uint64        `json:"generation"`
}

// QueueEventPayload is attached to bus events when messages are added.
type QueueEventPayload struct {
	ConversationID string `json:"conversation_id"`
	QueueType      string `json:"queue_type"`
	MessageID      string `json:"message_id"`
	ContentPreview string `json:"content_preview"`
	Source         string `json:"source"`
	QueueDepth     int    `json:"queue_depth"`
}

// QueueInjectedPayload is attached to bus events when the agent loop injects
// queued messages into a conversation.
type QueueInjectedPayload struct {
	ConversationID string      `json:"conversation_id"`
	QueueType      QueueType   `json:"queue_type"`
	Count          int         `json:"count"`
	MessageIDs     []string    `json:"message_ids"`
	Iteration      int         `json:"iteration"`
}

// AgentLifecyclePayload is attached to bus events for agent lifecycle tracking.
type AgentLifecyclePayload struct {
	ConversationID string `json:"conversation_id"`
	AgentID        string `json:"agent_id"`
	Reason         string `json:"reason,omitempty"`
}

// MessageQueue is a thread-safe dual-queue for steering and follow-up messages.
type MessageQueue struct {
	mu            sync.Mutex
	steeringQueue []QueuedMessage
	followUpQueue []QueuedMessage

	notifyCh chan struct{}

	generation uint64
	closed     atomic.Bool

	config  QueueConfig
	bus     *bus.MessageBus
	agentID string
	logger  Logger

	persister QueuePersisterOps
}

// QueuePersisterOps is the subset of QueuePersister used by MessageQueue.
type QueuePersisterOps interface {
	PersistSync(msg QueuedMessage) error
}

// QueueRestorePayload is published to the bus when pending follow-ups are
// recovered from SQLite on daemon startup.
type QueueRestorePayload struct {
	ConversationID string `json:"conversation_id"`
	Count          int    `json:"count"`
}

// MessageQueueOption configures a MessageQueue.
type MessageQueueOption func(*MessageQueue)

// WithQueueConfig sets the queue configuration.
func WithQueueConfig(cfg QueueConfig) MessageQueueOption {
	return func(q *MessageQueue) {
		q.config = cfg
	}
}

// WithQueueBus sets the message bus.
func WithQueueBus(b *bus.MessageBus) MessageQueueOption {
	return func(q *MessageQueue) {
		q.bus = b
	}
}

// WithQueueAgentID sets the owning agent ID.
func WithQueueAgentID(id string) MessageQueueOption {
	return func(q *MessageQueue) {
		q.agentID = id
	}
}

// WithQueueLogger sets the logger.
func WithQueueLogger(l Logger) MessageQueueOption {
	return func(q *MessageQueue) {
		q.logger = l
	}
}

// WithQueuePersister sets the persistence backend.
func WithQueuePersister(p QueuePersisterOps) MessageQueueOption {
	return func(q *MessageQueue) {
		q.persister = p
	}
}

// NewMessageQueue creates a new MessageQueue.
func NewMessageQueue(opts ...MessageQueueOption) *MessageQueue {
	q := &MessageQueue{
		steeringQueue: make([]QueuedMessage, 0, 1),
		followUpQueue: make([]QueuedMessage, 0, DefaultMaxFollowUp),
		notifyCh:      make(chan struct{}, 1),
		config:        DefaultQueueConfig(),
	}

	for _, opt := range opts {
		opt(q)
	}

	if q.logger == nil {
		q.logger = &noopLogger{}
	}

	return q
}

// Steer injects a steering message into the steering queue.
// If a steering message already exists, it is replaced (latest wins).
// Returns ErrQueueClosed if the queue is closed, ErrQueueFull if at capacity.
func (q *MessageQueue) Steer(ctx context.Context, content, source string) error {
	if q.closed.Load() {
		return ErrQueueClosed
	}

	msg := QueuedMessage{
		ID:        uuid.NewString(),
		Content:   content,
		QueueType: QueueTypeSteer,
		Timestamp: time.Now().UTC(),
		Source:    source,
	}

	q.mu.Lock()
	// Replace existing steering message (keep only the latest).
	if len(q.steeringQueue) > 0 {
		q.steeringQueue[0] = msg
	} else {
		q.steeringQueue = append(q.steeringQueue, msg)
	}
	q.generation++
	depth := len(q.steeringQueue)
	q.mu.Unlock()

	q.notifyNonBlocking()
	q.publishEvent(bus.EventQueueSteerAdded, QueueEventPayload{
		ContentPreview: previewContent(content),
		QueueType:      string(QueueTypeSteer),
		MessageID:      msg.ID,
		Source:         source,
		QueueDepth:     depth,
	})

	q.logger.Debug("steering message injected", "id", msg.ID, "source", source)
	return nil
}

// FollowUp injects a follow-up message into the follow-up queue.
// Returns ErrQueueClosed if the queue is closed, ErrQueueFull if at capacity.
func (q *MessageQueue) FollowUp(ctx context.Context, content, source string) error {
	if q.closed.Load() {
		return ErrQueueClosed
	}

	q.mu.Lock()

	if len(q.followUpQueue) >= q.config.MaxFollowUp {
		q.mu.Unlock()
		return ErrQueueFull
	}

	msg := QueuedMessage{
		ID:        uuid.NewString(),
		Content:   content,
		QueueType: QueueTypeFollowUp,
		Timestamp: time.Now().UTC(),
		Source:    source,
	}

	q.followUpQueue = append(q.followUpQueue, msg)
	q.generation++
	depth := len(q.followUpQueue)

	// Persist async if persister is available.
	if q.persister != nil {
		go func() {
			if err := q.persister.PersistSync(msg); err != nil {
				q.logger.Warn("failed to persist follow-up", "id", msg.ID, "err", err)
			}
		}()
	}

	q.mu.Unlock()

	q.notifyNonBlocking()
	q.publishEvent(bus.EventQueueFollowUpAdded, QueueEventPayload{
		ContentPreview: previewContent(content),
		QueueType:      string(QueueTypeFollowUp),
		MessageID:      msg.ID,
		Source:         source,
		QueueDepth:     depth,
	})

	q.logger.Debug("follow-up message injected", "id", msg.ID, "source", source)
	return nil
}

// DrainSteering returns at most one steering message (FIFO within the single-slot queue).
// Returns an empty slice if there are no steering messages.
func (q *MessageQueue) DrainSteering() []QueuedMessage {
	q.mu.Lock()
	defer q.mu.Unlock()

	if len(q.steeringQueue) == 0 {
		return nil
	}

	msg := q.steeringQueue[0]
	q.steeringQueue = q.steeringQueue[:0]
	q.generation++

	return []QueuedMessage{msg}
}

// DrainFollowUp returns follow-up messages according to the DrainMode.
// DrainOne: returns at most one message.
// DrainAll: returns all pending messages.
func (q *MessageQueue) DrainFollowUp() []QueuedMessage {
	q.mu.Lock()
	defer q.mu.Unlock()

	if len(q.followUpQueue) == 0 {
		return nil
	}

	var drained []QueuedMessage

	switch q.config.FollowUpDrain {
	case DrainOne:
		drained = []QueuedMessage{q.followUpQueue[0]}
		q.followUpQueue = q.followUpQueue[1:]
	default: // DrainAll
		drained = make([]QueuedMessage, len(q.followUpQueue))
		copy(drained, q.followUpQueue)
		q.followUpQueue = q.followUpQueue[:0]
	}

	q.generation++

	return drained
}

// HasSteering returns true if the steering queue has pending messages.
func (q *MessageQueue) HasSteering() bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.steeringQueue) > 0
}

// HasFollowUp returns true if the follow-up queue has pending messages.
func (q *MessageQueue) HasFollowUp() bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.followUpQueue) > 0
}

// IsClosed returns true if the queue has been closed.
func (q *MessageQueue) IsClosed() bool {
	return q.closed.Load()
}

// GetGeneration returns the current generation counter.
func (q *MessageQueue) GetGeneration() uint64 {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.generation
}

// Close marks the queue as closed. It persists pending follow-ups if configured.
func (q *MessageQueue) Close() {
	if !q.closed.CompareAndSwap(false, true) {
		return // already closed
	}

	// Persist pending follow-ups before closing.
	if q.config.PersistFollowUp && q.persister != nil {
		q.mu.Lock()
		pending := make([]QueuedMessage, len(q.followUpQueue))
		copy(pending, q.followUpQueue)
		q.mu.Unlock()

		for _, msg := range pending {
			if err := q.persister.PersistSync(msg); err != nil {
				q.logger.Warn("failed to persist follow-up on close", "id", msg.ID, "err", err)
			}
		}
	}

	q.logger.Info("message queue closed")
}

// Status returns a snapshot of the queue's current state.
func (q *MessageQueue) Status() QueueStatus {
	q.mu.Lock()
	defer q.mu.Unlock()

	return QueueStatus{
		SteeringDepth: len(q.steeringQueue),
		FollowUpDepth: len(q.followUpQueue),
		IsActive:      !q.closed.Load(),
		Generation:    q.generation,
	}
}

// notifyNonBlocking sends a signal to the notify channel without blocking.
func (q *MessageQueue) notifyNonBlocking() {
	select {
	case q.notifyCh <- struct{}{}:
	default:
		// Already signaled, no need to block.
	}
}

// publishEvent publishes a queued event to the message bus.
func (q *MessageQueue) publishEvent(topic string, payload QueueEventPayload) {
	if q.bus == nil {
		return
	}

	msg, err := models.NewBusMessage(models.MessageTypeEvent, "queue", payload)
	if err != nil {
		q.logger.Warn("failed to marshal queue event", "topic", topic, "err", err)
		return
	}

	_ = q.bus.Publish(topic, msg)
}

// previewContent returns the first maxChars characters of s, truncated if longer.
func previewContent(s string) string {
	const maxChars = 100
	if len(s) <= maxChars {
		return s
	}
	return s[:maxChars] + "..."
}

// noopLogger is a no-op implementation of Logger for when no logger is configured.
type noopLogger struct{}

func (noopLogger) Debug(_ string, _ ...any) {}
func (noopLogger) Warn(_ string, _ ...any)  {}
func (noopLogger) Error(_ string, _ ...any) {}
func (noopLogger) Info(_ string, _ ...any)  {}
