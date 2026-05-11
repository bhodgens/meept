package agent

import (
	"database/sql"
	"log/slog"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/pkg/models"
)

// RecoverPendingFollowUps loads all conversations that have persisted
// follow-up messages and publishes a restore event for each one.
//
// This is called once on daemon startup so that any TUI clients listening
// for agent.queue.followup.restored events are notified that history is
// available.
func RecoverPendingFollowUps(db *sql.DB, bus *bus.MessageBus, logger *slog.Logger) {
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
	defer rows.Close()

	var convIDs []string
	for rows.Next() {
		var convID string
		if err := rows.Scan(&convID); err != nil {
			logger.Warn("queue recovery: failed to scan conversation ID", "error", err)
			continue
		}
		convIDs = append(convIDs, convID)
	}
	if err := rows.Err(); err != nil {
		logger.Warn("queue recovery: error iterating conversations", "error", err)
		return
	}

	if len(convIDs) == 0 {
		return
	}

	logger.Info("queue recovery: found conversations with pending follow-ups", "count", len(convIDs))

	for _, convID := range convIDs {
		msgs, err := loadAndClear(db, convID)
		if err != nil {
			logger.Warn("queue recovery: failed to load messages", "conversation", convID, "error", err)
			continue
		}

		if len(msgs) == 0 {
			continue
		}

		logger.Info("queue recovery: restored follow-ups", "conversation", convID, "count", len(msgs))

		// Publish restore event so TUI can show the notification.
		payload := QueueRestorePayload{
			ConversationID: convID,
			Count:          len(msgs),
		}
		ev, marshalErr := models.NewBusMessage(models.MessageTypeEvent, "queue", payload)
		if marshalErr != nil {
			logger.Warn("queue recovery: failed to marshal event", "error", marshalErr)
			continue
		}
		_ = bus.Publish("agent.queue.followup.restored", ev)
	}
}

// loadAndClear fetches all pending follow-ups for a conversation and deletes
// them from the table so they are not returned again on a subsequent recovery.
func loadAndClear(db *sql.DB, convID string) ([]QueuedMessage, error) {
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
		msg.Timestamp, _ = parseTimestamp(createdAt)
		msgs = append(msgs, msg)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Delete consumed rows.
	_, err = db.Exec(`DELETE FROM queued_followups WHERE conversation_id = ?`, convID)
	if err != nil {
		return nil, err
	}

	return msgs, nil
}

// parseTimestamp tries RFC3339 first then falls back to CURRENT_TIMESTAMP format.
func parseTimestamp(s string) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	if t, err := time.Parse("2006-01-02 15:04:05", s); err == nil {
		return t, nil
	}
	return time.Time{}, nil
}
