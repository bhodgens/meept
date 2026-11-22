package agent

// 02-routing TEST-GATE T5 (W13; R7; session-steering A6/B7; master C3,
// matrix F8).
//
// RecoverPendingFollowUps -> loadFollowups SELECTs the persisted
// follow-up rows and DELETEs them, publishing only a COUNT event
// (QueueRestorePayload{ConversationID, Count}). The message CONTENT is
// destroyed at boot: nothing re-enqueues it to the session's inbox, so
// a daemon restart silently drops every queued follow-up.
//
// Contract (02-routing/03 T2): boot recovery re-enqueues the FULL
// original content to the durable session inbox; rows are deleted only
// AFTER successful injection (crash between inject and delete must not
// duplicate); the restore event reflects reality.
//
// RED today: after RecoverPendingFollowUps the rows are gone and no
// inbox/queue anywhere holds the content.

import (
	"database/sql"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"

	_ "modernc.org/sqlite"
)

// bootGateDB opens a scratch DB with the queued_followups schema (the
// same DDL QueuePersister ships) and seeds follow-up rows.
func bootGateDB(t *testing.T, convID string, contents []string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "queue.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := db.Close(); closeErr != nil {
			t.Errorf("close db: %v", closeErr)
		}
	})
	if _, err := db.Exec(queuedFollowupsSchema); err != nil {
		t.Fatalf("schema: %v", err)
	}
	for i, content := range contents {
		if _, err := db.Exec(
			`INSERT INTO queued_followups (conversation_id, message_id, content, queue_type, source, created_at)
			 VALUES (?, ?, ?, ?, ?, ?)`,
			convID, "msg-boot-"+string(rune('a'+i)), content, string(QueueTypeFollowUp), "tui",
			time.Now().UTC().Add(time.Duration(i)*time.Second).Format(time.RFC3339)); err != nil {
			t.Fatalf("seed follow-up %d: %v", i, err)
		}
	}
	return db
}

func countFollowupRows(t *testing.T, db *sql.DB, convID string) int {
	t.Helper()
	var n int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM queued_followups WHERE conversation_id = ?`, convID).Scan(&n); err != nil {
		t.Fatalf("count rows: %v", err)
	}
	return n
}

// wireBootRecoveryInbox attaches the session-inbox source boot recovery
// re-enqueues through, when the 02-routing/03 seam lands. The seam is
// looked up via an interface assertion so this gate compiles today; a
// missing seam IS the RED state (recovery has nowhere to inject).
func wireBootRecoveryInbox(convID string, q *MessageQueue) {
	type inboxWired interface {
		SetBootRecoveryInbox(convID string, q *MessageQueue)
	}
	// Package-level seam candidate: RecoverPendingFollowUps gains an
	// inbox-resolution parameter or a wired resolver. Until then this
	// is a no-op and the content assertions below fail RED.
	if w, ok := any(q).(inboxWired); ok {
		w.SetBootRecoveryInbox(convID, q)
	}
}

// TestBootFollowupReenqueue_ContentSurvivesBoot pins the core contract:
// after boot recovery the FULL follow-up content must be reachable in
// the session's durable inbox (the MessageQueue registered for the
// conversation). Today the content is deleted and only a count event is
// published.
func TestBootFollowupReenqueue_ContentSurvivesBoot(t *testing.T) {
	const convID = "conv-boot-recovery"
	contents := []string{
		"make it five languages instead",
		"also add a dry-run flag to the translator",
	}
	db := bootGateDB(t, convID, contents)

	logger := testLogger()
	msgBus := bus.New(nil, logger)
	t.Cleanup(msgBus.Close)

	// The session's live queue: boot recovery must re-enqueue here (C3:
	// one fresh instance per registration; resolution order exact key
	// -> thread-unwrapped base -> session-owned thread keys).
	q := NewMessageQueue()
	t.Cleanup(q.Close)
	wireBootRecoveryInbox(convID, q)

	restoreSub := msgBus.Subscribe("gate-boot-restore", bus.EventQueueFollowUpRestored)
	t.Cleanup(func() { msgBus.Unsubscribe(restoreSub) })

	RecoverPendingFollowUps(db, msgBus, logger)

	// The restore event must still fire (count reflects reality).
	select {
	case msg := <-restoreSub.Channel:
		if msg == nil {
			t.Fatal("nil restore event")
		}
		var payload QueueRestorePayload
		if err := json.Unmarshal(msg.Payload, &payload); err != nil {
			t.Fatalf("decode restore payload: %v", err)
		}
		if payload.ConversationID != convID || payload.Count != len(contents) {
			t.Errorf("restore payload = %+v, want conv %q count %d", payload, convID, len(contents))
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no queue restore event published")
	}

	// CONTENT must survive: the session inbox holds every follow-up,
	// verbatim, in order. RED today: the queue is empty (content deleted).
	drained := q.DrainFollowUp()
	if len(drained) != len(contents) {
		t.Fatalf("session inbox holds %d follow-ups after boot, want %d: loadAndClear deleted the content and published only a count - boot recovery must re-enqueue the FULL messages (C3)",
			len(drained), len(contents))
	}
	for i, msg := range drained {
		if msg.Content != contents[i] {
			t.Errorf("inbox message %d = %q, want the full original %q (never a summary)", i, msg.Content, contents[i])
		}
	}

	// Rows deleted only AFTER injection: with a live inbox the rows are
	// consumed; the delete-after-inject ordering itself is pinned by the
	// no-inbox test below.
	if n := countFollowupRows(t, db, convID); n != 0 {
		t.Errorf("rows remaining after successful injection = %d, want 0", n)
	}
}

// TestBootFollowupReenqueue_NoInboxKeepsRows pins the crash-safety
// ordering: when NO session inbox is available (daemon boots before any
// client reconnects), recovery must NOT delete the rows - deletion is
// only legal after successful injection. RED today: loadAndClear
// deletes unconditionally, so a boot with no listener destroys the
// follow-ups permanently.
func TestBootFollowupReenqueue_NoInboxKeepsRows(t *testing.T) {
	const convID = "conv-boot-nolistener"
	contents := []string{"refine the plan: add a rollback phase"}
	db := bootGateDB(t, convID, contents)

	logger := testLogger()
	msgBus := bus.New(nil, logger)
	t.Cleanup(msgBus.Close)

	// No inbox wired: nothing can receive the content. Recovery must
	// leave the durable rows intact for the next boot / reconnect.
	RecoverPendingFollowUps(db, msgBus, logger)

	if n := countFollowupRows(t, db, convID); n != len(contents) {
		t.Fatalf("rows after boot with no inbox = %d, want %d: loadAndClear DELETEd the follow-ups with nowhere to inject them - rows may only be deleted AFTER successful injection (crash between inject and delete must never lose or duplicate content)",
			n, len(contents))
	}

	// A second boot with a live inbox must still deliver the content
	// exactly once (idempotence across the crash window).
	q := NewMessageQueue()
	t.Cleanup(q.Close)
	wireBootRecoveryInbox(convID, q)

	RecoverPendingFollowUps(db, msgBus, logger)
	drained := q.DrainFollowUp()
	if len(drained) != len(contents) {
		t.Fatalf("second boot delivered %d messages, want %d (rows were destroyed by the first boot)", len(drained), len(contents))
	}
	if len(drained) > 0 && drained[0].Content != contents[0] {
		t.Errorf("redelivered content = %q, want %q", drained[0].Content, contents[0])
	}
}
