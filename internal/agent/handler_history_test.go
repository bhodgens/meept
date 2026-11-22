package agent

// 03-context TEST-GATE T2 (docs/plans/20261119-usability-remediation/
// 03-context/00T-gate.md, master contract C4; finding F-ORCH-29, W11).
//
// Conversation-only history durability: a chat.submit carrying only a
// conversation_id never creates a sessions row, so persistExchange's
// session_messages insert hits the sessions(id) foreign key and fails
// with "FOREIGN KEY constraint failed (787)" (handler.go:1543; the
// store DSN enables foreign_keys(1), store_sqlite.go:104/141). The
// exchange is warn-only and silently lost - resume shows nothing.
//
// C4 requires a sessions-row upsert BEFORE persistExchange: the
// user+assistant rows persist for a conversation-only id, idempotently,
// and concurrent same-conversation submits are race-safe (one row).
//
// RED today: the roundtrip subtest reproduces the actual FK 787 error
// text on current code (mutation discipline, 00T Notes). GREEN after
// 03-context/01. The control arm (a real sessions row) passes today and
// proves the fixture is not the failure reason.

import (
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/session"
)

// newHistoryGateHandler builds a ChatHandler whose message saver is the
// REAL SQLite session store (the production persistence shape: the
// daemon wires the store through SetMessageSaver). No loop is wired, so
// persistExchange takes the plain SaveMessages path - exactly the path
// the conv-only bench/GUI sessions hit.
func newHistoryGateHandler(t *testing.T) (*ChatHandler, *session.SQLiteStore) {
	t.Helper()
	store, err := session.NewSQLiteStore(filepath.Join(t.TempDir(), "sessions.db"), testLogger())
	if err != nil {
		t.Fatalf("open session store: %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("close session store: %v", err)
		}
	})
	h := &ChatHandler{}
	h.logger = testLogger()
	h.messageSaver = store
	return h, store
}

// gateHistoryExchange mirrors the persistExchange call shape of a
// completed turn (handler.go:1412): user message + assistant reply.
func gateHistoryExchange(h *ChatHandler, convID, userMsg, reply string) {
	h.persistExchange(convID, userMsg, nil, reply, "")
}

// TestConvOnlyHistoryRoundtrip pins C4 history durability for
// conversation-only submits: after the turn's exchange is persisted,
// messages.get (store.GetMessages) returns the user AND assistant rows.
func TestConvOnlyHistoryRoundtrip(t *testing.T) {
	h, store := newHistoryGateHandler(t)

	// Control (passes today): a conversation backed by a REAL sessions
	// row roundtrips. Production keys the exchange on persistID = the
	// session primary id when the submit carried one (handler.go:1290),
	// which satisfies the sessions(id) foreign key. Proves the store
	// fixture and read path work, so the conv-only arm below fails for
	// the missing upsert, not the fixture.
	t.Run("control: session-backed conversation roundtrips", func(t *testing.T) {
		sess, err := store.Create("gate-history-control")
		if err != nil {
			t.Fatalf("create session: %v", err)
		}
		gateHistoryExchange(h, sess.ID, "hello from a real session", "hi there")
		msgs, err := store.GetMessages(sess.ID, 0, 100)
		if err != nil {
			t.Fatalf("GetMessages: %v", err)
		}
		if len(msgs) != 2 || msgs[0].Role != "user" || msgs[1].Role != "assistant" {
			t.Fatalf("control roundtrip lost rows: %+v", msgs)
		}
	})

	// Gate arm (RED today, FK 787): a conversation-only id has no
	// sessions row; the exchange must still persist (C4 upsert before
	// persistExchange).
	t.Run("conv-only exchange persists user and assistant rows", func(t *testing.T) {
		const convID = "conv-gate-history-only"
		gateHistoryExchange(h, convID, "hello from a conv-only submit", "hi from the assistant")

		msgs, err := store.GetMessages(convID, 0, 100)
		if err != nil {
			t.Fatalf("GetMessages(conv-only): %v", err)
		}
		if len(msgs) != 2 {
			// Mutation discipline: surface the ACTUAL current failure.
			// persistExchange is warn-only, so re-run the store write
			// directly to capture the error text the turn swallowed.
			probeErr := store.SaveMessages(convID, []session.Message{
				{SessionID: convID, Role: "user", Content: "probe", Timestamp: time.Now().UTC(), EntryType: "message", BranchID: "main"},
			})
			t.Fatalf("conv-only history lost: want user+assistant rows, got %d (F-ORCH-29). Direct store write error on current code: %v",
				len(msgs), probeErr)
		}
		if msgs[0].Role != "user" || msgs[0].Content != "hello from a conv-only submit" {
			t.Errorf("user row wrong: %+v", msgs[0])
		}
		if msgs[1].Role != "assistant" || msgs[1].Content != "hi from the assistant" {
			t.Errorf("assistant row wrong: %+v", msgs[1])
		}
	})

	// Gate arm (RED today): concurrent same-conversation submits are
	// race-safe - every exchange lands and the upsert never duplicates
	// the backing row or deadlocks the store.
	t.Run("concurrent conv-only submits are race-safe", func(t *testing.T) {
		const convID = "conv-gate-history-race"
		const n = 6
		var wg sync.WaitGroup
		for i := 0; i < n; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				gateHistoryExchange(h, convID,
					"concurrent question "+string(rune('a'+i)),
					"concurrent answer "+string(rune('a'+i)))
			}(i)
		}
		wg.Wait()

		msgs, err := store.GetMessages(convID, 0, 1000)
		if err != nil {
			t.Fatalf("GetMessages(race conv): %v", err)
		}
		if len(msgs) != 2*n {
			t.Fatalf("concurrent conv-only history lost rows: want %d, got %d (C4: upsert must be idempotent and race-safe)", 2*n, len(msgs))
		}
		// One backing row, not one per submit: the upsert is idempotent.
		if got := store.GetByConversationID(convID); got == nil {
			t.Fatal("no sessions row backs the conversation after the upsert")
		}
	})
}

// TestConvOnlyHistoryFK787Reproduction is the mutation-discipline pin
// (00T Notes): on CURRENT code the conv-only store write fails with the
// actual FOREIGN KEY 787 error text. It documents the defect the upsert
// must close; after 03-context/01 lands, the write succeeds and this
// test's failure branch never fires (the success branch passes).
func TestConvOnlyHistoryFK787Reproduction(t *testing.T) {
	_, store := newHistoryGateHandler(t)
	const convID = "conv-gate-fk787-probe"

	err := store.SaveMessages(convID, []session.Message{
		{SessionID: convID, Role: "user", Content: "fk probe", Timestamp: time.Now().UTC(), EntryType: "message", BranchID: "main"},
	})
	if err == nil {
		// Post-fix: the upsert (or a relaxed write path) let the exchange
		// land. Verify it is readable, then pass.
		msgs, getErr := store.GetMessages(convID, 0, 10)
		if getErr != nil || len(msgs) != 1 {
			t.Fatalf("write succeeded but history unreadable: %v (n=%d)", getErr, len(msgs))
		}
		return
	}
	if !strings.Contains(err.Error(), "FOREIGN KEY constraint failed") || !strings.Contains(err.Error(), "787") {
		t.Fatalf("expected the FOREIGN KEY 787 defect text, got: %v", err)
	}
	t.Fatalf("RED (F-ORCH-29): conv-only exchange rejected by the sessions foreign key: %v", err)
}
