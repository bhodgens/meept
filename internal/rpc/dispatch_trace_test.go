package rpc

// 02-routing TEST-GATE T3 (W9; audit-routing R3; master C5, matrix F7).
//
// session.dispatch_trace is session_id-ONLY today: rows recorded under
// the conversation id are invisible to a query by the session's primary
// id (the GUI/TUI hold the primary id). C5 requires dual-key lookup:
// session_id OR conversation_id (thread-aware), rows carrying turn_id +
// a populated input_summary.
//
// RED today: the primary-id query returns count=0 (session_id-only
// WHERE), and the rows carry no turn_id / empty input_summary.
// GREEN after 02-routing/02 (dual-key resolution + C5 columns).
//
// The handler is driven through the REAL Server.CallMethod surface with
// a REAL metrics store; no handler internals are re-implemented.

import (
	"context"
	"encoding/json"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/metrics"
	"github.com/caimlas/meept/internal/session"
)

// traceGateFixture builds a server + store with one session and dispatch
// rows recorded under the session's CONVERSATION id (the live write
// path: recordDispatch keys on the conversation id).
type traceGateFixture struct {
	server *Server
	store  *metrics.Store
	sess   *session.Session
	turnID string
	input  string
}

func newTraceGateFixture(t *testing.T) *traceGateFixture {
	t.Helper()
	logger := slog.New(slog.DiscardHandler)

	store, err := metrics.NewStore(&metrics.StoreConfig{
		DatabasePath:  filepath.Join(t.TempDir(), "metrics.db"),
		BatchSize:     1,
		FlushInterval: time.Hour,
	})
	if err != nil {
		t.Fatalf("metrics store: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := store.Close(); closeErr != nil {
			t.Errorf("close metrics store: %v", closeErr)
		}
	})

	ss := session.NewMemoryStore(logger)
	t.Cleanup(func() {
		if closeErr := ss.Close(); closeErr != nil {
			t.Errorf("close session store: %v", closeErr)
		}
	})
	sess, err := ss.Create("trace-gate")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	f := &traceGateFixture{
		store: store,
		sess:  sess,
		// The frozen p1 edit prompt is the input under test (verbatim).
		input:  "Now change hello.txt so it says 'hello meept' instead.",
		turnID: "turn-dualkey-1",
	}

	store.RecordDispatch(metrics.DispatchEntry{
		SessionID:        sess.ConversationID,
		InputSummary:     f.input,
		IntentType:       "code",
		AgentID:          "coder",
		Confidence:       0.9,
		ClassifierMethod: "llm",
		HandlerCase:      "async_dispatch",
		TaskID:           "task-dualkey",
		TurnNo:           1,
		Outcome:          "pending",
	})

	server := New(&Config{SocketPath: filepath.Join(t.TempDir(), "rpc.sock")}, nil, logger)
	RegisterDispatchHandlers(server, store)
	// C5 wiring seam: dual-key resolution needs the session store to map
	// a primary session id (or thread id) to the owning conversation.
	// The setter lands with 02-routing/02; until then the handler stays
	// session_id-only and the primary-id arm below fails RED.
	wireTraceSessionStore(server, ss)
	f.server = server
	return f
}

// wireTraceSessionStore attaches the session store when the C5 seam
// exists. Reflection-free optional wiring: the setter is looked up via
// an interface assertion so this file compiles before the seam lands.
func wireTraceSessionStore(server *Server, ss session.Store) {
	type sessionStoreWired interface {
		SetDispatchSessionStore(store chatSubmitSessionStore)
	}
	if w, ok := any(server).(sessionStoreWired); ok {
		w.SetDispatchSessionStore(ss)
	}
}

// callTrace invokes session.dispatch_trace with the given params.
func (f *traceGateFixture) callTrace(t *testing.T, params map[string]any) map[string]any {
	t.Helper()
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}
	res, err := f.server.CallMethod(context.Background(), "session.dispatch_trace", raw)
	if err != nil {
		t.Fatalf("session.dispatch_trace: %v", err)
	}
	data, err := json.Marshal(res)
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	return out
}

// traceEntries extracts the entries array from a trace response. A nil
// slice marshals as JSON null; null and [] both mean "no rows".
func traceEntries(t *testing.T, out map[string]any) []map[string]any {
	t.Helper()
	if out["entries"] == nil {
		return nil
	}
	rawEntries, ok := out["entries"].([]any)
	if !ok {
		t.Fatalf("response entries is not an array: %v", out)
	}
	entries := make([]map[string]any, 0, len(rawEntries))
	for _, e := range rawEntries {
		m, ok := e.(map[string]any)
		if !ok {
			t.Fatalf("entry is not an object: %v", e)
		}
		entries = append(entries, m)
	}
	return entries
}

// TestDualKeyTrace_PrimarySessionID pins the C5 dual-key contract: rows
// recorded under the conversation id must be returned when the client
// queries by the session's PRIMARY id. RED today: count=0.
func TestDualKeyTrace_PrimarySessionID(t *testing.T) {
	f := newTraceGateFixture(t)

	out := f.callTrace(t, map[string]any{"session_id": f.sess.ID})
	entries := traceEntries(t, out)
	if len(entries) == 0 {
		t.Fatalf("query by primary session id %q returned 0 rows; the dispatch was recorded under conversation id %q - C5 requires dual-key resolution",
			f.sess.ID, f.sess.ConversationID)
	}
	for _, e := range entries {
		if sid, _ := e["session_id"].(string); sid != f.sess.ConversationID {
			t.Errorf("dual-key lookup cross-returned another session's row: session_id=%q", sid)
		}
	}
}

// TestDualKeyTrace_ConversationIDKeepsWorking pins the legacy key: the
// conversation-id query must keep returning the rows (no regression).
func TestDualKeyTrace_ConversationIDKeepsWorking(t *testing.T) {
	f := newTraceGateFixture(t)

	out := f.callTrace(t, map[string]any{"session_id": f.sess.ConversationID})
	entries := traceEntries(t, out)
	if len(entries) != 1 {
		t.Fatalf("conversation-id query returned %d rows, want 1", len(entries))
	}
}

// TestDualKeyTrace_ThreadIDResolves pins thread-awareness: a thread-scoped
// conversation id ("<base>-thread-<topic>-<suffix>") must unwrap to the
// owning session's rows, never cross-return.
func TestDualKeyTrace_ThreadIDResolves(t *testing.T) {
	f := newTraceGateFixture(t)
	threadID := f.sess.ConversationID + "-thread-work-0001"

	out := f.callTrace(t, map[string]any{"session_id": threadID})
	entries := traceEntries(t, out)
	if len(entries) == 0 {
		t.Fatalf("thread-scoped id %q returned 0 rows; C5 dual-key resolution must unwrap thread ids to the owning conversation", threadID)
	}
}

// TestDualKeyTrace_UnknownIDEmptyNotError pins the negative path: an
// unknown id yields an empty list, never an error (the GUI popover must
// render "no evidence", not a 500).
func TestDualKeyTrace_UnknownIDEmptyNotError(t *testing.T) {
	f := newTraceGateFixture(t)

	out := f.callTrace(t, map[string]any{"session_id": "session-does-not-exist"})
	entries := traceEntries(t, out)
	if len(entries) != 0 {
		t.Fatalf("unknown id returned %d rows, want 0 (never another session's rows)", len(entries))
	}
}

// TestTraceTurnID_RowsCarryTurnIDAndInput pins the C5 row shape: every
// returned row carries turn_id and a NON-EMPTY input_summary. RED today:
// no turn_id key on the wire, input_summary always "".
func TestTraceTurnID_RowsCarryTurnIDAndInput(t *testing.T) {
	f := newTraceGateFixture(t)

	out := f.callTrace(t, map[string]any{"session_id": f.sess.ConversationID})
	entries := traceEntries(t, out)
	if len(entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(entries))
	}
	e := entries[0]

	turnID, _ := e["turn_id"].(string)
	if turnID == "" {
		t.Error("trace row carries no turn_id: C5 requires the turn join key on every row (dispatch_log column + wire field)")
	}
	summary, _ := e["input_summary"].(string)
	if summary == "" {
		t.Error("trace row input_summary is empty: C5 requires populated input text (bounded 200 chars) so the why-this-route popover can show what was routed")
	}
}
