package agent

import (
	"database/sql"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/pkg/models"
)

// Boot-recovery inbox registry (02-routing/03 T2). Boot recovery must
// re-enqueue persisted follow-up CONTENT into the session's live inbox
// before deleting the durable rows; with no inbox registered the rows
// survive for the next boot / reconnect (crash-safety ordering).
var bootRecoveryInboxRegistry = struct {
	mu      sync.Mutex
	inboxes map[string]*MessageQueue
}{inboxes: make(map[string]*MessageQueue)}

// SetBootRecoveryInbox registers the session inbox boot recovery
// re-enqueues through for convID. A nil queue unregisters the key ONLY
// when no live inbox is registered (a turn-end unregister of an older
// queue generation must not drop a newer live registration).
func SetBootRecoveryInbox(convID string, q *MessageQueue) {
	bootRecoveryInboxRegistry.mu.Lock()
	defer bootRecoveryInboxRegistry.mu.Unlock()
	if q == nil {
		if cur, ok := bootRecoveryInboxRegistry.inboxes[convID]; !ok || cur == nil || cur.IsClosed() {
			delete(bootRecoveryInboxRegistry.inboxes, convID)
		}
		return
	}
	bootRecoveryInboxRegistry.inboxes[convID] = q
}

// SetBootRecoveryInbox is the per-queue registration seam: the queue
// registers itself as the boot inbox for its conversation.
func (q *MessageQueue) SetBootRecoveryInbox(convID string, inbox *MessageQueue) {
	SetBootRecoveryInbox(convID, inbox)
}

// bootRecoveryInboxFor resolves the inbox for a conversation: exact key
// first, then thread-unwrapped base (C3 resolution order).
func bootRecoveryInboxFor(convID string) *MessageQueue {
	bootRecoveryInboxRegistry.mu.Lock()
	if q, ok := bootRecoveryInboxRegistry.inboxes[convID]; ok && q != nil && !q.IsClosed() {
		bootRecoveryInboxRegistry.mu.Unlock()
		return q
	}
	bootRecoveryInboxRegistry.mu.Unlock()
	// Thread-unwrapped base (C3 resolution order): a thread-scoped key
	// falls back to the session-owned base inbox.
	if base := unwrapThreadConversationID(convID); base != convID {
		bootRecoveryInboxRegistry.mu.Lock()
		q := bootRecoveryInboxRegistry.inboxes[base]
		bootRecoveryInboxRegistry.mu.Unlock()
		if q != nil && !q.IsClosed() {
			return q
		}
	}
	return nil
}

// RecoverPendingFollowUps loads all conversations that have persisted
// follow-up messages and re-enqueues their FULL content into the
// session's live inbox (02-routing/03 T2). Durable rows are deleted only
// AFTER successful injection; a boot with no registered inbox leaves
// the rows intact so the next boot / reconnect still delivers them
// exactly once. The restore event fires only for actually-injected
// conversations and its count reflects the injected messages.
func RecoverPendingFollowUps(db *sql.DB, msgBus *bus.MessageBus, logger *slog.Logger) {
	query := `
		SELECT DISTINCT conversation_id
		FROM queued_followups
		WHERE conversation_id IS NOT NULL
		ORDER BY conversation_id
	`

	rows, err := db.Query(query)
	if err != nil {
		logger.Warn("queue recovery: failed to query conversations", "error", err)
		return
	}

	var convIDs []string
	for rows.Next() {
		var convID string
		if err := rows.Scan(&convID); err != nil {
			logger.Warn("queue recovery: failed to scan conversation ID", "error", err)
			continue
		}
		convIDs = append(convIDs, convID)
	}
	rowsErr := rows.Err()
	rows.Close()
	if rowsErr != nil {
		logger.Warn("queue recovery: error iterating conversations", "error", rowsErr)
		return
	}

	if len(convIDs) == 0 {
		return
	}

	logger.Info("queue recovery: found conversations with pending follow-ups", "count", len(convIDs))

	for _, convID := range convIDs {
		inbox := bootRecoveryInboxFor(convID)
		if inbox == nil {
			// No listener: deletion is only legal AFTER successful
			// injection. Keep the durable rows for the next boot.
			logger.Info("queue recovery: no session inbox; keeping persisted follow-ups",
				"conversation", convID)
			continue
		}

		msgs, err := loadFollowups(db, convID)
		if err != nil {
			logger.Warn("queue recovery: failed to load messages", "conversation", convID, "error", err)
			continue
		}
		if len(msgs) == 0 {
			continue
		}

		injected, err := injectRecoveredFollowUps(inbox, msgs)
		if err != nil {
			logger.Warn("queue recovery: injection failed; keeping persisted follow-ups",
				"conversation", convID, "injected", injected, "error", err)
			continue
		}

		// Delete only the injected rows, AFTER successful injection.
		if err := deleteFollowupRows(db, convID, msgs[:injected]); err != nil {
			// Crash window: rows may be redelivered on the next boot;
			// injection dedupes by message id so content never duplicates.
			logger.Warn("queue recovery: failed to delete injected rows",
				"conversation", convID, "error", err)
		}

		logger.Info("queue recovery: restored follow-ups", "conversation", convID, "count", injected)

		payload := QueueRestorePayload{
			ConversationID: convID,
			Count:          injected,
		}
		ev, marshalErr := models.NewBusMessage(models.MessageTypeEvent, "queue", payload)
		if marshalErr != nil {
			logger.Warn("queue recovery: failed to marshal event", "error", marshalErr)
			continue
		}
		msgBus.Publish(bus.EventQueueFollowUpRestored, ev)
	}
}

// injectRecoveredFollowUps re-enqueues the FULL original messages into
// the inbox, preserving id/content/timestamp/source. Messages already
// present (same message id -- the crash-between-inject-and-delete window)
// are skipped so redelivery never duplicates. Returns the number of
// messages now held by the inbox from this batch.
func injectRecoveredFollowUps(q *MessageQueue, msgs []QueuedMessage) (int, error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.closed.Load() {
		return 0, fmt.Errorf("session inbox is closed")
	}

	present := make(map[string]bool, len(q.followUpQueue))
	for _, m := range q.followUpQueue {
		present[m.ID] = true
	}

	injected := 0
	newlyInjected := 0
	for _, msg := range msgs {
		if present[msg.ID] {
			injected++
			continue
		}
		if len(q.followUpQueue) >= q.config.MaxFollowUp {
			return injected, fmt.Errorf("session inbox full after %d messages", injected)
		}
		q.followUpQueue = append(q.followUpQueue, msg)
		present[msg.ID] = true
		injected++
		newlyInjected++
	}
	if newlyInjected > 0 {
		q.bootRecovered += newlyInjected
		q.generation++
		q.notifyNonBlocking()
	}
	return injected, nil
}

// loadFollowups fetches all pending follow-ups for a conversation
// WITHOUT deleting them (deletion happens only after injection).
func loadFollowups(db *sql.DB, convID string) ([]QueuedMessage, error) {
	rows, err := db.Query(`
		SELECT message_id, content, queue_type, source, created_at
		FROM queued_followups
		WHERE conversation_id = ?
		ORDER BY created_at ASC
	`, convID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var msgs []QueuedMessage
	for rows.Next() {
		var msg QueuedMessage
		var createdAt string
		if err := rows.Scan(&msg.ID, &msg.Content, (*string)(&msg.QueueType), &msg.Source, &createdAt); err != nil {
			return nil, err
		}
		msg.Timestamp = parseTimestamp(createdAt)
		msgs = append(msgs, msg)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return msgs, nil
}

// deleteFollowupRows removes exactly the injected rows by message id.
func deleteFollowupRows(db *sql.DB, convID string, msgs []QueuedMessage) error {
	for _, msg := range msgs {
		if _, err := db.Exec(
			`DELETE FROM queued_followups WHERE conversation_id = ? AND message_id = ?`,
			convID, msg.ID); err != nil {
			return fmt.Errorf("delete follow-up %s: %w", msg.ID, err)
		}
	}
	return nil
}

// parseTimestamp tries RFC3339 first then falls back to CURRENT_TIMESTAMP format.
func parseTimestamp(s string) time.Time {
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t
	}
	if t, err := time.Parse("2006-01-02 15:04:05", s); err == nil {
		return t
	}
	return time.Time{}
}
