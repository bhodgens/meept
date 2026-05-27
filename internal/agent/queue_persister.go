package agent

import (
	"database/sql"
	"log/slog"
	"sync"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/pkg/models"
)

const (
	defaultFlushDelay = 5 * time.Second
)

// QueuePersister implements write-behind persistence for follow-up messages.
type QueuePersister struct {
	db             *sql.DB
	bus            *bus.MessageBus
	logger         *slog.Logger
	mu             sync.Mutex
	pending        []QueuedMessage
	flushTimer     *time.Timer
	flushDelay     time.Duration
	conversationID string
	maxPending     int
}

// QueuePersisterConfig holds configuration for QueuePersister.
type QueuePersisterConfig struct {
	FlushDelay time.Duration
	MaxPending int
}

// DefaultQueuePersisterConfig returns defaults.
func DefaultQueuePersisterConfig() QueuePersisterConfig {
	return QueuePersisterConfig{
		FlushDelay: defaultFlushDelay,
		MaxPending: 100,
	}
}

const queuedFollowupsSchema = `
CREATE TABLE IF NOT EXISTS queued_followups (
    conversation_id TEXT NOT NULL,
    message_id      TEXT PRIMARY KEY,
    content         TEXT NOT NULL,
    queue_type      TEXT NOT NULL,
    source          TEXT NOT NULL,
    created_at      DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at      DATETIME DEFAULT CURRENT_TIMESTAMP
);
`

const createQueuedFollowupsIndex = `
CREATE INDEX IF NOT EXISTS idx_queued_followups_conversation
    ON queued_followups(conversation_id);
`

// NewQueuePersister creates a new write-behind persister.
// It initializes the database schema immediately.
func NewQueuePersister(db *sql.DB, conversationID string, logger *slog.Logger) (*QueuePersister, error) {
	if logger == nil {
		logger = slog.Default()
	}

	p := &QueuePersister{
		db:             db,
		logger:         logger,
		flushDelay:     defaultFlushDelay,
		conversationID: conversationID,
		pending:        make([]QueuedMessage, 0),
		maxPending:     DefaultQueuePersisterConfig().MaxPending,
	}

	// Start the flush timer.
	p.flushTimer = time.AfterFunc(defaultFlushDelay, func() {
		p.Flush()
	})

	// Initialize schema.
	if err := p.initSchema(); err != nil {
		return nil, err
	}

	return p, nil
}

// WithBus attaches a message bus for event publishing on persist.
func (p *QueuePersister) WithBus(b *bus.MessageBus) {
	p.bus = b
}

// initSchema creates the necessary tables and indexes.
func (p *QueuePersister) initSchema() error {
	if _, err := p.db.Exec(queuedFollowupsSchema); err != nil {
		return err
	}
	if _, err := p.db.Exec(createQueuedFollowupsIndex); err != nil {
		return err
	}
	return nil
}

// EnqueueAsync buffers a message for later flushing.
// It starts or resets the flush timer (debounced write-behind).
// If the pending buffer reaches MaxPending, an immediate flush is triggered first.
func (p *QueuePersister) EnqueueAsync(msg QueuedMessage) {
	p.mu.Lock()

	if len(p.pending) >= p.maxPending {
		p.logger.Warn("queue persister: max pending reached, flushing",
			"pending", len(p.pending),
			"max_pending", p.maxPending,
		)
		// flushLockedHeld drains pending without acquiring the lock (caller holds it).
		p.flushLockedHeld()
	}

	p.pending = append(p.pending, msg)

	// Reset the flush timer (debounce) — must be inside the lock to prevent
	// concurrent manipulation from the timer goroutine in flushPending.
	if !p.flushTimer.Stop() {
		select {
		case <-p.flushTimer.C:
		default:
		}
	}
	p.flushTimer.Reset(p.flushDelay)

	p.mu.Unlock()
}

// PersistSync immediately inserts a message into SQLite.
func (p *QueuePersister) PersistSync(msg QueuedMessage) error {
	now := time.Now().UTC().Format(time.RFC3339)

	_, err := p.db.Exec(`
		INSERT OR REPLACE INTO queued_followups
			(conversation_id, message_id, content, queue_type, source, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`,
		p.conversationID,
		msg.ID,
		msg.Content,
		string(msg.QueueType),
		msg.Source,
		now,
		now,
	)

	if err != nil {
		p.logger.Warn("queue persister: failed to persist follow-up", "id", msg.ID, "err", err)
		return err
	}

	p.publishPersistedEvent(msg)
	p.logger.Debug("queue persister: persisted follow-up", "id", msg.ID)
	return nil
}

// Flush writes all pending buffered messages to SQLite.
func (p *QueuePersister) Flush() {
	p.mu.Lock()
	if len(p.pending) == 0 {
		p.mu.Unlock()
		return
	}
	pending := p.pending
	p.pending = make([]QueuedMessage, 0)
	p.mu.Unlock()

	p.flushPending(pending)
}

// flushLockedHeld drains the pending buffer. Caller must hold p.mu.
func (p *QueuePersister) flushLockedHeld() {
	if len(p.pending) == 0 {
		return
	}
	pending := p.pending
	p.pending = make([]QueuedMessage, 0)
	p.mu.Unlock()

	p.flushPending(pending)

	// Reacquire lock for the caller (EnqueueAsync expects it held).
	p.mu.Lock()
}

// flushPending writes messages to SQLite, re-enqueuing failures.
func (p *QueuePersister) flushPending(pending []QueuedMessage) {
	for _, msg := range pending {
		if err := p.PersistSync(msg); err == nil {
			continue
		}
		// Re-enqueue on failure so it gets retried on next flush.
		p.mu.Lock()
		p.pending = append(p.pending, msg)

		// Restart the timer for a retry (inside the lock).
		if !p.flushTimer.Stop() {
			select {
			case <-p.flushTimer.C:
			default:
			}
		}
		p.flushTimer.Reset(p.flushDelay)
		p.mu.Unlock()
		break
	}
}

// LoadPending returns all persisted follow-ups for this conversation.
func (p *QueuePersister) LoadPending() ([]QueuedMessage, error) {
	rows, err := p.db.Query(`
		SELECT message_id, content, queue_type, source, created_at
		FROM queued_followups
		WHERE conversation_id = ?
		ORDER BY created_at ASC
	`, p.conversationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var messages []QueuedMessage
	for rows.Next() {
		var msg QueuedMessage
		var createdAt string
		if err := rows.Scan(&msg.ID, &msg.Content, (*string)(&msg.QueueType), &msg.Source, &createdAt); err != nil {
			return nil, err
		}
		msg.Timestamp, _ = time.Parse(time.RFC3339, createdAt)
		messages = append(messages, msg)
	}

	return messages, rows.Err()
}

// ClearPending removes all persisted follow-ups for this conversation.
func (p *QueuePersister) ClearPending() error {
	_, err := p.db.Exec(`
		DELETE FROM queued_followups WHERE conversation_id = ?
	`, p.conversationID)
	return err
}

// Stop halts the flush timer and flushes any remaining pending messages.
func (p *QueuePersister) Stop() {
	if p.flushTimer != nil {
		p.flushTimer.Stop()
	}
	p.Flush()
}

// publishPersistedEvent publishes a bus event when a message is persisted.
func (p *QueuePersister) publishPersistedEvent(msg QueuedMessage) {
	if p.bus == nil {
		return
	}

	payload := QueueEventPayload{
		ConversationID: p.conversationID,
		QueueType:      string(msg.QueueType),
		MessageID:      msg.ID,
		ContentPreview: previewContent(msg.Content),
		Source:         msg.Source,
	}

	ev, err := models.NewBusMessage(models.MessageTypeEvent, "queue", payload)
	if err != nil {
		p.logger.Warn("queue persister: failed to marshal event", "err", err)
		return
	}

	_ = p.bus.Publish(bus.EventQueuePersisted, ev)
}
